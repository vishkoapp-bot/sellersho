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

	"github.com/drup/platform/internal/testdb"

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
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`DROP TABLE IF EXISTS supplier_shipment_audit_logs, supplier_shipments, order_items, order_item_origins, orders, products, suppliers CASCADE`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, supplier_id VARCHAR(50), fulfillment_method VARCHAR(50) DEFAULT 'warehouse'::character varying ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_version INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			brand text NOT NULL DEFAULT ''::text,
			category_slug character varying(100),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			description text,
			height_mm integer,
			image_url text,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			is_active boolean NOT NULL DEFAULT true,
			is_featured boolean DEFAULT false,
			is_original boolean NOT NULL DEFAULT true,
			is_special_deal boolean DEFAULT false,
			length_mm integer,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
			measurement_source character varying(20),
			origin_supplier_warehouse_id text,
			original_price bigint,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			platform_margin bigint,
			preparation_days integer DEFAULT 1,
			product_type text NOT NULL DEFAULT 'legacy'::text,
			rating numeric(3,2) NOT NULL DEFAULT 0,
			reviews_count integer NOT NULL DEFAULT 0,
			schema_version integer NOT NULL DEFAULT 0,
			shipping_class text NOT NULL DEFAULT 'standard'::text,
			slug character varying(255),
			specs jsonb DEFAULT '{}'::jsonb,
			spn character varying(20),
			spn_number bigint,
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			supplier_warranty character varying(100),
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			tax_class text NOT NULL DEFAULT 'standard'::text,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60),
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, order_number VARCHAR(30) NOT NULL, status VARCHAR(30) NOT NULL DEFAULT 'processing', discount_code TEXT NOT NULL DEFAULT '', payment_status VARCHAR(20) NOT NULL DEFAULT 'pending', fulfillment_method VARCHAR(20) NOT NULL DEFAULT 'warehouse', tracking_code VARCHAR(50), shipped_at TIMESTAMPTZ, customer_province VARCHAR(50) NOT NULL DEFAULT '', customer_city VARCHAR(50) NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			payable_amount BIGINT NOT NULL DEFAULT 0,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			customer_user_id UUID,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			customer_address text,
			customer_full_name character varying(100),
			customer_phone character varying(20),
			customer_postal_code character varying(20),
			discount_amount bigint NOT NULL DEFAULT 0,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
			gateway_amount bigint NOT NULL DEFAULT 0,
			idempotency_key text,
			inventory_captured boolean NOT NULL DEFAULT false,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			packed_at timestamp with time zone,
			payment_expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval),
			payment_method character varying(20) NOT NULL DEFAULT 'cash'::character varying,
			platform_addon_amount bigint NOT NULL DEFAULT 0,
			platform_profit bigint NOT NULL DEFAULT 0,
			profit_released boolean DEFAULT false,
			profit_released_at timestamp with time zone,
			referral_commission bigint DEFAULT 0,
			referral_commission_released boolean DEFAULT false,
			referrer_seller_id character varying(50),
			return_window_ends_at timestamp with time zone,
			seller_discount_amount bigint NOT NULL DEFAULT 0,
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			store_id character varying(50),
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_number character varying(100),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE order_items (id TEXT  DEFAULT gen_random_uuid() PRIMARY KEY, order_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, quantity INT NOT NULL DEFAULT 1, wholesale_cost BIGINT NOT NULL, variant_id UUID, variant_name TEXT, variant_spnc TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			discount_amount bigint NOT NULL DEFAULT 0,
			final_unit_price bigint,
			platform_fee bigint,
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text,
			seller_margin bigint)`,
		`CREATE TABLE order_item_origins (order_item_id TEXT NOT NULL, leg_index INT NOT NULL DEFAULT 0, origin_type VARCHAR(20) NOT NULL,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id text,
			order_id character varying(255),
			origin_label text,
			quantity integer,
			supplier_id character varying(255),
			supplier_warehouse_id text,
			warehouse_id text)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY, owner_user_id TEXT,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			approval_status VARCHAR(20) NOT NULL DEFAULT 'approved',
			name VARCHAR(200) NOT NULL DEFAULT '',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			warehouse_city VARCHAR(255) NOT NULL DEFAULT 'تهران',
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			approved_at timestamp with time zone,
			approved_by character varying(50),
			badge character varying(50),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			default_markup_type character varying(10),
			default_markup_value numeric(10,2),
			delivery_score integer DEFAULT 100,
			description text,
			logo_url text,
			rating numeric(3,2) DEFAULT 5.00,
			rejection_reason text,
			signed_contract_version integer NOT NULL DEFAULT 1,
			supports_direct_shipping boolean DEFAULT false,
			suspended_at timestamp with time zone,
			suspended_by text,
			suspension_reason text,
			tapin_shop_id text,
			tapin_token text,
			total_products integer DEFAULT 0,
			warehouse_city_confirmed boolean NOT NULL DEFAULT false,
			warehouse_city_source character varying(20))`,
		`CREATE TABLE supplier_shipments (id VARCHAR(255) PRIMARY KEY, order_id VARCHAR(255) NOT NULL, supplier_id VARCHAR(255) NOT NULL, carrier VARCHAR(100), tracking_number VARCHAR(255), shipped_at TIMESTAMPTZ, delivered_at TIMESTAMPTZ, status VARCHAR(50) DEFAULT 'pending', shipping_address JSONB NOT NULL DEFAULT '{}', customer_phone TEXT NOT NULL DEFAULT '', customer_name TEXT NOT NULL DEFAULT '', supplier_notes TEXT, created_at TIMESTAMPTZ DEFAULT NOW(), updated_at TIMESTAMPTZ DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			send_price bigint,
			tapin_box_id text,
			tapin_last_status text,
			tapin_register_attempted_at timestamp with time zone,
			tapin_registered_at timestamp with time zone,
			tapin_service text)`,
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
		`INSERT INTO products (id,title,supplier_id,fulfillment_method) VALUES ('prod-b-direct','کالای مستقیم ب','sup-b','supplier_direct')`,
		`INSERT INTO suppliers (id,owner_user_id) VALUES ('sup-a','user-sup-a'),('sup-b','user-sup-b')`,
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
		// Paid direct order for B (scoping witness: A must never see it).
		`INSERT INTO orders (id,order_number,status,payment_status) VALUES ('ord-direct-b','ORD-D-B','processing','paid')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-7','ord-direct-b','prod-b-direct',1,7000)`,
		`INSERT INTO supplier_shipments (id,order_id,supplier_id,status) VALUES ('ship-3','ord-direct-b','sup-b','pending')`,
		// Unpaid warehouse order with A's product: invisible.
		`INSERT INTO orders (id,order_number,status,payment_status) VALUES ('ord-unpaid','ORD-U-1','processing','pending')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity,wholesale_cost) VALUES ('it-6','ord-unpaid','prod-a-wh',1,1000)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
	if _, err := db.Exec(`ALTER TABLE order_item_origins ADD COLUMN IF NOT EXISTS quantity INT, ADD COLUMN IF NOT EXISTS supplier_id TEXT;
		UPDATE order_item_origins x SET quantity=oi.quantity,supplier_id=p.supplier_id FROM order_items oi JOIN products p ON p.id=oi.product_id WHERE oi.id=x.order_item_id`); err != nil {
		t.Fatal(err)
	}
	return db, repository.NewSupplierShipmentRepository(db)
}

// gap14Ctx builds a request context with the given legacy Role string and,
// unless supplierID is "", the middleware-resolved supplierIDKey. The
// customer-role case mirrors the real test account (09331796649):
// Role "customer" with capabilities customer+seller+supplier.
func gap14Ctx(supplierID, role string) context.Context {
	claims := &auth.UserClaims{UserID: "user-" + supplierID, Role: role}
	switch role {
	case "customer":
		claims.Capabilities = []string{"customer", "seller", "supplier"}
	case "admin":
		claims.Capabilities = []string{"admin"}
	case "super_admin":
		claims.Capabilities = []string{"super_admin"}
	}
	ctx := context.WithValue(context.Background(), userClaimsKey, claims)
	if supplierID == "" {
		return ctx
	}
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/supplier/warehouse-orders", nil).WithContext(gap14Ctx(supplierID, "supplier"))
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
	handleSupplierAcceptOrder(rec, req, gap14Ctx(supplierID, "supplier"), orderID, repo)
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

type gap14SupplierOrder struct {
	OrderID     string `json:"orderId"`
	OrderNumber string `json:"orderNumber"`
	Status      string `json:"status"`
	Items       []struct {
		ProductID string `json:"productId"`
	} `json:"items"`
}

// gap14GetSupplierOrders calls handleGetSupplierOrders with the given context
// and raw URL (query string carries ?supplierId= for the admin cases).
// supplierRepo/orderRepo are nil on purpose: scoping must come from the
// middleware context, never from the getSupplierIDFromContext DB fallback.
func gap14GetSupplierOrders(t *testing.T, repo *repository.SupplierShipmentRepository, ctx context.Context, rawURL string) (*httptest.ResponseRecorder, []gap14SupplierOrder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, rawURL, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handleGetSupplierOrders(rec, req, req.Context(), repo, nil, nil)
	var body struct {
		Success bool                 `json:"success"`
		Orders  []gap14SupplierOrder `json:"orders"`
	}
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode supplier orders: %v", err)
		}
	}
	return rec, body.Orders
}

func gap14OrderIDs(orders []gap14SupplierOrder) map[string]bool {
	ids := map[string]bool{}
	for _, o := range orders {
		ids[o.OrderID] = true
	}
	return ids
}

// Item 1: the legacy Role string is 'customer' for an account created as a
// customer that later became a supplier owner, so Role branching 403d its
// "My Orders" panel. Middleware-context scoping must admit it.
func TestGap14SupplierOrdersCustomerRole(t *testing.T) {
	_, repo := gap14Repos(t)
	rec, orders := gap14GetSupplierOrders(t, repo, gap14Ctx("sup-a", "customer"), "/api/v1/supplier/orders")
	if rec.Code != http.StatusOK {
		t.Fatalf("customer-role supplier orders status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	ids := gap14OrderIDs(orders)
	if !ids["ord-direct"] || !ids["ord-direct2"] {
		t.Fatalf("customer-role orders = %v, want ord-direct + ord-direct2", ids)
	}
	if ids["ord-direct-b"] {
		t.Fatalf("customer-role orders leak supplier B's order: %v", ids)
	}
	for _, o := range orders {
		for _, item := range o.Items {
			if item.ProductID != "prod-a-direct" {
				t.Fatalf("order %s exposes another supplier's item %s", o.OrderID, item.ProductID)
			}
		}
	}
}

// Same customer-role token without the middleware scope must still be denied.
func TestGap14SupplierOrdersNoScopeForbidden(t *testing.T) {
	_, repo := gap14Repos(t)
	rec, _ := gap14GetSupplierOrders(t, repo, gap14Ctx("", "customer"), "/api/v1/supplier/orders")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unscoped supplier orders status = %d, want 403, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Supplier scope required") {
		t.Fatalf("unscoped body = %s, want 'Supplier scope required'", rec.Body.String())
	}
}

// Regression test for the admin scope leak: an admin with no ?supplierId=
// used to get EVERY supplier's shipments (GetShipments omits the filter for
// supplierID ""). Admin + ?supplierId=X sees only X's rows.
func TestGap14SupplierOrdersAdminScope(t *testing.T) {
	_, repo := gap14Repos(t)
	rec, _ := gap14GetSupplierOrders(t, repo, gap14Ctx("", "admin"), "/api/v1/supplier/orders")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unscoped admin status = %d, want 403, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Supplier scope required") {
		t.Fatalf("unscoped admin body = %s, want 'Supplier scope required'", rec.Body.String())
	}
	rec, orders := gap14GetSupplierOrders(t, repo, gap14Ctx("", "admin"), "/api/v1/supplier/orders?supplierId=sup-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin scoped to sup-a status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	ids := gap14OrderIDs(orders)
	if !ids["ord-direct"] || !ids["ord-direct2"] || ids["ord-direct-b"] {
		t.Fatalf("admin sup-a view = %v, want only ord-direct + ord-direct2", ids)
	}
	rec, orders = gap14GetSupplierOrders(t, repo, gap14Ctx("", "admin"), "/api/v1/supplier/orders?supplierId=sup-b")
	if rec.Code != http.StatusOK {
		t.Fatalf("admin scoped to sup-b status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if ids = gap14OrderIDs(orders); len(ids) != 1 || !ids["ord-direct-b"] {
		t.Fatalf("admin sup-b view = %v, want only ord-direct-b", ids)
	}
	// super_admin counts as admin (HasCapability alias) and is scoped the same way.
	rec, _ = gap14GetSupplierOrders(t, repo, gap14Ctx("", "super_admin"), "/api/v1/supplier/orders")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unscoped super_admin status = %d, want 403, body %s", rec.Code, rec.Body.String())
	}
	rec, orders = gap14GetSupplierOrders(t, repo, gap14Ctx("", "super_admin"), "/api/v1/supplier/orders?supplierId=sup-b")
	if rec.Code != http.StatusOK {
		t.Fatalf("super_admin scoped to sup-b status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if ids = gap14OrderIDs(orders); len(ids) != 1 || !ids["ord-direct-b"] {
		t.Fatalf("super_admin sup-b view = %v, want only ord-direct-b", ids)
	}
}

// Item 1: POST accept + POST shipping also 403d the customer-role token at
// the Role gate (:709/:516). With middleware-context scoping both must work.
func TestGap14SupplierShipAcceptCustomerRole(t *testing.T) {
	db, repo := gap14Repos(t)
	ctx := gap14Ctx("sup-a", "customer")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/orders/ord-direct/accept", nil)
	handleSupplierAcceptOrder(rec, req, ctx, "ord-direct", repo)
	if rec.Code != http.StatusOK {
		t.Fatalf("customer-role accept status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if got := gap14ShipmentStatus(t, db, "ship-1"); got != "processing" {
		t.Fatalf("shipment status after customer-role accept = %q, want processing", got)
	}

	body := `{"carrier":"پست پیشتاز","trackingNumber":"TRK-CUST-1"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/supplier/orders/ord-direct2/shipping", strings.NewReader(body))
	handleSupplierSubmitShipping(rec, req, ctx, "ord-direct2", repo, repository.NewOrderRepository(db), repository.NewSupplierRepository(db), repository.NewTapinRepository(db))
	if rec.Code != http.StatusOK {
		t.Fatalf("customer-role shipping status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	if got := gap14ShipmentStatus(t, db, "ship-2"); got != "shipped" {
		t.Fatalf("shipment status after customer-role shipping = %q, want shipped", got)
	}
}
