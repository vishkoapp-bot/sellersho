package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestINV07SKUStockHTTPValidationConflictsAndAudit(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	product := f.product + "-v2"
	if err := repo.CreateDirectProduct(ctx, domain.Product{ID: product, Slug: product, Title: "SKU counted product", CategorySlug: "digital", SupplierID: f.supplier, SupplierBaseCost: 1000, Stock: 0}); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE products SET schema_version=2,product_type='variable',attribute_schema='{}'::jsonb,origin_supplier_warehouse_id=$2 WHERE id=$1`, product, f.warehouse)
	sku, sibling := uuid.NewString(), uuid.NewString()
	foreign := uuid.NewString()
	exec(`INSERT INTO product_variants(id,product_id,spnc,inventory_mode,stock) VALUES($1,$2,'FOREIGN','tracked',0)`, foreign, f.product)
	exec(`INSERT INTO product_variants(id,product_id,spnc,inventory_mode,stock) VALUES($1,$2,'SKU-A','tracked',0),($3,$2,'SKU-B','tracked',0)`, sku, product, sibling)
	warehouse := "stock-" + uuid.NewString()
	exec(`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, warehouse)
	exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,$4,'supplier','supplier',$5,'sellable',5,$6),($7,$2,$3,$8,'supplier','supplier',$5,'sellable',3,$6)`, uuid.NewString(), product, f.supplier, sku, "supplier-warehouse:"+f.warehouse, f.warehouse, uuid.NewString(), sibling)
	exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,$3,$4,'supplier','warehouse',$5,'sellable',5,$6)`, uuid.NewString(), product, f.supplier, sku, "warehouse:"+warehouse, warehouse)
	h := NewSupplierHandlers(repo, auth.NewJWTService("stock-http-fixture"), false)
	request := func(payload string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/"+product+"/stock", strings.NewReader(payload))
		req = req.WithContext(context.WithValue(context.WithValue(req.Context(), supplierIDKey, f.supplier), userClaimsKey, &auth.UserClaims{UserID: f.user}))
		response := httptest.NewRecorder()
		h.HandleUpdateStock(response, req)
		return response
	}
	var beforeMoves int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE product_id=$1`, product).Scan(&beforeMoves); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, payload string
		status        int
	}{
		{"blank reason", `{"variantId":"` + sku + `","new_stock":8,"reason":"  "}`, 400},
		{"missing quantity", `{"variantId":"` + sibling + `","reason":"missing"}`, 400},
		{"null quantity", `{"variantId":"` + sibling + `","new_stock":null,"reason":"missing"}`, 400},
		{"missing SKU", `{"new_stock":8,"reason":"SKU required"}`, 400},
		{"negative", `{"variantId":"` + sku + `","new_stock":-1,"reason":"invalid"}`, 400},
		{"fraction", `{"variantId":"` + sku + `","new_stock":1.5,"reason":"invalid"}`, 400},
		{"overflow", `{"variantId":"` + sku + `","new_stock":2147483648,"reason":"invalid"}`, 400},
		{"malformed SKU", `{"variantId":"invalid","new_stock":8,"reason":"invalid"}`, 400},
		{"foreign SKU", `{"variantId":"` + uuid.NewString() + `","new_stock":8,"reason":"foreign"}`, 404},
		{"other product SKU", `{"variantId":"` + foreign + `","new_stock":8,"reason":"foreign"}`, 404},
		{"below held", `{"variantId":"` + sku + `","new_stock":4,"reason":"below held"}`, 409},
		{"aggregate one over limit", `{"variantId":"` + sku + `","new_stock":2147483645,"reason":"aggregate overflow"}`, 409},
		{"aggregate overflow at SKU maximum", `{"variantId":"` + sku + `","new_stock":2147483647,"reason":"aggregate overflow"}`, 409},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := request(c.payload)
			if w.Code != c.status {
				t.Errorf("HTTP=%d want%d body=%s", w.Code, c.status, w.Body.String())
			}
			if strings.Contains(w.Body.String(), `"productStock"`) {
				t.Errorf("rejected stock request returned productStock: %s", w.Body.String())
			}
		})
	}
	var siblingStock int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, sibling).Scan(&siblingStock); err != nil || siblingStock != 3 {
		t.Errorf("invalid requests mutated sibling stock=%d err=%v", siblingStock, err)
	}
	var selectedStock, afterMoves int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, sku).Scan(&selectedStock); err != nil || selectedStock != 10 {
		t.Errorf("invalid requests mutated selected SKU=%d err=%v", selectedStock, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE product_id=$1`, product).Scan(&afterMoves); err != nil || afterMoves != beforeMoves {
		t.Errorf("invalid requests wrote movements before=%d after=%d err=%v", beforeMoves, afterMoves, err)
	}
	w := request(`{"variantId":"` + sku + `","new_stock":8,"reason":"  counted exact SKU  "}`)
	result := readStockAdjustmentWire(t, w, 8, 11)
	if result.ProductID != product || result.VariantID != sku || result.PreviousStock != 10 || result.Delta != -2 {
		t.Fatalf("incorrect flat adjustment result: %+v", result)
	}
	var projection, local, held int
	var reason, actor string
	if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, product).Scan(&projection); err != nil || projection != *result.ProductStock {
		t.Fatalf("productStock disagrees with authoritative projection: DB=%d HTTP=%d err=%v", projection, *result.ProductStock, err)
	}
	if err := db.QueryRow(`SELECT SUM(quantity) FILTER(WHERE supplier_warehouse_id=$2),SUM(quantity) FILTER(WHERE warehouse_id=$3) FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, sku, f.warehouse, warehouse).Scan(&local, &held); err != nil || local != 3 || held != 5 {
		t.Fatalf("exact SKU locations=%d/%d err=%v", local, held, err)
	}
	if err := db.QueryRow(`SELECT metadata->>'reason',actor_user_id FROM inventory_movements WHERE product_id=$1 AND reference_type='sku_stock_adjustment'`, product).Scan(&reason, &actor); err != nil || reason != "counted exact SKU" || actor != f.user {
		t.Errorf("audit reason=%q actor=%q err=%v", reason, actor, err)
	}
	maxInput := `{"variantId":"` + sku + `","new_stock":2147483644,"reason":"maximum aggregate"}`
	// Projection headroom alone does not let a new adjustment mint more
	// codes than the per-variant SPNCX sequence can represent.
	capped := request(maxInput)
	if capped.Code != http.StatusConflict || !strings.Contains(capped.Body.String(), `"error":"unit_cap_exceeded"`) {
		t.Fatalf("oversized mint HTTP=%d body=%s", capped.Code, capped.Body.String())
	}
	if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, product).Scan(&projection); err != nil || projection != 11 {
		t.Fatalf("rejected mint changed the product projection: DB=%d err=%v", projection, err)
	}
	// Seed a pre-backfill lot just shy of the projection ceiling. Adding
	// one real unit can reach INT_MAX without requiring billions of labels.
	exec(`UPDATE inventory_balances SET quantity=2147483638 WHERE product_id=$1
		AND variant_id=$2 AND supplier_warehouse_id=$3 AND status='sellable'`, product, sku, f.warehouse)
	maximum := readStockAdjustmentWire(t, request(maxInput), 2147483644, 2147483647)
	if maximum.Delta != 1 || maximum.PreviousStock != 2147483643 {
		t.Fatalf("expected adjustment of one unit at the projection ceiling: %+v", maximum)
	}
	w = request(`{"variantId":"` + sku + `","new_stock":8,"reason":"restore counted value"}`)
	if w.Code != 200 {
		t.Fatal("cannot reduce maximum stock")
	}
	readStockAdjustmentWire(t, request(`{"variantId":"`+sibling+`","new_stock":0,"reason":"zero is valid"}`), 0, 8)
	exec(`UPDATE supplier_warehouses SET is_active=FALSE WHERE id=$1`, f.warehouse)
	w = request(`{"variantId":"` + sku + `","new_stock":8,"reason":"origin unavailable"}`)
	if w.Code != 409 {
		t.Errorf("inactive origin HTTP=%d body=%s", w.Code, w.Body.String())
	}
}

// Assert the HTTP contract independently of the repository's internal DTO.
type stockAdjustmentWire struct {
	ProductID           string          `json:"product_id"`
	VariantID           string          `json:"variant_id"`
	PreviousStock       int             `json:"previous_stock"`
	Stock               *int            `json:"stock"`
	Delta               int             `json:"delta"`
	RetiredCodes        []string        `json:"retired_codes"`
	RetiredPrintedCodes []string        `json:"retired_printed_codes"`
	CoverageGap         json.RawMessage `json:"coverage_gap"`
	ProductStock        *int            `json:"productStock"`
}

func readStockAdjustmentWire(t *testing.T, w *httptest.ResponseRecorder, stock, productStock int) stockAdjustmentWire {
	t.Helper()
	var v stockAdjustmentWire
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || w.Code != http.StatusOK {
		t.Fatalf("stock adjustment HTTP=%d err=%v body=%s", w.Code, err, w.Body.String())
	}
	if v.Stock == nil || *v.Stock != stock || v.ProductStock == nil || *v.ProductStock != productStock {
		t.Fatalf("want flat stock=%d and productStock=%d: %s", stock, productStock, w.Body.String())
	}
	if v.RetiredCodes == nil || v.RetiredPrintedCodes == nil || v.CoverageGap == nil {
		t.Fatalf("missing flat SPNCX retirement or coverage fields: %s", w.Body.String())
	}
	return v
}
