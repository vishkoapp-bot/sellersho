package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestCartonCreationAPIAtomicAndRetrySafe(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	root, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "carton_api_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid disposable URL")
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	db, err := testdb.Open(t, u.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE suppliers(id TEXT PRIMARY KEY)`,
		`CREATE TABLE warehouses(id TEXT PRIMARY KEY,is_active BOOL)`,
		`CREATE TABLE supplier_warehouses(id TEXT PRIMARY KEY,supplier_id TEXT,is_active BOOL)`,
		`CREATE TABLE products(id TEXT PRIMARY KEY,supplier_id TEXT,origin_supplier_warehouse_id TEXT)`,
		`CREATE TABLE product_variants(id UUID PRIMARY KEY,product_id TEXT)`,
		`CREATE TABLE shipments(id UUID PRIMARY KEY,shipment_code TEXT UNIQUE,supplier_id TEXT,status TEXT,warehouse_id TEXT,supplier_warehouse_id TEXT,created_at TIMESTAMPTZ DEFAULT NOW())`,
		`CREATE TABLE shipment_items(id UUID PRIMARY KEY DEFAULT gen_random_uuid(),shipment_id UUID REFERENCES shipments(id),variant_id UUID REFERENCES product_variants(id),expected_qty INT CHECK(expected_qty>0),received_qty INT DEFAULT 0)`,
		`CREATE TABLE inventory_reservations(id TEXT PRIMARY KEY)`,
		`INSERT INTO suppliers VALUES('s'); INSERT INTO warehouses VALUES('w',TRUE); INSERT INTO supplier_warehouses VALUES('sw','s',TRUE); INSERT INTO products VALUES('p','s','sw')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../../migrations/202611140005_carton_business_idempotency.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	red, blue := uuid.NewString(), uuid.NewString()
	if _, err := db.Exec(`INSERT INTO product_variants VALUES($1,'p'),($2,'p')`, red, blue); err != nil {
		t.Fatal(err)
	}
	h := &SupplierHandlers{supplierRepo: repository.NewSupplierRepository(db)}
	call := func(key string, items []repository.CartonShipmentItem) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]any{"warehouse_id": "w", "creation_key": key, "items": items})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/shipments", strings.NewReader(string(payload)))
		req = req.WithContext(context.WithValue(req.Context(), supplierIDKey, "s"))
		rec := httptest.NewRecorder()
		h.HandleCreateCartonShipment(rec, req)
		return rec
	}
	counts := func(headers, lines int) {
		t.Helper()
		var h, l, r int
		if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM shipments),(SELECT COUNT(*) FROM shipment_items),(SELECT COUNT(*) FROM inventory_reservations)`).Scan(&h, &l, &r); err != nil || h != headers || l != lines || r != 0 {
			t.Fatalf("atomic counts %d/%d/%d err=%v", h, l, r, err)
		}
	}
	items := []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 1}}
	if rec := call("", items); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing creation key: %d", rec.Code)
	}
	if rec := call("overflow", []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 1}, {VariantID: blue, ExpectedQty: 2147483648}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("overflow: %d", rec.Code)
	}
	counts(0, 0)
	if _, err := db.Exec(`CREATE FUNCTION fail_carton_line() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF EXISTS(SELECT 1 FROM shipment_items WHERE shipment_id=NEW.shipment_id) THEN RAISE EXCEPTION 'injected line failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_line BEFORE INSERT ON shipment_items FOR EACH ROW EXECUTE FUNCTION fail_carton_line()`); err != nil {
		t.Fatal(err)
	}
	if rec := call("fail", []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 1}, {VariantID: blue, ExpectedQty: 1}}); rec.Code == http.StatusOK {
		t.Fatal("injected failure succeeded")
	}
	counts(0, 0)
	first := call("replay", items)
	if first.Code != http.StatusOK {
		t.Fatalf("create: %d %s", first.Code, first.Body.String())
	}
	replay := call("replay", items)
	if replay.Code != http.StatusOK || first.Body.String() != replay.Body.String() {
		t.Fatalf("replay differs: %d", replay.Code)
	}
	if rec := call("replay", []repository.CartonShipmentItem{{VariantID: red, ExpectedQty: 2}}); rec.Code != http.StatusConflict {
		t.Fatalf("key payload conflict: %d want409", rec.Code)
	}
	missingSKU := uuid.NewString()
	if rec := call("replay", []repository.CartonShipmentItem{{VariantID: missingSKU, ExpectedQty: 1}}); rec.Code != http.StatusConflict {
		t.Fatalf("existing key with nonexistent changed SKU: %d want409", rec.Code)
	}
	if rec := call("fresh-missing", []repository.CartonShipmentItem{{VariantID: missingSKU, ExpectedQty: 1}}); rec.Code != http.StatusNotFound {
		t.Fatalf("fresh key with nonexistent SKU: %d want404", rec.Code)
	}
	counts(1, 1)
}
