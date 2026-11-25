package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestVariantWorkspaceSupplierStockCompareAndReplay(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	product := f.product + "-work"
	if err := repo.CreateDirectProduct(ctx, domain.Product{ID: product, Slug: product, Title: "Warehouse counts", CategorySlug: "digital", SupplierID: f.supplier, SupplierBaseCost: 1000}); err != nil {
		t.Fatal(err)
	}
	sku := uuid.NewString()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE products SET schema_version=2,product_type='variable',attribute_schema='{}'::jsonb,origin_supplier_warehouse_id=$2 WHERE id=$1`, product, f.warehouse)
	exec(`INSERT INTO product_variants(id,product_id,spnc,inventory_mode) VALUES($1,$2,'WORK-SKU','tracked')`, sku, product)
	exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id)
		VALUES($1,$2,$3,$4,'supplier','supplier',$5,'sellable',5,$6),
		      ($7,$2,$3,$4,'supplier','supplier',$5,'sales_reserved',2,$6)`,
		uuid.NewString(), product, f.supplier, sku, "supplier-warehouse:"+f.warehouse, f.warehouse, uuid.NewString())

	rows, err := repo.ListWorkspaceVariants(ctx, f.supplier, true, false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("workspace rows=%+v err=%v", rows, err)
	}
	if row := rows[0]; row.ID != sku || row.Sellable != 5 || row.Reserved != 2 || row.Cost != nil ||
		row.ProductCost != 1000 || !row.CanEditStock || row.CanEditCost {
		t.Fatalf("workspace row=%+v", row)
	}
	h := NewSupplierHandlers(repo, nil, false)
	request := func(total, expected int, key string) *httptest.ResponseRecorder {
		t.Helper()
		payload := fmt.Sprintf(`{"variantId":%q,"new_stock":%d,"expected_stock":%d,"reason":"verified count"}`, sku, total, expected)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+product+"/stock", strings.NewReader(payload))
		req.Header.Set("Idempotency-Key", key)
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), supplierIDKey, f.supplier), userClaimsKey, &auth.UserClaims{UserID: f.user}))
		rec := httptest.NewRecorder()
		h.HandleUpdateStock(rec, req)
		return rec
	}
	readStockAdjustmentWire(t, request(4, 5, "workspace-first"), 4, 4)
	if rec := request(6, 5, "workspace-stale"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "variant_workspace_conflict") {
		t.Fatalf("stale workspace adjustment HTTP=%d body=%s", rec.Code, rec.Body)
	}
	readStockAdjustmentWire(t, request(3, 4, "workspace-second"), 3, 3)
	readStockAdjustmentWire(t, request(4, 5, "workspace-first"), 4, 4)
	if rec := request(2, 5, "workspace-first"); rec.Code != http.StatusConflict {
		t.Fatalf("reused operation identity for changed request HTTP=%d body=%s", rec.Code, rec.Body)
	}
	var actual int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, sku).Scan(&actual); err != nil || actual != 3 {
		t.Fatalf("replay changed inventory: stock=%d err=%v", actual, err)
	}
	if status := supplierFlowAuthorize(t, db, f.user, "/api/v1/supplier/products/variant-workspace", http.MethodGet); status != http.StatusNoContent {
		t.Fatalf("supplier cannot reach workspace collection: %d", status)
	}

	h.WithPricingRepository(repository.NewPricingRepository(db))
	price := func(payload string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+product+"/variants/"+sku+"/pricing", strings.NewReader(payload))
		req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, f.supplier))
		rec := httptest.NewRecorder()
		h.HandleUpdateVariantPricing(rec, req, product, sku)
		return rec
	}
	if rec := price(`{"supplierBaseCost":900,"expected":{"cost":null,"productCost":1000}}`); rec.Code != http.StatusOK {
		t.Fatalf("decrease with matching baseline HTTP=%d body=%s", rec.Code, rec.Body)
	}
	if rec := price(`{"supplierBaseCost":800,"expected":{"cost":null,"productCost":1000}}`); rec.Code != http.StatusConflict {
		t.Fatalf("decrease with stale baseline HTTP=%d body=%s", rec.Code, rec.Body)
	}
	const increase = `{"supplierBaseCost":5000,"reason":"wholesale review","expected":{"cost":900,"productCost":1000}}`
	for i := 0; i < 2; i++ {
		if rec := price(increase); rec.Code != http.StatusAccepted {
			t.Fatalf("governed increase after %d submissions HTTP=%d body=%s", i+1, rec.Code, rec.Body)
		}
	}
	rows, err = repo.ListWorkspaceVariants(ctx, f.supplier, true, true)
	if err != nil || len(rows) != 1 || rows[0].Cost == nil || *rows[0].Cost != 900 || rows[0].PendingCost == nil || *rows[0].PendingCost != 5000 {
		t.Fatalf("queued increase changed the current cost: workspace rows=%+v err=%v", rows, err)
	}
}

func TestVariantWorkspaceSellerVisibilityCanonicalPriceAndScope(t *testing.T) {
	rp := variantEffectiveMarginFixture(t)
	const store, product = "cap-store-2", "cap-product-1"
	owner, otherSeller := capacityFixtureUUID("cap-seller-2"), capacityFixtureUUID("cap-seller-3")
	sku := capacityFixtureUUID("cap-sku-1")
	execEffectiveMarginsSQL(t, rp, `INSERT INTO store_variant_visibility (store_id,product_id,variant_id,is_visible) VALUES($1,$2,$3,FALSE)`, store, product, sku)
	execEffectiveMarginsSQL(t, rp, `INSERT INTO store_variant_overrides (store_id,product_id,variant_id,override_value,mode) VALUES($1,$2,$3,700,'fixed')`, store, product, sku)
	t.Cleanup(func() {
		execEffectiveMarginsSQL(t, rp, `DELETE FROM store_variant_visibility WHERE store_id=$1 AND product_id=$2`, store, product)
		execEffectiveMarginsSQL(t, rp, `DELETE FROM store_variant_overrides WHERE store_id=$1 AND product_id=$2`, store, product)
		execEffectiveMarginsSQL(t, rp, `UPDATE product_variants SET sale_price=NULL,sale_from=NULL,sale_to=NULL WHERE id=$1`, sku)
	})
	const sale = 100
	execEffectiveMarginsSQL(t, rp, `UPDATE product_variants SET sale_price=$2,sale_from=NOW()-INTERVAL '1 hour',sale_to=NOW()+INTERVAL '1 hour' WHERE id=$1`, sku, sale)

	request := func(user, method, path, payload string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		req = req.WithContext(context.WithValue(req.Context(), userClaimsKey, &auth.UserClaims{UserID: user, Role: "seller"}))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}
	list := func(w http.ResponseWriter, r *http.Request) {
		handleSellerVariantEffectiveMargins(w, r, rp.pricingRepo, rp.storeRepo, rp.catalogRepo)
	}
	const path = "/api/v1/seller/variant-effective-margins?workspace=1&storeId=" + store
	if rec := request(otherSeller, http.MethodGet, path, "", list); rec.Code != http.StatusForbidden {
		t.Fatalf("another seller read workspace HTTP=%d body=%s", rec.Code, rec.Body)
	}
	rec := request(owner, http.MethodGet, path, "", list)
	var page struct {
		WorkItems []sellerVariantWorkProduct `json:"workItems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); rec.Code != http.StatusOK || err != nil || len(page.WorkItems) == 0 {
		t.Fatalf("workspace HTTP=%d body=%s err=%v", rec.Code, rec.Body, err)
	}
	found := false
	for _, item := range page.WorkItems {
		for _, row := range item.Variants {
			if row.ID != sku {
				continue
			}
			found = true
			if item.ProductID != product || !item.NeedsAttention || row.Visible || row.Price == nil || *row.Price <= sale || row.Margin != nil {
				t.Fatalf("hidden row or canonical cost floor lost: item=%+v sku=%+v", item, row)
			}
		}
	}
	if !found {
		t.Fatal("missing hidden SKU from management view")
	}
	wantAttention := 0
	for _, row := range page.WorkItems {
		if row.NeedsAttention {
			wantAttention++
		}
	}
	countRec := request(owner, http.MethodGet, "/api/v1/seller/variant-effective-margins?workspace=count&storeId="+store, "", list)
	var countPage map[string]int
	if err := json.Unmarshal(countRec.Body.Bytes(), &countPage); countRec.Code != http.StatusOK || err != nil ||
		len(countPage) != 1 || countPage["attentionCount"] != wantAttention {
		t.Fatalf("attention count differs from workspace rows: HTTP=%d body=%s err=%v", countRec.Code, countRec.Body, err)
	}
	write := func(w http.ResponseWriter, r *http.Request) {
		handleSellerVariantOverrides(w, r, rp.pricingRepo, rp.storeRepo)
	}
	marginPath := "/api/v1/seller/variant-overrides?workspace=1"
	makePayload := func(value string, expected string, variant string) string {
		return fmt.Sprintf(`{"storeId":%q,"productId":%q,"variantId":%q,"margin":%s,"expected":{"value":%s}}`, store, product, variant, value, expected)
	}
	if rec := request(otherSeller, http.MethodPut, marginPath, makePayload("15", "null", sku), write); rec.Code != http.StatusForbidden {
		t.Fatalf("another seller changed margin HTTP=%d body=%s", rec.Code, rec.Body)
	}
	if rec := request(owner, http.MethodPut, marginPath, makePayload("null", "null", sku), write); rec.Code != http.StatusOK {
		t.Fatalf("legacy inherited margin HTTP=%d body=%s", rec.Code, rec.Body)
	}
	if override, err := rp.pricingRepo.GetStoreVariantOverride(context.Background(), store, product, sku); err != nil || override == nil || override.Mode != "fixed" {
		t.Fatalf("identical inherited baseline modified the historical row: %+v err=%v", override, err)
	}
	for _, tc := range []struct {
		value, expected string
		code            int
	}{
		{"15", "null", 200}, {"16", "null", 409}, {"null", "null", 409}, {"16", "15", 200}, {"16", "15", 200}, {"null", "16", 200},
	} {
		if rec := request(owner, http.MethodPut, marginPath, makePayload(tc.value, tc.expected, sku), write); rec.Code != tc.code {
			t.Fatalf("margin %s from %s HTTP=%d want=%d body=%s", tc.value, tc.expected, rec.Code, tc.code, rec.Body)
		}
	}
	if rec := request(owner, http.MethodPut, marginPath, makePayload("15", "null", capacityFixtureUUID("cap-sku-8")), write); rec.Code != http.StatusConflict {
		t.Fatalf("unrelated SKU accepted HTTP=%d body=%s", rec.Code, rec.Body)
	}
}
