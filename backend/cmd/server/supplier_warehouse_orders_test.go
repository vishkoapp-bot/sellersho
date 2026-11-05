package main

// Gap 14 regression tests: supplier visibility of warehouse-routed
// orders + the direct-shipment accept step. These run ONLY when
// TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres
// (never the live database); otherwise they skip.
//
// Covered behaviour:
//   - GET /supplier/warehouse-orders returns paid, warehouse-routed
//     orders containing the caller's products, with ONLY the caller's
//     items (wholesale cost view — no other suppliers' items, no
//     retail/margin fields, no customer identity beyond city/province).
//   - Direct orders (caller has a supplier_shipments row) and unpaid
//     warehouse orders never appear there.
//   - POST /supplier/orders/:id/accept moves pending -> processing,
//     conflicts on any other state, 404s for another supplier's order
//     (same scoping family as the shipping endpoint), and never gates
//     SubmitShipping: shipping works from pending (as before) and
//     from processing (after accept).

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func gap14Repos(t *testing.T) (*sql.DB, *repository.SupplierShipmentRepository) {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB HTTP test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`DROP TABLE IF EXISTS supplier_shipment_audit_logs, supplier_shipments, order_items, orders, products CASCADE`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, supplier_id VARCHAR(50), fulfillment_method VARCHAR(50))`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, order_number VARCHAR(30) NOT NULL, status VARCHAR(30) NOT NULL DEFAULT 'processing', payment_status VARCHAR(20) NOT NULL DEFAULT 'pending', fulfillment_method VARCHAR(20) NOT NULL DEFAULT 'warehouse', tracking_code TEXT, shipped_at TIMESTAMPTZ, customer_province VARCHAR(50) NOT NULL DEFAULT '', customer_city VARCHAR(50) NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE order_items (id TEXT PRIMARY KEY, order_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, quantity INT NOT NULL DEFAULT 1, wholesale_cost BIGINT NOT NULL)`,
		`CREATE TABLE supplier_shipments (id VARCHAR(255) PRIMARY KEY, order_id VARCHAR(255) NOT NULL, supplier_id VARCHAR(255) NOT NULL, carrier VARCHAR(100), tracking_number VARCHAR(255), shipped_at TIMESTAMPTZ, status VARCHAR(50) DEFAULT 'pending', created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW())`,
		`CREATE TABLE supplier_shipment_audit_logs (id VARCHAR(255) PRIMARY KEY, shipment_id VARCHAR(255), actor_user_id VARCHAR(255), action VARCHAR(100) NOT NULL, timestamp TIMESTAMPTZ DEFAULT NOW())`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	seed := []string{
		`INSERT INTO products (id,title,supplier_id,fulfillment_method) VALUES ('prod-a-wh','کالای انباری الف','sup-a','warehouse')`,
		`INSERT INTO products (id,title,supplier_id,fulfillment_method) VALUES ('prod-a-direct','کالای مستقیم الف','sup-a','supplier_direct')`,
		`INSERT INTO products (id,title,supplier_id,fulfillment_method) VALUES ('prod-b-wh','کالای انباری ب','sup-b','warehouse')`,
		// Paid mixed-supplier warehouse order: A has 2x1000, B has 1x2000.
		`INSERT INTO orders (id,order_number,status,payment_status,customer_province,customer_city) VALUES ('ord-wh-mixed','ORD-WH-1','processing','paid','قم','قم')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-1','ord-wh-mixed','prod-a-wh',2,1000)`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-2','ord-wh-mixed','prod-b-wh',1,2000)`,
		// Second paid warehouse order, A only.
		`INSERT INTO orders (id,order_number,status,payment_status,customer_province,customer_city) VALUES ('ord-wh-a2','ORD-WH-2','shipped','paid','تهران','تهران')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-3','ord-wh-a2','prod-a-wh',3,1000)`,
		// Paid direct order for A (has a pending shipment row).
		`INSERT INTO orders (id,order_number,status,payment_status) VALUES ('ord-direct','ORD-D-1','processing','paid')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-4','ord-direct','prod-a-direct',1,5000)`,
		`INSERT INTO supplier_shipments (id,order_id,supplier_id,status) VALUES ('ship-1','ord-direct','sup-a','pending')`,
		// Second paid direct order for A, shipped later without accept.
		`INSERT INTO orders (id,order_number,status,payment_status) VALUES ('ord-direct2','ORD-D-2','processing','paid')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-5','ord-direct2','prod-a-direct',1,5000)`,
		`INSERT INTO supplier_shipments (id,order_id,supplier_id,status) VALUES ('ship-2','ord-direct2','sup-a','pending')`,
		// Unpaid warehouse order with A's product: invisible.
		`INSERT INTO orders (id,order_number,status,payment_status) VALUES ('ord-unpaid','ORD-U-1','processing','pending')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-6','ord-unpaid','prod-a-wh',1,1000)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
	return db, repository.NewSupplierShipmentRepository(db)
}

func gap14Ctx(supplierID string) context.Context {
	ctx := context.WithValue(context.Background(), userClaimsKey, &auth.UserClaims{UserID: "user-" + supplierID, Role: "supplier"})
	return context.WithValue(ctx, supplierIDKey, supplierID)
}

type gap14WarehouseItem struct {
	ProductID     string `json:"productId"`
	ProductTitle  string `json:"productTitle"`
	Quantity      int    `json:"quantity"`
	WholesaleCost int64  `json:"wholesaleCost"`
	LineTotal     int64  `json:"lineTotal"`
}

type gap14WarehouseOrder struct {
	OrderID     string               `json:"orderId"`
	OrderNumber string               `json:"orderNumber"`
	Status      string               `json:"status"`
	Province    string               `json:"province"`
	City        string               `json:"city"`
	Items       []gap14WarehouseItem `json:"items"`
	ItemsTotal  int64                `json:"itemsTotal"`
}

func gap14GetWarehouseOrders(t *testing.T, repo *repository.SupplierShipmentRepository, supplierID string) (*httptest.ResponseRecorder, []gap14WarehouseOrder, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/supplier/warehouse-orders", nil).WithContext(gap14Ctx(supplierID))
	rec := httptest.NewRecorder()
	handleGetSupplierWarehouseOrders(rec, req, req.Context(), repo, nil)
	var body struct {
		Success bool                  `json:"success"`
		Orders  []gap14WarehouseOrder `json:"orders"`
		Total   int                   `json:"total"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode warehouse orders: %v", err)
		}
	}
	return rec, body.Orders, body.Total
}

func TestGap14WarehouseOrdersScopeAndFields(t *testing.T) {
	_, repo := gap14Repos(t)
	rec, orders, total := gap14GetWarehouseOrders(t, repo, "sup-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if total != 2 || len(orders) != 2 {
		t.Fatalf("total = %d, orders = %d, want 2/2 (direct + unpaid orders excluded)", total, len(orders))
	}
	var mixed *gap14WarehouseOrder
	for i := range orders {
		if orders[i].OrderID == "ord-direct" || orders[i].OrderID == "ord-unpaid" {
			t.Fatalf("unexpected order in warehouse list: %s", orders[i].OrderID)
		}
		if orders[i].OrderID == "ord-wh-mixed" {
			mixed = &orders[i]
		}
	}
	if mixed == nil {
		t.Fatalf("mixed warehouse order missing")
	}
	if len(mixed.Items) != 1 || mixed.Items[0].ProductID != "prod-a-wh" {
		t.Fatalf("mixed order items = %+v, want only supplier A's item", mixed.Items)
	}
	if mixed.Items[0].WholesaleCost != 1000 || mixed.Items[0].Quantity != 2 || mixed.Items[0].LineTotal != 2000 || mixed.ItemsTotal != 2000 {
		t.Fatalf("mixed item totals wrong: %+v (itemsTotal %d)", mixed.Items[0], mixed.ItemsTotal)
	}
	if mixed.Province != "قم" || mixed.City != "قم" {
		t.Fatalf("destination = %s/%s, want قم/قم", mixed.Province, mixed.City)
	}
	// Secrecy: no other supplier's product, no retail/margin/customer fields.
	body := rec.Body.String()
	for _, forbidden := range []string{"prod-b-wh", "کالای انباری ب", "finalUnitPrice", "sellerMargin", "platformFee", "customerName", "customerPhone", "customerAddress"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("warehouse-orders body leaks %q: %s", forbidden, body)
		}
	}

	// Supplier B sees the same mixed order with only B's item, and not A's second order.
	_, ordersB, totalB := gap14GetWarehouseOrders(t, repo, "sup-b")
	if totalB != 1 || len(ordersB) != 1 || ordersB[0].OrderID != "ord-wh-mixed" {
		t.Fatalf("supplier B view wrong: total %d orders %+v", totalB, ordersB)
	}
	if len(ordersB[0].Items) != 1 || ordersB[0].Items[0].ProductID != "prod-b-wh" || ordersB[0].ItemsTotal != 2000 {
		t.Fatalf("supplier B items wrong: %+v", ordersB[0].Items)
	}
}

func gap14Accept(t *testing.T, repo *repository.SupplierShipmentRepository, supplierID, orderID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/orders/"+orderID+"/accept", nil)
	rec := httptest.NewRecorder()
	handleSupplierAcceptOrder(rec, req, gap14Ctx(supplierID), orderID, repo)
	return rec
}

func gap14ShipmentStatus(t *testing.T, db *sql.DB, shipmentID string) string {
	t.Helper()
	var status string
	if err := db.QueryRow(`SELECT status FROM supplier_shipments WHERE id = $1`, shipmentID).Scan(&status); err != nil {
		t.Fatalf("read shipment status: %v", err)
	}
	return status
}

func TestGap14AcceptThenShipStillWorks(t *testing.T) {
	db, repo := gap14Repos(t)
	ctx := context.Background()

	rec := gap14Accept(t, repo, "sup-a", "ord-direct")
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if got := gap14ShipmentStatus(t, db, "ship-1"); got != "processing" {
		t.Fatalf("shipment status after accept = %q, want processing", got)
	}

	// Accepting again conflicts: the shipment is no longer pending.
	rec = gap14Accept(t, repo, "sup-a", "ord-direct")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second accept status = %d, want 409, body %s", rec.Code, rec.Body.String())
	}

	// Shipping from 'processing' works exactly as shipping from 'pending' did.
	if err := repo.SubmitShipping(ctx, "sup-a", "ord-direct", "پست پیشتاز", "TRK-14-1", "user-sup-a"); err != nil {
		t.Fatalf("SubmitShipping after accept: %v", err)
	}
	if got := gap14ShipmentStatus(t, db, "ship-1"); got != "shipped" {
		t.Fatalf("shipment status after shipping = %q, want shipped", got)
	}

	// Accept on a shipped shipment conflicts.
	rec = gap14Accept(t, repo, "sup-a", "ord-direct")
	if rec.Code != http.StatusConflict {
		t.Fatalf("accept after ship status = %d, want 409, body %s", rec.Code, rec.Body.String())
	}

	// Accept is NOT a gate: a never-accepted pending shipment ships as before.
	if err := repo.SubmitShipping(ctx, "sup-a", "ord-direct2", "پست پیشتاز", "TRK-14-2", "user-sup-a"); err != nil {
		t.Fatalf("SubmitShipping without accept: %v", err)
	}
	if got := gap14ShipmentStatus(t, db, "ship-2"); got != "shipped" {
		t.Fatalf("shipment 2 status = %q, want shipped", got)
	}
}

func TestGap14AcceptOtherSupplierNotFound(t *testing.T) {
	_, repo := gap14Repos(t)
	rec := gap14Accept(t, repo, "sup-b", "ord-direct")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("accept of another supplier's order = %d, want 404 (shipping endpoint scoping), body %s", rec.Code, rec.Body.String())
	}
}
