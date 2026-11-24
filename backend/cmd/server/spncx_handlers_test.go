package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func TestSPNCXStaffRoutesKeepUnitCodeOutOfCustomerAccessAndCatalog(t *testing.T) {
	db := opsHTTPDB(t)
	id := uuid.NewString()
	sku, owner, staff, customer := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	product, supplier, warehouse := "spnx-p-"+id[:8], "spnx-s-"+id[:8], "spnx-w-"+id[:8]
	spnc := "PRINT" + strings.ToUpper(id[:7])
	const city = "label-test-city"

	for _, step := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,mobile,full_name,role,warehouse_city,warehouse_city_confirmed) VALUES
			($1,$4,'supplier staff','supplier','',FALSE),($2,$5,'warehouse staff','warehouse_staff',$7,TRUE),($3,$6,'buyer','customer','',FALSE)`,
			[]any{owner, staff, customer, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), fmt.Sprintf("09%09d", (time.Now().UnixNano()+141)%1e9), fmt.Sprintf("09%09d", (time.Now().UnixNano()+631)%1e9), city}},
		{`INSERT INTO suppliers(id,name,owner_user_id,approval_status) VALUES($1,$1,$2,'approved')`, []any{supplier, owner}},
		{`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'test province',$2)`, []any{warehouse, city}},
		{`INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,schema_version,product_type,attribute_schema)
			VALUES($1,$1,$1,$2,1000000,600000,0,0,2,'variable','{}')`, []any{product, supplier}},
		{`INSERT INTO product_variants(id,product_id,spnc,variant_name,inventory_mode,is_default) VALUES($1,$2,$3,'variant','tracked',TRUE)`, []any{sku, product, spnc}},
		{`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_id)
			VALUES($1,$2,$3,$4,'supplier','warehouse',$5,'sellable',1,$6)`, []any{id, product, supplier, sku, "warehouse:" + warehouse, warehouse}},
		{`INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id) VALUES($1,$2,$3,1,'in_stock',$4)`, []any{spnc + "-X0001", sku, product, id}},
	} {
		if _, err := db.Exec(step.query, step.args...); err != nil {
			t.Fatal(err)
		}
	}
	code := spnc + "-X0001"
	jwt := auth.NewJWTService("local SPNCX test key")
	mux := http.NewServeMux()
	installSPNCXRoutes(mux, jwt, repository.NewSupplierRepository(db), repository.NewSPNCXRepository(db))
	call := func(path, principal, role, body string) *httptest.ResponseRecorder {
		t.Helper()
		token, err := jwt.GenerateToken(principal, "", "test", role, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", "label-001")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	query := `{"code":"` + code + `"}`
	warehousePath := "/api/v1/warehouse/inventory/spncx/"
	for _, role := range []string{"customer", "seller", "support_specialist"} {
		denied := call(warehousePath+"lookup", customer, role, query)
		if denied.Code != http.StatusForbidden || strings.Contains(denied.Body.String(), code) {
			t.Fatalf("%s lookup=%d %s", role, denied.Code, denied.Body.String())
		}
	}
	discover := fmt.Sprintf(`{"spnc":%q,"limit":1}`, spnc)
	for _, disallowed := range []struct {
		path, principal, role string
		status                int
	}{
		{warehousePath + "unprinted", customer, "customer", http.StatusForbidden},
		{"/api/v1/supplier/inventory/spncx/unprinted", owner, "supplier", http.StatusOK},
	} {
		w := call(disallowed.path, disallowed.principal, disallowed.role, discover)
		if w.Code != disallowed.status || strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), sku) {
			t.Fatalf("unprinted discovery disclosed out-of-custody stock: %s %d %s", disallowed.role, w.Code, w.Body.String())
		}
	}
	unprinted := call(warehousePath+"unprinted", staff, "warehouse_staff", discover)
	if unprinted.Code != http.StatusOK || !strings.Contains(unprinted.Body.String(), code) || !strings.Contains(unprinted.Body.String(), sku) {
		t.Fatalf("warehouse could not find an issued unprinted code: %d %s", unprinted.Code, unprinted.Body.String())
	}
	w := call(warehousePath+"lookup", staff, "warehouse_staff", query)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), code) || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("warehouse lookup=%d %s", w.Code, w.Body.String())
	}
	w = call("/api/v1/supplier/inventory/spncx/lookup", owner, "supplier", query)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), code) {
		t.Fatalf("supplier lookup=%d %s", w.Code, w.Body.String())
	}
	w = call(warehousePath+"print-jobs", staff, "warehouse_staff", fmt.Sprintf(`{"productId":%q,"variantId":%q,"codes":[%q]}`, product, sku, code))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "CODE128") {
		t.Fatalf("authorized warehouse print=%d %s", w.Code, w.Body.String())
	}

	unprinted = call(warehousePath+"unprinted", staff, "warehouse_staff", discover)
	if unprinted.Code != http.StatusOK || !strings.Contains(unprinted.Body.String(), `"codes":[]`) || strings.Contains(unprinted.Body.String(), code) {
		t.Fatalf("printed unit remained in first-label results: %d %s", unprinted.Code, unprinted.Body.String())
	}

	catalog, stores, pricing := repository.NewCatalogRepository(db), repository.NewStoreRepository(db), repository.NewPricingRepository(db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+product, nil)
	w = httptest.NewRecorder()
	handleProductDetail(w, req, context.Background(), catalog, stores, pricing, product)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "printCount") {
		t.Fatalf("public detail improperly exposed unit data: %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/products?search="+product, nil)
	w = httptest.NewRecorder()
	handleProductsList(w, req, context.Background(), catalog, stores, pricing)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), product) || strings.Contains(w.Body.String(), code) || strings.Contains(w.Body.String(), "printCount") {
		t.Fatalf("public search improperly exposed unit data: %d %s", w.Code, w.Body.String())
	}
}
