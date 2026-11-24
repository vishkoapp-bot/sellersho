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

// G4 (Batch 4b): bulk origin assign + the stock-origin gate it unblocks.
// Fixture: one supplier, one active warehouse, five products — three
// NULL-origin supplier_direct, one warehouse-fulfilment, one already
// originated — plus a schema-v2 product with a tracked variant for the
// stock-gate half.
type bulkOriginFixture struct {
	db        *sql.DB
	repo      *repository.SupplierRepository
	handler   *SupplierHandlers
	ownerID   string
	supplier  string
	warehouse string
	direct    []string
	warehouseProduct string
	originated string
	trackedProduct string
	trackedVariant string
}

func setupBulkOriginFixture(t *testing.T) *bulkOriginFixture {
	t.Helper()
	db := setupTestPostgresDB(t)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	suffix := uuid.NewString()[:8]
	owner, supplier, err := repo.RegisterSupplier(ctx, "Bulk Origin Test", "Owner", "bulk-"+suffix, "TestBulkPassword!", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "", "Tehran")
	if err != nil {
		t.Fatal(err)
	}
	f := &bulkOriginFixture{db: db, repo: repo, ownerID: owner.ID, supplier: supplier.ID}
	f.handler = NewSupplierHandlers(repo, auth.NewJWTService("bulk-fixture"), false)
	f.warehouse = panelSourceFixture(t, db, supplier.ID)
	mk := func(id string, fulfilment string) {
		p := domain.Product{ID: id, Slug: id, Title: "Bulk product " + id, CategorySlug: "digital", SupplierID: supplier.ID, SupplierBaseCost: 1000, Stock: 5}
		if fulfilment != "" {
			p.FulfillmentMethod = fulfilment
		}
		if err := repo.CreateDirectProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("bulk-direct-%d-%s", i, suffix)
		mk(id, "")
		f.direct = append(f.direct, id)
	}
	f.warehouseProduct = "bulk-warehouse-" + suffix
	mk(f.warehouseProduct, "warehouse")
	f.originated = "bulk-originated-" + suffix
	mk(f.originated, "")
	if err := repo.UpdateProductOrigin(ctx, supplier.ID, f.originated, f.warehouse); err != nil {
		t.Fatal(err)
	}
	// Tracked (schema v2) product + tracked variant for the stock-gate half.
	f.trackedProduct = "bulk-tracked-" + suffix
	mk(f.trackedProduct, "")
	if _, err := db.Exec(`UPDATE products SET schema_version=2, product_type='variable', attribute_schema='{}'::jsonb WHERE id=$1`, f.trackedProduct); err != nil {
		t.Fatal(err)
	}
	f.trackedVariant = uuid.NewString()
	if _, err := db.Exec(`INSERT INTO product_variants(id, product_id, stock, inventory_mode, spnc) VALUES($1,$2,0,'tracked',$3)`, f.trackedVariant, f.trackedProduct, "BLK-"+strings.ToUpper(suffix)); err != nil {
		t.Fatal(err)
	}
	all := append(append([]string{}, f.direct...), f.warehouseProduct, f.originated, f.trackedProduct)
	t.Cleanup(func() {
		for _, id := range all {
			_, _ = db.Exec(`DELETE FROM inventory_movements WHERE product_id = $1`, id)
			_, _ = db.Exec(`DELETE FROM inventory_balances WHERE product_id = $1`, id)
			_, _ = db.Exec(`DELETE FROM product_variants WHERE product_id = $1`, id)
			_, _ = db.Exec(`DELETE FROM products WHERE id = $1`, id)
		}
		_, _ = db.Exec(`DELETE FROM supplier_warehouses WHERE supplier_id = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM suppliers WHERE id = $1`, supplier.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID)
	})
	return f
}

func (f *bulkOriginFixture) originOf(t *testing.T, productID string) sql.NullString {
	t.Helper()
	var origin sql.NullString
	if err := f.db.QueryRow(`SELECT origin_supplier_warehouse_id FROM products WHERE id = $1`, productID).Scan(&origin); err != nil {
		t.Fatal(err)
	}
	return origin
}

func (f *bulkOriginFixture) callBulk(t *testing.T, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/v1/supplier/products/origin/bulk", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), supplierIDKey, f.supplier)
	r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: f.ownerID}))
	w := httptest.NewRecorder()
	f.handler.HandleBulkAssignProductOrigin(w, r)
	return w
}

func decodeBulk(t *testing.T, w *httptest.ResponseRecorder) (int, map[string]interface{}) {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bulk response not JSON: %d %s: %v", w.Code, w.Body.String(), err)
	}
	return w.Code, body
}

func TestSupplierBulkAssignProductOrigin(t *testing.T) {
	f := setupBulkOriginFixture(t)

	// Foreign warehouse: 404, zero rows touched.
	if w := f.callBulk(t, http.MethodPost, `{"warehouseId":"foreign-warehouse"}`); w.Code != http.StatusNotFound {
		t.Fatalf("foreign warehouse status=%d body=%s", w.Code, w.Body.String())
	}
	for _, id := range f.direct {
		if o := f.originOf(t, id); o.Valid {
			t.Fatalf("foreign warehouse touched %s: origin=%s", id, o.String)
		}
	}

	// Inactive warehouse: 404, zero rows touched.
	inactive := "bulk-inactive-" + uuid.NewString()[:8]
	if _, err := f.db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name,is_active) VALUES($1,$2,$1,'Test','Test',false)`, inactive, f.supplier); err != nil {
		t.Fatal(err)
	}
	if w := f.callBulk(t, http.MethodPost, fmt.Sprintf(`{"warehouseId":%q}`, inactive)); w.Code != http.StatusNotFound {
		t.Fatalf("inactive warehouse status=%d body=%s", w.Code, w.Body.String())
	}
	for _, id := range f.direct {
		if o := f.originOf(t, id); o.Valid {
			t.Fatalf("inactive warehouse touched %s: origin=%s", id, o.String)
		}
	}

	// Empty warehouseId: 400. Bad JSON: 400. Wrong method: 405.
	if w := f.callBulk(t, http.MethodPost, `{"warehouseId":""}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty warehouseId status=%d body=%s", w.Code, w.Body.String())
	}
	if w := f.callBulk(t, http.MethodPost, `{bad json`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON status=%d body=%s", w.Code, w.Body.String())
	}
	if w := f.callBulk(t, http.MethodGet, ``); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}

	// productIds subset: only the named id is assigned.
	code, body := decodeBulk(t, f.callBulk(t, http.MethodPost, fmt.Sprintf(`{"warehouseId":%q,"productIds":[%q]}`, f.warehouse, f.direct[0])))
	if code != http.StatusOK || body["assigned"] != float64(1) || body["warehouseId"] != f.warehouse {
		t.Fatalf("subset status=%d body=%v", code, body)
	}
	if o := f.originOf(t, f.direct[0]); !o.Valid || o.String != f.warehouse {
		t.Fatalf("subset did not assign %s: %+v", f.direct[0], o)
	}
	if o := f.originOf(t, f.direct[1]); o.Valid {
		t.Fatalf("subset touched %s: origin=%s", f.direct[1], o.String)
	}

	// productIds naming a foreign / nonexistent id assigns 0, no error.
	code, body = decodeBulk(t, f.callBulk(t, http.MethodPost, fmt.Sprintf(`{"warehouseId":%q,"productIds":["no-such-product"]}`, f.warehouse)))
	if code != http.StatusOK || body["assigned"] != float64(0) {
		t.Fatalf("foreign id status=%d body=%v", code, body)
	}

	// Omitted productIds: assigns ALL remaining NULL-origin supplier_direct
	// products (3 left: direct[1], direct[2], and the tracked product, which
	// is supplier_direct too) — the warehouse-fulfilment and
	// already-originated rows are untouched.
	code, body = decodeBulk(t, f.callBulk(t, http.MethodPost, fmt.Sprintf(`{"warehouseId":%q}`, f.warehouse)))
	if code != http.StatusOK || body["assigned"] != float64(3) {
		t.Fatalf("omitted productIds status=%d body=%v", code, body)
	}
	for _, id := range append(append([]string{}, f.direct...), f.trackedProduct) {
		if o := f.originOf(t, id); !o.Valid || o.String != f.warehouse {
			t.Fatalf("omitted call did not assign %s: %+v", id, o)
		}
	}
	if o := f.originOf(t, f.warehouseProduct); o.Valid {
		t.Fatalf("warehouse-fulfilment product touched: origin=%s", o.String)
	}
	if o := f.originOf(t, f.originated); !o.Valid || o.String != f.warehouse {
		t.Fatalf("already-originated product changed: %+v", o)
	}

	// Idempotent: a second identical call assigns 0.
	code, body = decodeBulk(t, f.callBulk(t, http.MethodPost, fmt.Sprintf(`{"warehouseId":%q}`, f.warehouse)))
	if code != http.StatusOK || body["assigned"] != float64(0) {
		t.Fatalf("second call status=%d body=%v", code, body)
	}
}

func TestSupplierStockOriginGateHTTP(t *testing.T) {
	f := setupBulkOriginFixture(t)
	h := f.handler

	callStock := func(productID, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+productID+"/stock", strings.NewReader(body))
		ctx := context.WithValue(r.Context(), supplierIDKey, f.supplier)
		r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: f.ownerID}))
		w := httptest.NewRecorder()
		h.HandleUpdateStock(w, r)
		return w
	}
	callOrigin := func(productID, warehouseID string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+productID+"/origin", strings.NewReader(fmt.Sprintf(`{"supplierWarehouseId":%q}`, warehouseID)))
		ctx := context.WithValue(r.Context(), supplierIDKey, f.supplier)
		r = r.WithContext(context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: f.ownerID}))
		w := httptest.NewRecorder()
		h.HandleUpdateProductOrigin(w, r, productID)
		return w
	}
	product := f.direct[0]

	// NULL origin → 409 ErrStockOriginRequired, verbatim Persian message.
	if w := callStock(product, `{"new_stock":8,"reason":"counted correction"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "انبار فعال متعلق به تأمین‌کننده") {
		t.Fatalf("NULL origin stock status=%d body=%s", w.Code, w.Body.String())
	}
	// Tracked variant path: same gate, same 409.
	if w := callStock(f.trackedProduct, fmt.Sprintf(`{"new_stock":3,"reason":"counted correction","variantId":%q}`, f.trackedVariant)); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "انبار فعال متعلق به تأمین‌کننده") {
		t.Fatalf("tracked NULL origin stock status=%d body=%s", w.Code, w.Body.String())
	}

	// Set the origin via PUT origin → the same stock call succeeds (200).
	if w := callOrigin(product, f.warehouse); w.Code != http.StatusOK {
		t.Fatalf("PUT origin status=%d body=%s", w.Code, w.Body.String())
	}
	// Success needs the sellable-balance fixture (same shape as
	// TestSupplierStockAggregateHTTPReturnsVerifiedQuantity).
	central := "bulk-central-" + uuid.NewString()[:8]
	for _, q := range []struct {
		sql  string
		args []interface{}
	}{
		{`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, []interface{}{central}},
		{`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,'supplier','supplier',$4,'sellable',5,$5)`, []interface{}{uuid.NewString(), product, f.supplier, "supplier-warehouse:" + f.warehouse, f.warehouse}},
		{`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,$3,'supplier','warehouse',$4,'sellable',5,$5)`, []interface{}{uuid.NewString(), product, f.supplier, "warehouse:" + central, central}},
	} {
		if _, err := f.db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	w := callStock(product, `{"new_stock":8,"reason":"counted correction"}`)
	var result struct {
		Stock        int `json:"stock"`
		ProductStock int `json:"productStock"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result.Stock != 8 || result.ProductStock != 8 {
		t.Fatalf("origin set stock status=%d result=%+v err=%v body=%s", w.Code, result, err, w.Body.String())
	}
	// Tracked path with the origin set also succeeds past the gate.
	if w := callOrigin(f.trackedProduct, f.warehouse); w.Code != http.StatusOK {
		t.Fatalf("PUT tracked origin status=%d body=%s", w.Code, w.Body.String())
	}
	w = callStock(f.trackedProduct, fmt.Sprintf(`{"new_stock":3,"reason":"counted correction","variantId":%q}`, f.trackedVariant))
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result.Stock != 3 {
		t.Fatalf("tracked origin set stock status=%d result=%+v err=%v body=%s", w.Code, result, err, w.Body.String())
	}

	// Origin pointing at an INACTIVE warehouse still 409s.
	if _, err := f.db.Exec(`UPDATE supplier_warehouses SET is_active=false WHERE id=$1`, f.warehouse); err != nil {
		t.Fatal(err)
	}
	if w := callStock(product, `{"new_stock":9,"reason":"counted correction"}`); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "انبار فعال متعلق به تأمین‌کننده") {
		t.Fatalf("inactive-warehouse origin stock status=%d body=%s", w.Code, w.Body.String())
	}
}
