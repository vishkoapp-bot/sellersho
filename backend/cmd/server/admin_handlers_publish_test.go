package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func newReviewPublicationProposal(t *testing.T, db *sql.DB, supplier string, data map[string]any) string {
	t.Helper()
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS test02_pfr_proposal_field_key ON proposal_field_reviews(proposal_id, field_key)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	id, err := repo.ProposeProduct(ctx, supplier, data)
	if err != nil {
		t.Fatal(err)
	}
	var decisions []repository.FieldDecision
	for key, value := range repository.ReviewLeaves(data) {
		decisions = append(decisions, repository.FieldDecision{
			Key: key, Decision: "approved", ValueHash: repository.ReviewValueHash(value),
		})
	}
	if err := repo.SaveFieldReview(ctx, id, "test-admin", decisions); err != nil {
		t.Fatal(err)
	}
	return id
}

func newV2ReviewPublicationProposal(t *testing.T, db *sql.DB) (id, category, user, title string) {
	t.Helper()
	owner := supplierFlowSeed(t, db)
	suffix := uuid.NewString()[:8]
	category = "cat-g3-" + suffix
	title = "G3 publication " + suffix
	if _, err := db.Exec(`INSERT INTO categories(id,title,slug,icon_name) VALUES($1,'G3 fixture category',$1,'')`, category); err != nil {
		t.Fatal(err)
	}
	id = newReviewPublicationProposal(t, db, owner.supplier, map[string]any{
		"title": title, "categorySlug": category, "supplierBaseCost": float64(1000), "stock": float64(10),
		"schemaVersion": float64(2), "productType": "simple",
		"productSchema": map[string]any{"version": float64(2), "type": "simple", "attributes": []any{}},
		"variants": []any{map[string]any{
			"id": uuid.NewString(), "selection": map[string]any{}, "stock": float64(10),
			"supplier_base_cost": float64(1000), "is_active": true,
		}},
		"supplierWarehouseId": owner.warehouse, "weightGrams": float64(500),
		"lengthMm": float64(100), "widthMm": float64(100), "heightMm": float64(100),
		"fulfillmentMethod": "supplier_direct",
	})
	return id, category, owner.user, title
}

func postReviewPublication(t *testing.T, db *sql.DB, id, user string) *httptest.ResponseRecorder {
	t.Helper()
	jwt := auth.NewJWTService(strings.Repeat("r", 32))
	token, err := jwt.GenerateToken(user, "09123456789", "Audit admin", "super_admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/approvals/"+id+"/review", strings.NewReader(`{"status":"approved"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	RequireRoles(jwt, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleReviewApproval(w, r, r.Context(), id, repository.NewAdminRepository(db), repository.NewSupplierRepository(db))
	})(res, req)
	return res
}

func assertReviewPublicationState(t *testing.T, db *sql.DB, id, title, wantStatus string, wantProduct bool) {
	t.Helper()
	var status string
	if err := db.QueryRow(`SELECT status FROM pending_approvals WHERE id=$1`, id).Scan(&status); err != nil || status != wantStatus {
		t.Fatalf("proposal status = %q, want %q, error: %v", status, wantStatus, err)
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM products WHERE title=$1)`, title).Scan(&exists); err != nil || exists != wantProduct {
		t.Fatalf("product exists = %t, want %t, error: %v", exists, wantProduct, err)
	}
}

func TestHandleReviewApproval_PublishSucceedsWhenReferencesValid(t *testing.T) {
	db := supplierFlowDB(t)
	id, _, user, title := newV2ReviewPublicationProposal(t, db)
	res := postReviewPublication(t, db, id, user)
	if res.Code != http.StatusOK {
		t.Fatalf("publish = %d %s", res.Code, res.Body.String())
	}
	var response struct {
		Success   bool   `json:"success"`
		Status    string `json:"status"`
		ProductID string `json:"productId"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil || !response.Success || response.Status != "approved" || response.ProductID == "" {
		t.Fatalf("unexpected publish response: %s (%v)", res.Body.String(), err)
	}
	assertReviewPublicationState(t, db, id, title, "approved", true)
	var publishedTitle string
	if err := db.QueryRow(`SELECT title FROM products WHERE id=$1`, response.ProductID).Scan(&publishedTitle); err != nil || publishedTitle != title {
		t.Fatalf("returned product title = %q, want %q, error: %v", publishedTitle, title, err)
	}
}

func TestHandleReviewApproval_PublishSurfacesCategoryCauseNotApprovalNotFound(t *testing.T) {
	db := supplierFlowDB(t)
	id, category, user, title := newV2ReviewPublicationProposal(t, db)
	deleted, err := db.Exec(`DELETE FROM categories WHERE id=$1`, category)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := deleted.RowsAffected(); err != nil || count != 1 {
		t.Fatalf("deleted %d categories, want 1, error: %v", count, err)
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM categories WHERE slug=$1)`, category).Scan(&exists); err != nil || exists {
		t.Fatalf("category still exists: %t, error: %v", exists, err)
	}
	res := postReviewPublication(t, db, id, user)
	if res.Code != http.StatusInternalServerError || !strings.Contains(res.Body.String(), "invalid product category") {
		t.Fatalf("publish = %d %s, want 500 with category cause", res.Code, res.Body.String())
	}
	assertReviewPublicationState(t, db, id, title, "pending", false)
}

func TestHandleReviewApproval_PublishSurfacesWarehouseCauseNotApprovalNotFound(t *testing.T) {
	db := supplierFlowDB(t)
	owner := supplierFlowSeed(t, db)
	warehouse := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name,is_active)
		VALUES($1,$2,'Inactive source','Test','Test',FALSE)`, warehouse, owner.supplier); err != nil {
		t.Fatal(err)
	}
	var variants []any
	for i, color := range []string{"red", "blue", "green", "black", "white"} {
		variants = append(variants, map[string]any{
			"variant_name": "color: " + color, "attributes": map[string]any{"color": color},
			"stock": float64(i + 1), "supplier_base_cost": float64(1000 + i*100),
		})
	}
	title := "G3 inactive warehouse " + warehouse
	id := newReviewPublicationProposal(t, db, owner.supplier, map[string]any{
		"title": title, "productType": "variable", "categorySlug": "digital",
		"description":      "Synthetic product for isolated review tests only",
		"supplierBaseCost": float64(1000), "stock": float64(15), "variants": variants,
		"imageUrls": []any{"/static/test-a.png", "/static/test-b.png"}, "imageUrl": "/static/test-a.png",
		"weightGrams": float64(100), "lengthMm": float64(100), "widthMm": float64(100), "heightMm": float64(100),
		"supplierWarehouseId": warehouse,
	})
	res := postReviewPublication(t, db, id, owner.user)
	if res.Code != http.StatusInternalServerError || !strings.Contains(res.Body.String(), "publication requires an active owned supplier origin") {
		t.Fatalf("publish = %d %s, want 500 with supplier origin cause", res.Code, res.Body.String())
	}
	assertReviewPublicationState(t, db, id, title, "pending", false)
}

func TestHandleReviewApproval_UnknownIdStillReturnsApprovalNotFound(t *testing.T) {
	db := supplierFlowDB(t)
	owner := supplierFlowSeed(t, db)
	res := postReviewPublication(t, db, "does-not-exist", owner.user)
	if res.Code != http.StatusNotFound || strings.TrimSpace(res.Body.String()) != `{"error":"Approval not found"}` {
		t.Fatalf("publish = %d %s, want 404 with Approval not found", res.Code, res.Body.String())
	}
}
