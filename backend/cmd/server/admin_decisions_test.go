package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestAdminSMSActionRequiresPost(t *testing.T) {
	response := httptest.NewRecorder()
	handleIPPanelTest(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ippanel/test", nil), nil)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("SMS action status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func adminDecisionsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func reviewAdminDecision(db *sql.DB, id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/approvals/"+id+"/review", strings.NewReader(body))
	handleReviewApproval(w, r, r.Context(), id, repository.NewAdminRepository(db), repository.NewSupplierRepository(db))
	return w
}

func TestAdminProposalDecisionRejectsMalformedPayload(t *testing.T) {
	repo := repository.NewSupplierRepository(testdb.NoConnections(t))
	handlers := NewSupplierHandlers(repo, nil, false)
	for _, tc := range []struct{ name, body string }{
		{"truncated", `{"approved":`},
		{"wrong type", `{"approved":"false"}`},
		{"trailing JSON", `{"approved":true} {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/approve/fixture-proposal", strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			handlers.HandleApproveProposal(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("proposal decision = %d %s, want 400", w.Code, w.Body)
			}
		})
	}
}

func TestAdminWarrantyDecisionRejectsSupplierRequest(t *testing.T) {
	db := adminDecisionsDB(t)
	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()
	var supplier string
	if err := db.QueryRow(`SELECT id FROM suppliers ORDER BY id LIMIT 1`).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	name := "Warranty " + uuid.NewString()
	request := func() string {
		t.Helper()
		id, err := repo.RequestWarrantyProperty(ctx, supplier, "", name, "Detailed warranty fixture description", []string{"fixture.png"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := request()
	w := reviewAdminDecision(db, id, `{"status":"rejected","rejectionReason":"Need more detail"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("reject = %d %s", w.Code, w.Body)
	}
	var status, reason string
	if err := db.QueryRow(`SELECT status, COALESCE(rejection_reason,'') FROM warranty_property_requests WHERE id=$1`, id).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" || reason != "Need more detail" {
		t.Fatalf("warranty decision = (%q, %q), want rejected with reason", status, reason)
	}
	request()
	if w := reviewAdminDecision(db, id, `{"status":"approved"}`); w.Code != http.StatusConflict {
		t.Fatalf("re-review = %d, want 409", w.Code)
	}
}

func TestAdminWarrantyDecisionReusesExistingProperty(t *testing.T) {
	db := adminDecisionsDB(t)
	var supplier string
	if err := db.QueryRow(`SELECT id FROM suppliers ORDER BY id LIMIT 1`).Scan(&supplier); err != nil {
		t.Fatal(err)
	}
	property := "property-" + uuid.NewString()
	name := "Warranty " + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO warranty_properties(id,name,description,status) VALUES($1,$2,'Existing property','inactive')`, property, name); err != nil {
		t.Fatal(err)
	}
	id, err := repository.NewSupplierRepository(db).RequestWarrantyProperty(context.Background(), supplier, "", strings.ToLower(name), "Detailed warranty fixture description", []string{"fixture.png"})
	if err != nil {
		t.Fatal(err)
	}
	w := reviewAdminDecision(db, id, `{"status":"approved"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", w.Code, w.Body)
	}
	var status, propertyID, propertyStatus string
	if err := db.QueryRow(`SELECT r.status,r.approved_property_id,p.status
		FROM warranty_property_requests r JOIN warranty_properties p ON p.id=r.approved_property_id WHERE r.id=$1`, id).Scan(&status, &propertyID, &propertyStatus); err != nil {
		t.Fatal(err)
	}
	if status != "approved" || propertyID != property || propertyStatus != "active" {
		t.Fatalf("warranty decision = (%q, %q, %q), want approved with existing active property", status, propertyID, propertyStatus)
	}
}

func TestAdminStoreDecisionAppliesChange(t *testing.T) {
	db := adminDecisionsDB(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	store := "review-store-" + suffix
	var owner string
	if err := db.QueryRow(`SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO stores(id,name,slug,owner_name,owner_user_id) VALUES($1,'Old name',$1,'Fixture owner',$2)`, store, owner); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewStoreRepository(db)
	for _, tc := range []struct{ kind, value string }{
		{"store_name", "New name"},
		{"store_slug", "review-new-" + suffix},
	} {
		if err := repo.RequestStoreChange(ctx, owner, store, tc.kind, tc.value); err != nil {
			t.Fatal(err)
		}
		var id string
		if err := db.QueryRow(`SELECT id FROM pending_approvals WHERE entity_type=$1 AND target_id=$2`, tc.kind, store).Scan(&id); err != nil {
			t.Fatal(err)
		}
		w := reviewAdminDecision(db, id, `{"status":"approved"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("approve %s = %d %s", tc.kind, w.Code, w.Body)
		}
	}
	var name, slug string
	if err := db.QueryRow(`SELECT name, slug FROM stores WHERE id=$1`, store).Scan(&name, &slug); err != nil {
		t.Fatal(err)
	}
	if name != "New name" || slug != "review-new-"+suffix {
		t.Fatalf("store = (%q, %q), want updated name and slug", name, slug)
	}
}

func TestAdminProductPricingRollsBackCeilingWhenMarkupFails(t *testing.T) {
	db := adminDecisionsDB(t)
	var product string
	if err := db.QueryRow(`SELECT id FROM products ORDER BY id LIMIT 1`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO product_margin_ceilings(product_id,max_margin_pct) VALUES($1,35)
		ON CONFLICT(product_id) DO UPDATE SET max_margin_pct=35`, product); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM product_margin_ceilings WHERE product_id=$1`, product)
	})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/products/"+product+"/pricing",
		strings.NewReader(`{"maxMarginPct":15,"markupType":"fixed","markupValue":100000000}`))
	w := httptest.NewRecorder()
	wr69AdminProductPricingPut(w, r, db, product)
	if w.Code < 400 {
		t.Fatalf("out-of-range update = %d %s", w.Code, w.Body)
	}
	var ceiling float64
	if err := db.QueryRow(`SELECT max_margin_pct FROM product_margin_ceilings WHERE product_id=$1`, product).Scan(&ceiling); err != nil {
		t.Fatal(err)
	}
	if ceiling != 35 {
		t.Fatalf("ceiling after failed update = %v, want 35", ceiling)
	}
}

func TestAdminPayoutListIncludesSupplierWithoutStore(t *testing.T) {
	db := adminDecisionsDB(t)
	id := "review-payout-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO payout_requests(id,supplier_id,recipient_type,recipient_name,shaba,bank_name,amount,status)
		VALUES($1,(SELECT id FROM suppliers ORDER BY id LIMIT 1),'supplier','Fixture supplier','fixture','Fixture bank',25000,'approved')`, id); err != nil {
		t.Fatal(err)
	}
	for _, page := range []struct {
		path, key string
		handle    func(http.ResponseWriter, *http.Request, *sql.DB)
	}{
		{"/api/v1/admin/payouts", "payouts", handleT01AdminPayouts},
		{"/api/v1/finance/settlements/approved", "settlements", handleT01SettlementsApproved},
	} {
		t.Run(page.key, func(t *testing.T) {
			w := httptest.NewRecorder()
			page.handle(w, httptest.NewRequest(http.MethodGet, page.path, nil), db)
			if w.Code != http.StatusOK {
				t.Fatalf("payout list = %d %s", w.Code, w.Body)
			}
			var data map[string][]struct{ ID, StoreID, SupplierID, RecipientType string }
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			for _, payout := range data[page.key] {
				if payout.ID == id {
					if payout.StoreID != "" || payout.SupplierID == "" || payout.RecipientType != "supplier" {
						t.Fatalf("supplier payout = %+v", payout)
					}
					return
				}
			}
			t.Fatal("supplier payout missing")
		})
	}
}
