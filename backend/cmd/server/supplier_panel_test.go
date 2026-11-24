package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func panelSourceFixture(t *testing.T, db *sql.DB, supplier string) string {
	t.Helper()
	id := "panel-source-" + uuid.NewString()[:16]
	if _, err := db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'Test','Test')`, id, supplier); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSupplierPanelScopeAndPersistence(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Panel Test", "Owner", "panel-"+suffix, "TestPanelPassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	productID := "panel-product-" + suffix
	t.Cleanup(func() {
		for _, table := range []string{"payout_requests", "supplier_campaigns", "inbound_shipments", "qc_discrepancies"} {
			_, _ = db.Exec("DELETE FROM "+table+" WHERE supplier_id = $1", supplier.ID)
		}
		_, _ = db.Exec(`DELETE FROM pending_approvals WHERE requested_by = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM products WHERE id = $1`, productID)
		_, _ = db.Exec(`DELETE FROM suppliers WHERE id = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID)
	})
	if err := repo.CreateDirectProduct(ctx, domain.Product{ID: productID, Slug: productID, Title: "Panel product", CategorySlug: "digital", SupplierID: supplier.ID, SupplierBaseCost: 1000, Stock: 10}); err != nil {
		t.Fatal(err)
	}
	source := panelSourceFixture(t, db, supplier.ID)
	if err := repo.UpdateProductOrigin(ctx, supplier.ID, productID, source); err != nil {
		t.Fatal(err)
	}
	for name, action := range map[string]func() error{
		"foreign stock":  func() error { return repo.UpdateProductStock(ctx, "foreign", productID, 99, "") },
		"negative stock": func() error { return repo.UpdateProductStock(ctx, supplier.ID, productID, -1, "") },
		"foreign price":  func() error { return repo.UpdateProductPricing(ctx, "foreign", productID, 99, "") },
		"invalid price":  func() error { return repo.UpdateProductPricing(ctx, supplier.ID, productID, 0, "") },
		"foreign supply": func() error { return repo.UpdateProductSupplyOption(ctx, "foreign", productID, "3day") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
	if err := repo.UpdateProductStock(ctx, supplier.ID, productID, 0, "Counted empty panel fixture"); err != nil {
		t.Fatal(err)
	}
	var stock int
	if err := db.QueryRow(`SELECT stock FROM products WHERE id = $1`, productID).Scan(&stock); err != nil || stock != 0 {
		t.Fatalf("stock %d: %v", stock, err)
	}
	if _, err := repo.ProposeProduct(ctx, supplier.ID, map[string]interface{}{"title": "Scoped proposal", "supplierBaseCost": 1000}); err != nil {
		t.Fatal(err)
	}
	other, err := repo.GetSupplierProposals(ctx, "foreign")
	if err != nil || len(other) != 0 {
		t.Fatalf("foreign proposals leaked: %v %v", other, err)
	}
	own, err := repo.GetSupplierProposals(ctx, supplier.ID)
	if err != nil || len(own) != 1 {
		t.Fatalf("own proposals: %v %v", own, err)
	}
	inbounds, err := repo.GetSupplierInbounds(ctx, supplier.ID)
	if err != nil || inbounds == nil || len(inbounds) != 0 {
		t.Fatalf("empty inbounds: %v %v", inbounds, err)
	}
	if _, err := repo.GetInboundByID(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateInbound(ctx, supplier.ID, map[string]interface{}{}); err == nil {
		t.Fatal("empty inbound accepted")
	}
	inbound := map[string]interface{}{"items": []interface{}{map[string]interface{}{"product_id": productID, "quantity": float64(5), "carton_count": float64(1)}}}
	destination := "panel-destination-" + suffix
	if _, err := db.Exec(`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'Test','Test')`, destination); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateProductStock(ctx, supplier.ID, productID, 5, "Prepare inbound fixture"); err != nil {
		t.Fatal(err)
	}
	inbound["warehouseId"], inbound["supplierWarehouseId"] = destination, source
	if _, err := repo.CreateInbound(ctx, "foreign", inbound); err == nil {
		t.Fatal("foreign inbound accepted")
	}
	if _, err := repo.CreateInbound(ctx, supplier.ID, inbound); err != nil {
		t.Fatal(err)
	}
	campaign := map[string]interface{}{"campaign_name": "Panel campaign", "budget": float64(1000), "cpc_bid": float64(100), "product_ids": []interface{}{productID}, "target_categories": []interface{}{}}
	if _, err := repo.CreateCampaign(ctx, "foreign", campaign); err == nil {
		t.Fatal("foreign campaign accepted")
	}
	campaignID, err := repo.CreateCampaign(ctx, supplier.ID, campaign)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := repo.GetCampaignStats(ctx, supplier.ID, campaignID)
	if err != nil || stats == nil || stats.Impressions != 0 || stats.Clicks != 0 {
		t.Fatalf("fabricated campaign stats: %v %v", stats, err)
	}
	stats, err = repo.GetCampaignStats(ctx, "foreign", campaignID)
	if err != nil || stats != nil {
		t.Fatalf("foreign stats: %v %v", stats, err)
	}
	balance, err := repo.GetSettlementBalance(ctx, supplier.ID)
	if err != nil || balance.Available != 0 {
		t.Fatalf("empty balance: %v %v", balance, err)
	}
	if _, err := repo.RequestSettlement(ctx, supplier.ID, 1, "", "", "Owner"); err == nil {
		t.Fatal("overdrawn settlement accepted")
	}
	report, err := repo.GetSalesReport(ctx, supplier.ID, "", "")
	if err != nil || report.TotalRevenue != 0 {
		t.Fatalf("empty report: %v %v", report, err)
	}
}

func TestSupplierStockAggregateHTTPReturnsVerifiedQuantity(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Stock test", "Owner", "stock-"+suffix, "TestStockPassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	product, warehouse := "stock-"+suffix, "stock-wh-"+suffix
	if err = repo.CreateDirectProduct(ctx, domain.Product{ID: product, Slug: product, Title: "Stock product", CategorySlug: "digital", SupplierID: supplier.ID, SupplierBaseCost: 1000, Stock: 5}); err != nil {
		t.Fatal(err)
	}
	source := panelSourceFixture(t, db, supplier.ID)
	if err = repo.UpdateProductOrigin(ctx, supplier.ID, product, source); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, []interface{}{warehouse}},
		{`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,'supplier','supplier',$4,'sellable',5,$5)`, []interface{}{uuid.NewString(), product, supplier.ID, "supplier-warehouse:" + source, source}},
		{`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,$3,'supplier','warehouse',$4,'sellable',5,$5)`, []interface{}{uuid.NewString(), product, supplier.ID, "warehouse:" + warehouse, warehouse}},
	} {
		if _, err = db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	h := NewSupplierHandlers(repo, auth.NewJWTService("stock-fixture"), false)
	request := func(payload string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+product+"/stock", strings.NewReader(payload))
		contextWithSupplier := context.WithValue(r.Context(), supplierIDKey, supplier.ID)
		r = r.WithContext(context.WithValue(contextWithSupplier, userClaimsKey, &auth.UserClaims{UserID: owner.ID}))
		w := httptest.NewRecorder()
		h.HandleUpdateStock(w, r)
		return w
	}
	if w := request(`{"new_stock":10}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing reason status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(`{"new_stock":4,"reason":"below held"}`); w.Code != http.StatusConflict {
		t.Fatalf("below held status=%d body=%s", w.Code, w.Body.String())
	}
	w := request(`{"new_stock":8,"reason":"counted correction"}`)
	var result struct {
		Stock        int `json:"stock"`
		ProductStock int `json:"productStock"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result.Stock != 8 || result.ProductStock != 8 {
		t.Fatalf("verified response status=%d result=%+v err=%v", w.Code, result, err)
	}
	var projection int
	if err = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, product).Scan(&projection); err != nil || projection != result.ProductStock {
		t.Fatalf("legacy productStock disagrees with authoritative projection: DB=%d HTTP=%d err=%v", projection, result.ProductStock, err)
	}
	var local, held int
	var actor string
	if err = db.QueryRow(`SELECT SUM(quantity) FILTER(WHERE supplier_warehouse_id=$2),SUM(quantity) FILTER(WHERE warehouse_id=$3) FROM inventory_balances WHERE product_id=$1 AND status='sellable'`, product, source, warehouse).Scan(&local, &held); err != nil || local != 3 || held != 5 {
		t.Fatalf("HTTP balances=%d/%d err=%v", local, held, err)
	}
	if err = db.QueryRow(`SELECT actor_user_id FROM inventory_movements WHERE product_id=$1 AND reference_type='legacy_stock_adjustment'`, product).Scan(&actor); err != nil || actor != owner.ID {
		t.Fatal("HTTP stock actor missing")
	}
}

func TestSupplierHandlerRejectsInvalidPayload(t *testing.T) {
	h := NewSupplierHandlers(nil, auth.NewJWTService("panel-test"), false)
	cases := []struct {
		path, body string
		handler    http.HandlerFunc
	}{
		{"/api/v1/supplier/products/p/stock", `{"new_stock":-1}`, h.HandleUpdateStock},
		{"/api/v1/supplier/products/p/pricing", `{"new_supplier_base_cost":0}`, h.HandleUpdatePricing},
		{"/api/v1/supplier/settlement/request", `{"amount":-1,"shaba":"IR000","account_holder":"x"}`, h.HandleRequestSettlement},
		{"/api/v1/supplier/sales-report?start_date=invalid", `{}`, h.HandleGetSalesReport},
	}
	for _, item := range cases {
		t.Run(item.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			method := http.MethodPost
			if strings.HasPrefix(item.path, "/api/v1/supplier/sales-report?") {
				method = http.MethodGet
			}
			item.handler(w, httptest.NewRequest(method, item.path, strings.NewReader(item.body)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSupplierProposalNotPublishedBeforeApproval(t *testing.T) {
	db := setupTestPostgresDB(t)
	defer db.Close()
	repo := repository.NewSupplierRepository(db)
	ctx := context.Background()
	// TEST-02 round 4: SaveFieldReview upserts ON CONFLICT
	// (proposal_id, field_key) (migration 202610170010). Standing
	// proposal_field_reviews rebuilds drop the UNIQUE, so the upsert
	// fails with 42P10. Restore it.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS test02_pfr_proposal_field_key ON proposal_field_reviews(proposal_id, field_key)`); err != nil {
		t.Fatal(err)
	}
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Proposal Test", "Owner", "proposal-"+suffix, "TestProposalPassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM pending_approvals WHERE requested_by=$1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM products WHERE supplier_id=$1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM suppliers WHERE id=$1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, owner.ID)
	})
	h := NewSupplierHandlers(repo, auth.NewJWTService("panel-test"), false)
	source := panelSourceFixture(t, db, supplier.ID)
	niche := "niche-panel-" + suffix
	if _, err := db.Exec(`INSERT INTO niches(id,title,slug,category_slug) VALUES($1,'Panel fixture niche',$1,'digital')`, niche); err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{
		"title": "Pending product", "categorySlug": "digital", "supplierBaseCost": 1000, "stock": 10,
		"schemaVersion": 2, "productType": "simple", "nicheIds": []string{niche},
		"productSchema":       map[string]interface{}{"version": 2, "type": "simple", "attributes": []interface{}{}},
		"variants":            []interface{}{map[string]interface{}{"id": uuid.NewString(), "selection": map[string]interface{}{}, "stock": 10, "supplier_base_cost": 1000, "is_active": true}},
		"supplierWarehouseId": source, "weightGrams": 500, "lengthMm": 100, "widthMm": 100, "heightMm": 100, "fulfillmentMethod": "supplier_direct",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/products/propose", strings.NewReader(string(raw)))
	req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, supplier.ID))
	w := httptest.NewRecorder()
	h.HandleProposeProduct(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		ProductID  string `json:"product_id"`
		ProposalID string `json:"proposal_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM products WHERE supplier_id=$1`, supplier.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("proposal published early: %d %v", count, err)
	}
	if _, err := repo.ApproveProposal(ctx, response.ProposalID, ""); err == nil {
		t.Fatal("unreviewed product was published")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM products WHERE supplier_id=$1`, supplier.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("unreviewed approval left a partial publication", err)
	}
	var stored []byte
	if err := db.QueryRow(`SELECT requested_data FROM pending_approvals WHERE id=$1`, response.ProposalID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var reviewData map[string]interface{}
	if err := json.Unmarshal(stored, &reviewData); err != nil {
		t.Fatal(err)
	}
	var decisions []repository.FieldDecision
	for key, value := range repository.ReviewLeaves(reviewData) {
		decisions = append(decisions, repository.FieldDecision{Key: key, Decision: "approved", ValueHash: repository.ReviewValueHash(value)})
	}
	if err := repo.SaveFieldReview(ctx, response.ProposalID, "test-admin", decisions); err != nil {
		t.Fatal(err)
	}
	productID, err := repo.ApproveProposal(ctx, response.ProposalID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM products WHERE id=$1 AND supplier_id=$2`, productID, supplier.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("approval did not publish: %d %v", count, err)
	}
	var fulfillment string
	if err := db.QueryRow(`SELECT fulfillment_method FROM products WHERE id=$1`, productID).Scan(&fulfillment); err != nil {
		t.Fatal(err)
	}
	if fulfillment != "supplier_direct" {
		t.Fatalf("approved fulfillment = %q, want supplier_direct", fulfillment)
	}
}
