package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGranularFullPublication(t *testing.T) {
	db := supplierFlowDB(t)
	owner := supplierFlowSeed(t, db)
	// TEST-02 round 4: SaveFieldReview upserts ON CONFLICT
	// (proposal_id, field_key) (migration 202610170010). Standing
	// proposal_field_reviews rebuilds drop the UNIQUE, so the upsert
	// fails with 42P10. Restore it.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS test02_pfr_proposal_field_key ON proposal_field_reviews(proposal_id, field_key)`); err != nil {
		t.Fatal(err)
	}

	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()
	variants := []interface{}{}
	for i, color := range []string{"red", "blue", "green", "black", "white"} {
		variants = append(variants, map[string]interface{}{"variant_name": "color: " + color, "attributes": map[string]interface{}{"color": color}, "stock": float64(i + 1), "supplier_base_cost": float64(1000 + i*100)})
	}
	data := map[string]interface{}{"title": "Granular publication test", "productType": "variable", "categorySlug": "digital", "description": "Synthetic product for isolated review tests only", "supplierBaseCost": float64(1000), "stock": float64(15), "variants": variants, "imageUrls": []interface{}{"/static/test-a.png", "/static/test-b.png"}, "imageUrl": "/static/test-a.png", "weightGrams": float64(100), "lengthMm": float64(100), "widthMm": float64(100), "heightMm": float64(100)}
	data["supplierWarehouseId"] = owner.warehouse
	id, e := repo.ProposeProduct(ctx, owner.supplier, data)
	if e != nil {
		t.Fatal(e)
	}
	jwt := auth.NewJWTService(strings.Repeat("r", 32))
	endpoint := RequireRoles(jwt, "admin")(func(w http.ResponseWriter, r *http.Request) { handleProductFieldReview(w, r, repo) })
	for _, role := range []string{"", "supplier", "super_admin"} {
		req := httptest.NewRequest("GET", "/api/v1/admin/product-field-reviews/"+id, nil)
		if role != "" {
			token, err := jwt.GenerateToken(owner.user, "09123456789", "Audit", role, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		result := httptest.NewRecorder()
		endpoint(result, req)
		want := 200
		if role == "" {
			want = 401
		} else if role == "supplier" {
			want = 403
		}
		if result.Code != want {
			t.Fatalf("review authorization %s: %d %s", role, result.Code, result.Body.String())
		}
	}
	if _, e = repo.ApproveProposal(ctx, id, ""); e == nil {
		t.Fatal("unreviewed publication")
	}
	decisions := []repository.FieldDecision{}
	for key, value := range repository.ReviewLeaves(data) {
		d := repository.FieldDecision{Key: key, Decision: "approved", ValueHash: repository.ReviewValueHash(value)}
		if key == "/variants/0/stock" || key == "/imageUrl" || key == "/variants/2/supplier_base_cost" || key == "/variants/1/attributes/color" {
			d.Decision = "rejected"
			d.Reason = "correct count"
		}
		decisions = append(decisions, d)
	}
	if e = repo.SaveFieldReview(ctx, id, "test-admin", decisions); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.ApproveProposal(ctx, id, ""); e == nil {
		t.Fatal("rejected publication")
	}
	var titleReviewedAt time.Time
	var titleReviewer string
	if err := db.QueryRow(`SELECT reviewed_at,reviewer_id FROM proposal_field_reviews WHERE proposal_id=$1 AND field_key='/title'`, id).Scan(&titleReviewedAt, &titleReviewer); err != nil {
		t.Fatal(err)
	}
	supplierJWT := auth.NewJWTService(strings.Repeat("s", 32))
	supplierJWT.SetCapabilityReader(repository.NewCapabilityRepository(db))
	supplierToken, err := supplierJWT.GenerateToken(owner.user, "09123456789", "Audit supplier", "supplier", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	corrections := map[string]interface{}{"/variants/0/stock": float64(7), "/imageUrl": "/static/corrected-primary.png", "/variants/2/supplier_base_cost": float64(3333), "/variants/1/attributes/color": "orange"}
	body, _ := json.Marshal(map[string]interface{}{"data": corrections})
	resubmit := httptest.NewRequest("POST", "/api/v1/supplier/catalog/review/"+id+"/resubmit", bytes.NewReader(body))
	resubmit.Header.Set("Authorization", "Bearer "+supplierToken)
	result := httptest.NewRecorder()
	supplierHandlers := NewSupplierHandlers(repo, supplierJWT, false, repository.NewCapabilityRepository(db))
	RequireSupplierAccess(supplierJWT, repo)(func(w http.ResponseWriter, r *http.Request) { supplierHandlers.HandleResubmitCatalogReview(w, r, id) })(result, resubmit)
	if result.Code != 200 {
		t.Fatal("supplier HTTP correction failed", result.Code, result.Body.String())
	}
	feedback := httptest.NewRequest("GET", "/api/v1/supplier/catalog/review/"+id, nil)
	feedback.Header.Set("Authorization", "Bearer "+supplierToken)
	feedbackResult := httptest.NewRecorder()
	RequireSupplierAccess(supplierJWT, repo)(func(w http.ResponseWriter, r *http.Request) { supplierHandlers.HandleGetCatalogReviewDetail(w, r, id) })(feedbackResult, feedback)
	if feedbackResult.Code != 200 {
		t.Fatal("supplier feedback failed", feedbackResult.Code, feedbackResult.Body.String())
	}

	dossier, e := repo.GetAdminFieldReview(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	decisions = nil
	changed := map[string]bool{"/variants/0/stock": true, "/imageUrl": true, "/variants/2/supplier_base_cost": true, "/variants/1/attributes/color": true}
	fields := dossier["fields"].([]map[string]interface{})
	for _, field := range fields {
		key := field["field_key"].(string)
		if changed[key] {
			if field["decision"] != "pending" {
				t.Fatalf("corrected field not reopened: %s %v", key, field["decision"])
			}
			decisions = append(decisions, repository.FieldDecision{Key: key, Decision: "approved", ValueHash: field["value_hash"].(string)})
		} else if field["decision"] != "approved" {
			t.Fatalf("prior approval lost: %s %v", key, field["decision"])
		}
	}
	if len(decisions) != len(changed) {
		t.Fatalf("unexpected re-review count %d", len(decisions))
	}
	var sameAt time.Time
	var sameReviewer string
	if err := db.QueryRow(`SELECT reviewed_at,reviewer_id FROM proposal_field_reviews WHERE proposal_id=$1 AND field_key='/title'`, id).Scan(&sameAt, &sameReviewer); err != nil || !sameAt.Equal(titleReviewedAt) || sameReviewer != titleReviewer {
		t.Fatal("unchanged approval metadata modified", err)
	}
	sparseBody, _ := json.Marshal(map[string]interface{}{"fields": decisions})
	sparse := httptest.NewRequest("PUT", "/api/v1/admin/product-field-reviews/"+id, bytes.NewReader(sparseBody))
	adminToken, err := jwt.GenerateToken(owner.user, "09123456789", "Audit admin", "super_admin", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sparse.Header.Set("Authorization", "Bearer "+adminToken)
	sparseResult := httptest.NewRecorder()
	endpoint(sparseResult, sparse)
	if sparseResult.Code != 200 {
		t.Fatal("sparse admin HTTP review", sparseResult.Code, sparseResult.Body.String())
	}

	if err := db.QueryRow(`SELECT reviewed_at,reviewer_id FROM proposal_field_reviews WHERE proposal_id=$1 AND field_key='/title'`, id).Scan(&sameAt, &sameReviewer); err != nil || !sameAt.Equal(titleReviewedAt) || sameReviewer != titleReviewer {
		t.Fatal("sparse review rewrote prior approval", err)
	}
	finalRequest := httptest.NewRequest("POST", "/api/v1/admin/approvals/"+id+"/review", strings.NewReader(`{"status":"approved"}`))
	finalRequest.Header.Set("Authorization", "Bearer "+adminToken)
	finalResult := httptest.NewRecorder()
	RequireRoles(jwt, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleReviewApproval(w, r, r.Context(), id, repository.NewAdminRepository(db), repo)
	})(finalResult, finalRequest)
	if finalResult.Code != 200 {
		t.Fatal("final HTTP approval", finalResult.Code, finalResult.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(finalResult.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	product, ok := response["productId"].(string)
	if !ok || product == "" {
		t.Fatal("missing final product ID")
	}

	var primary string
	if err := db.QueryRow(`SELECT image_url FROM products WHERE id=$1`, product).Scan(&primary); err != nil || primary != "/static/corrected-primary.png" {
		t.Fatal("primary correction lost", primary, err)
	}
	var count, defaults, total, images int
	if e = db.QueryRow(`SELECT COUNT(*),COUNT(*) FILTER(WHERE is_default),SUM(stock) FROM product_variants WHERE product_id=$1`, product).Scan(&count, &defaults, &total); e != nil {
		t.Fatal(e)
	}
	if count != 5 || defaults != 0 || total != 21 {
		t.Fatalf("SKUs %d default %d stock %d", count, defaults, total)
	}
	var minimum, maximum int
	if err := db.QueryRow(`SELECT MIN(supplier_base_cost),MAX(supplier_base_cost) FROM product_variants WHERE product_id=$1`, product).Scan(&minimum, &maximum); err != nil || minimum != 1000 || maximum != 3333 {
		t.Fatal("independent SKU prices lost", minimum, maximum, err)
	}
	var corrected int
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_variants WHERE product_id=$1 AND attributes->>'color'='orange' AND variant_name LIKE '%orange%'`, product).Scan(&corrected); err != nil || corrected != 1 {
		t.Fatal("attribute correction lost", corrected, err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM product_images WHERE product_id=$1`, product).Scan(&images)
	if images != 2 {
		t.Fatal("gallery dropped", images)
	}
	if _, e = repo.ApproveProposal(ctx, id, ""); e == nil {
		t.Fatal("duplicate approval")
	}
	if repo.ResubmitGranularReview(ctx, owner.supplier, id, map[string]interface{}{"/variants/0/stock": float64(9)}) == nil {
		t.Fatal("published correction")
	}
}
