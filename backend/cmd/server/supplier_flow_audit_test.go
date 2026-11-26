package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func supplierFlowDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SUPPLIER_FLOW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SUPPLIER_FLOW_TEST_DATABASE_URL must point at a migrated disposable supplier_flow_test database")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

type supplierFlowFixture struct{ user, supplier, product, warehouse string }

func supplierFlowSeed(t *testing.T, db *sql.DB) supplierFlowFixture {
	t.Helper()
	ctx := context.Background()
	repo := repository.NewSupplierRepository(db)
	suffix := uuid.NewString()[:8]
	user, supplier, err := repo.RegisterSupplier(ctx, "Audit supplier", "Audit owner", "audit-"+suffix, "SupplierAudit!2026", fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "audit@example.invalid", "Audit warehouse")
	if err != nil {
		t.Fatal(err)
	}
	if !user.IsActive || supplier.ApprovalStatus != "approved" {
		t.Fatal("new supplier registration must activate the owner and supplier immediately")
	}
	if err = repository.NewCapabilityRepository(db).Grant(ctx, user.ID, "supplier", "supplier_owner", supplier.ID, nil); err != nil {
		t.Fatal(err)
	}
	// TEST-02 round 4: SubmitShipping detects a duplicate tracking
	// number only via the unique index idx_supplier_shipments_tracking
	// (migration 000011). Standing supplier_shipments rebuilds drop the
	// index, so the duplicate UPDATE succeeds silently. Restore it.
	if _, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_shipments_tracking ON supplier_shipments(tracking_number) WHERE tracking_number IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	product := "audit-product-" + suffix
	warehouse := "audit-source-" + suffix
	if _, err = db.Exec(`INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,'Audit source','Test','Test')`, warehouse, supplier.ID); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,length_mm,width_mm,height_mm,weight_grams,measurement_status,warehouse_name,origin_supplier_warehouse_id) VALUES($1,'Audit product',$1,$2,2000,1000,0,20,100,100,100,100,'verified','Audit warehouse',$3)`, product, supplier.ID, warehouse)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity) VALUES($1,$2,$3,'supplier','supplier',$4,'sellable',20)`, uuid.NewString(), product, supplier.ID, "supplier-warehouse:"+warehouse); err != nil {
		t.Fatal(err)
	}
	return supplierFlowFixture{user.ID, supplier.ID, product, warehouse}
}
func supplierFlowAuthorize(t *testing.T, db *sql.DB, user, path, method string) int {
	t.Helper()
	service := auth.NewJWTService(strings.Repeat("s", 32))
	service.SetCapabilityReader(repository.NewCapabilityRepository(db))
	token, err := service.GenerateToken(user, "09123456789", "Audit", "supplier", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	RequireSupplierAccess(service, repository.NewSupplierRepository(db))(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })(w, r)
	return w.Code
}
func TestSupplierFlowPermissionMatrix(t *testing.T) {
	db := supplierFlowDB(t)
	owner := supplierFlowSeed(t, db)
	repo := repository.NewSupplierRepository(db)
	cases := []struct {
		role            string
		allowed, denied []string
	}{
		{"operations", []string{"/inventory", "/returns", "/warehouses", "/warehouses/central", "/inbounds", "/discrepancies", "/products", "/orders", "/customer-returns", "/return-shipping-charges", "/shipping-methods", "/shipping-sla", "/tapin/settings", "/tapin/boxes"}, []string{"/settlement/balance", "/campaigns", "/team", "/addons"}},
		{"orders", []string{"/orders", "/warehouse-orders", "/customer-returns", "/return-shipping-charges", "/shipping-methods", "/shipping-sla", "/tapin/settings", "/tapin/boxes"}, []string{"/inventory", "/settlement/balance", "/team", "/addons"}},
		{"products", []string{"/products", "/products/" + owner.product + "/variants", "/products/implicit-agreements", "/addons", "/catalog/review", "/price-increases", "/warranties"}, []string{"/inventory", "/orders", "/settlement/balance", "/team"}},
		{"finance", []string{"/settlement/balance", "/settlement/history", "/settlement/storage-fees", "/sales-report"}, []string{"/inventory", "/products", "/campaigns", "/team"}},
		{"marketing", []string{"/campaigns", "/products"}, []string{"/inventory", "/orders", "/settlement/balance", "/team"}},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			suffix := uuid.NewString()[:8]
			member, err := repo.CreateMember(context.Background(), owner.supplier, owner.user, "Audit "+tc.role, "audit-member-"+suffix, fmt.Sprintf("09%09d", time.Now().UnixNano()%1e9), "SupplierAudit!2026", tc.role)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range tc.allowed {
				if status := supplierFlowAuthorize(t, db, member.UserID, "/api/v1/supplier"+path, "GET"); status != 204 {
					t.Errorf("allowed %s returned %d", path, status)
				}
			}
			if tc.role != "manager" && supplierFlowAuthorize(t, db, member.UserID, "/api/v1/supplier/tapin/settings", "PUT") != 403 {
				t.Error("non-manager may replace carrier credentials")
			}
			for _, path := range tc.denied {
				if status := supplierFlowAuthorize(t, db, member.UserID, "/api/v1/supplier"+path, "GET"); status != 403 {
					t.Errorf("forbidden %s returned %d", path, status)
				}
			}
		})
	}
}
func TestSupplierFlowOwnershipAndBlockedMembership(t *testing.T) {
	db := supplierFlowDB(t)
	a, b := supplierFlowSeed(t, db), supplierFlowSeed(t, db)
	for _, tc := range []struct {
		user, path string
		want       int
	}{
		{a.user, "/api/v1/supplier/products/" + a.product + "/variants", 204},
		{a.user, "/api/v1/supplier/products/" + b.product + "/variants", 403},
		{a.user, "/api/v1/supplier/products/implicit-agreements", 204},
	} {
		if status := supplierFlowAuthorize(t, db, tc.user, tc.path, "GET"); status != tc.want {
			t.Errorf("path=%s status=%d want=%d", tc.path, status, tc.want)
		}
	}
	if _, err := db.Exec(`UPDATE users SET is_active=FALSE WHERE id=$1`, a.user); err != nil {
		t.Fatal(err)
	}
	if status := supplierFlowAuthorize(t, db, a.user, "/api/v1/supplier/products", "GET"); status != 403 {
		t.Fatal("blocked owner retained access", status)
	}
}

func TestSupplierFlowQCOptionalReferences(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	id := "audit-qc-" + uuid.NewString()[:8]
	if _, err := db.Exec(`INSERT INTO qc_discrepancies(id,supplier_id,expected_quantity,received_quantity,reason) VALUES($1,$2,3,2,'audit shortage')`, id, f.supplier); err != nil {
		t.Fatal(err)
	}
	items, err := repository.NewSupplierRepository(db).GetDiscrepancies(context.Background(), f.supplier)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].InboundID != "" || items[0].ProductID != "" || items[0].ResponseDueAt.Before(time.Now().Add(47*time.Hour)) {
		t.Fatalf("optional QC references or deadline lost: %+v", items)
	}
}

func TestSupplierFlowScheduledInboundTransitions(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	r := repository.NewSupplierRepository(db)
	ctx := context.Background()
	payload := map[string]interface{}{"items": []interface{}{map[string]interface{}{"product_id": f.product, "quantity": float64(2), "carton_count": float64(1)}}}
	destination := "audit-destination-" + uuid.NewString()[:8]
	if _, err := db.Exec(`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'Test','Test')`, destination); err != nil {
		t.Fatal(err)
	}
	payload["warehouseId"], payload["supplierWarehouseId"] = destination, f.warehouse
	first, err := r.CreateInbound(ctx, f.supplier, payload)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRow(`SELECT status FROM inbound_shipments WHERE id=$1`, first).Scan(&status); err != nil || status != "scheduled" {
		t.Fatal(status, err)
	}
	if err = r.DispatchInbound(ctx, "other-supplier", first); !errors.Is(err, repository.ErrInboundProvenance) {
		t.Fatal("cross-supplier dispatch accepted", err)
	}
	if err = r.DispatchInbound(ctx, f.supplier, first); err != nil {
		t.Fatal(err)
	}
	if err = r.CancelInbound(ctx, f.supplier, first); err != nil {
		t.Fatal("fully dispatched cancellation retry must preserve transit units", err)
	}
	if err = db.QueryRow(`SELECT status FROM inbound_shipments WHERE id=$1`, first).Scan(&status); err != nil || status != "in_transit" {
		t.Fatal("dispatched cancellation changed transit status", status, err)
	}
	second, err := r.CreateInbound(ctx, f.supplier, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.CancelInbound(ctx, f.supplier, second); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT status FROM inbound_shipments WHERE id=$1`, second).Scan(&status); err != nil || status != "cancelled" {
		t.Fatal(status, err)
	}
	var sellable, total int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity) FILTER(WHERE status='sellable'),0),SUM(quantity) FROM inventory_balances WHERE product_id=$1`, f.product).Scan(&sellable, &total); err != nil || sellable != 18 || total != 20 {
		t.Fatalf("cancelled reservation / transit conservation sellable=%d total=%d error=%v", sellable, total, err)
	}
}

func TestSupplierFlowTrackingConflictRollsBack(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	ctx := context.Background()
	r := repository.NewSupplierShipmentRepository(db)
	makeOrder := func() string {
		id := "audit-order-" + uuid.NewString()[:8]
		_, err := db.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status,status,fulfillment_method) VALUES($1,$1,'store-central','Audit seller',2000,2000,'Audit buyer','09123456789','Tehran','Tehran','Audit address','1234512345',$1,$2,'paid','processing','supplier_direct')`, id, f.user)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,$2,$3,1,1000,0,1000,2000)`, uuid.NewString(), id, f.product)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status) VALUES($1,$2,$3,'{}','09123456789','Audit buyer','pending')`, "audit-ship-"+uuid.NewString()[:8], id, f.supplier)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := makeOrder(), makeOrder()
	tracking := "AUDIT-" + uuid.NewString()[:12]
	if err := r.SubmitShipping(ctx, f.supplier, a, "post", tracking, f.user); err != nil {
		t.Fatal(err)
	}
	if err := r.SubmitShipping(ctx, f.supplier, b, "post", tracking, f.user); !errors.Is(err, repository.ErrTrackingNumberInUse) {
		t.Fatal("duplicate tracking must return a domain conflict", err)
	}
	var status, orderStatus string
	if err := db.QueryRow(`SELECT ss.status,o.status FROM supplier_shipments ss JOIN orders o ON o.id=ss.order_id WHERE ss.order_id=$1`, b).Scan(&status, &orderStatus); err != nil || status != "pending" || orderStatus != "processing" {
		t.Fatal("failed shipping changed state", status, orderStatus, err)
	}
	body := `{"carrier":"post","trackingNumber":"` + tracking + `"}`
	req := httptest.NewRequest("POST", "/api/v1/supplier/orders/"+b+"/shipping", strings.NewReader(body))
	ctx = context.WithValue(req.Context(), userClaimsKey, &auth.UserClaims{UserID: f.user, Role: "supplier"})
	ctx = context.WithValue(ctx, supplierIDKey, f.supplier)
	w := httptest.NewRecorder()
	handleSupplierSubmitShipping(w, req.WithContext(ctx), ctx, b, r, repository.NewOrderRepository(db), repository.NewSupplierRepository(db), repository.NewTapinRepository(db))
	if w.Code != http.StatusConflict {
		t.Fatalf("tracking conflict HTTP status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSupplierOperationalTrackingRequiresActiveBusiness(t *testing.T) {
	db := supplierFlowDB(t)
	f := supplierFlowSeed(t, db)
	ctx := context.Background()
	orderID := "audit-order-" + uuid.NewString()[:8]
	if _, err := db.Exec(`INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payment_status,status,fulfillment_method) VALUES($1,$1,'store-central','Audit seller',2000,2000,'Audit buyer','09123456789','Tehran','Tehran','Audit address','1234512345',$1,'paid','processing','supplier_direct')`, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status) VALUES($1,$2,$3,'{}','09123456789','Audit buyer','pending')`, "audit-ship-"+uuid.NewString()[:8], orderID, f.supplier); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewOrderRepository(db)
	if allowed, err := repo.CanReadOperationalTracking(ctx, orderID, f.user, "supplier"); err != nil || !allowed {
		t.Fatalf("active supplier read = %v, error %v", allowed, err)
	}
	if _, err := db.Exec(`UPDATE suppliers SET approval_status='suspended' WHERE id=$1`, f.supplier); err != nil {
		t.Fatal(err)
	}
	if allowed, err := repo.CanReadOperationalTracking(ctx, orderID, f.user, "supplier"); err != nil || allowed {
		t.Fatalf("suspended supplier read = %v, error %v", allowed, err)
	}
}

func TestSupplierFlowInboundValidationHTTP(t *testing.T) {
	db := supplierFlowDB(t)
	f, other := supplierFlowSeed(t, db), supplierFlowSeed(t, db)
	h := NewSupplierHandlers(repository.NewSupplierRepository(db), auth.NewJWTService(strings.Repeat("s", 32)), false)
	validItems := []interface{}{map[string]interface{}{"product_id": f.product, "quantity": 2, "carton_count": 1}}
	cases := []struct {
		body map[string]interface{}
		want int
	}{
		{map[string]interface{}{}, 400},
		{map[string]interface{}{"items": []interface{}{map[string]interface{}{"product_id": f.product, "quantity": -1, "carton_count": 1}}}, 400},
		{map[string]interface{}{"items": validItems, "warehouseId": "unknown-audit-warehouse"}, 400},
		{map[string]interface{}{"items": validItems, "dispatchAt": "not-a-date"}, 400},
		{map[string]interface{}{"items": []interface{}{map[string]interface{}{"product_id": other.product, "quantity": 1, "carton_count": 1}}}, 403},
	}
	for _, tc := range cases {
		body, _ := json.Marshal(tc.body)
		req := httptest.NewRequest("POST", "/api/v1/supplier/inbounds/create", strings.NewReader(string(body)))
		ctx := context.WithValue(req.Context(), supplierIDKey, f.supplier)
		w := httptest.NewRecorder()
		h.HandleCreateInbound(w, req.WithContext(ctx))
		if w.Code != tc.want {
			t.Errorf("body=%s status=%d want=%d response=%s", body, w.Code, tc.want, w.Body.String())
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inbound_shipments WHERE supplier_id=$1`, f.supplier).Scan(&n); err != nil || n != 0 {
		t.Fatal("invalid inbound inserted data", n, err)
	}
}
