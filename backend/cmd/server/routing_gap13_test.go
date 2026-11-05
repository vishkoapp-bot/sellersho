package main

// Gap 13 regression tests: ONE routing truth for supplier
// fulfilment. These run ONLY when GAP13_TEST_DATABASE_URL is set to
// a DISPOSABLE local Postgres (never the live database); otherwise
// they skip.
//
// Covered:
//   - a direct-only-enabled supplier's WAREHOUSE-flagged product
//     routes supplier_direct at creation (shipment + 'supplier'
//     origin row) — the divergence the queue's toggle semantics
//     exposed when creation read the product flag alone;
//   - both-enabled + direct flag stays direct;
//   - a supplier with NO shipping-method rows + a direct-flagged
//     product now routes WAREHOUSE with no shipment (the intended
//     behaviour change: the resolver's fail-safe);
//   - quote/order parity: a mixed cart quotes by RESOLVED routing,
//     and an order placed with that quoteId is not rejected with
//     shipping_quote_changed.

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

type gap13Repos struct {
	db           *sql.DB
	orderRepo    *repository.OrderRepository
	catalogRepo  *repository.CatalogRepository
	storeRepo    *repository.StoreRepository
	shipmentRepo *repository.SupplierShipmentRepository
	pricingRepo  *repository.PricingRepository
	supplierRepo *repository.SupplierRepository
}

func gap13Setup(t *testing.T) *gap13Repos {
	t.Helper()
	dsn := os.Getenv("GAP13_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GAP13_TEST_DATABASE_URL not set; skipping disposable-DB gap13 test")
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
		`DROP TABLE IF EXISTS supplier_shipments, order_item_origins, order_items, orders, supplier_shipping_methods, suppliers, warehouses, store_products, store_product_overrides, store_niche_overrides, stores, product_images, product_variants, product_niches, products, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT PRIMARY KEY, return_window_days INT, shipping_base_rate BIGINT, shipping_per_kg BIGINT, volumetric_divisor BIGINT, default_item_weight_grams BIGINT)`,
		`INSERT INTO platform_settings (id, return_window_days, shipping_base_rate, shipping_per_kg, volumetric_divisor, default_item_weight_grams) VALUES (1, 7, 100000, 20000, 5000, 500)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100), preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50), supply_option VARCHAR(50), is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, origin_supplier_warehouse_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'x', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_images (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper', owner_name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id VARCHAR(50) NOT NULL DEFAULT '', referral_code VARCHAR(100) NOT NULL DEFAULT '', referrer_seller_id VARCHAR(50), signed_contract_version INT NOT NULL DEFAULT 0, custom_domain VARCHAR(255) NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, template_id VARCHAR(100) NOT NULL DEFAULT '', template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '', panel_config JSONB, tagline TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', status VARCHAR(50) NOT NULL DEFAULT 'active', active_categories TEXT[] NOT NULL DEFAULT '{}', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_product_overrides (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE store_niche_overrides (store_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		// approval_status: read by the gap-12 suspension pre-check in
		// handleCreateOrder. tapin_shop_id/token: read by the Tapin
		// quote step (NULL = not connected, formula price stands).
		`CREATE TABLE suppliers (id TEXT PRIMARY KEY, owner_user_id TEXT, approval_status TEXT NOT NULL DEFAULT 'approved', tapin_shop_id TEXT, tapin_token TEXT)`,
		`CREATE TABLE supplier_shipping_methods (owner_user_id TEXT NOT NULL, method TEXT NOT NULL, status TEXT NOT NULL, requested_at TIMESTAMPTZ, decided_at TIMESTAMPTZ, decided_by TEXT)`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY, name TEXT UNIQUE, province_name TEXT, city_name TEXT, priority INT NOT NULL DEFAULT 100, is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE orders (
			id TEXT PRIMARY KEY, customer_user_id UUID, order_number TEXT, store_id TEXT, store_name TEXT,
			total_amount BIGINT NOT NULL DEFAULT 0, discount_amount BIGINT NOT NULL DEFAULT 0, seller_discount_amount BIGINT NOT NULL DEFAULT 0, discount_code TEXT,
			payable_amount BIGINT NOT NULL DEFAULT 0, payment_method TEXT, payment_status TEXT,
			customer_full_name TEXT, customer_phone TEXT, customer_province TEXT, customer_city TEXT,
			customer_address TEXT, customer_postal_code TEXT, status TEXT, tracking_code TEXT,
			shipping_cost BIGINT NOT NULL DEFAULT 0, dropshipper_profit BIGINT NOT NULL DEFAULT 0,
			platform_profit BIGINT NOT NULL DEFAULT 0, referral_commission BIGINT NOT NULL DEFAULT 0,
			referral_commission_released BOOLEAN NOT NULL DEFAULT FALSE, referrer_seller_id TEXT,
			profit_released BOOLEAN NOT NULL DEFAULT FALSE, profit_released_at TIMESTAMPTZ,
			shipped_at TIMESTAMPTZ, confirmed_at TIMESTAMPTZ, disputed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			fulfillment_method TEXT, wallet_amount BIGINT NOT NULL DEFAULT 0, gateway_amount BIGINT NOT NULL DEFAULT 0,
			platform_addon_amount BIGINT NOT NULL DEFAULT 0, supplier_addon_amount BIGINT NOT NULL DEFAULT 0,
			inventory_captured BOOLEAN NOT NULL DEFAULT FALSE, packaging_length_mm INT, packaging_width_mm INT,
			packaging_height_mm INT, dispute_reason TEXT, carrier TEXT, tracking_number TEXT,
			shipping_label TEXT, packed_at TIMESTAMPTZ, box_size TEXT, weight_grams INT,
			idempotency_key TEXT, return_window_ends_at TIMESTAMPTZ, shipping_quote_id TEXT
		)`,
		`CREATE TABLE order_items (
			id TEXT PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT,
			wholesale_cost BIGINT, platform_fee BIGINT, seller_margin BIGINT, final_unit_price BIGINT, discount_amount BIGINT NOT NULL DEFAULT 0,
			variant_id UUID, variant_name TEXT, variant_spnc TEXT
		)`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id TEXT, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT)`,
		`CREATE TABLE supplier_shipments (id TEXT PRIMARY KEY, order_id TEXT, supplier_id TEXT, shipping_address JSONB, customer_phone TEXT, customer_name TEXT, status TEXT, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ)`,
		// Central store: order routing is supplier-side, so the
		// store is central to keep margin/discount machinery out.
		`INSERT INTO stores (id,name,slug,owner_type,panel_config,seller_deals_widget_config) VALUES ('store-central','مرکزی','central','central','{}'::jsonb,'null'::jsonb)`,
		`INSERT INTO suppliers (id, owner_user_id, approval_status) VALUES ('sup-direct-only','owner-direct','approved'), ('sup-both','owner-both','approved'), ('sup-none','owner-none','approved')`,
		// direct-only: direct enabled, warehouse disabled.
		`INSERT INTO supplier_shipping_methods (owner_user_id, method, status) VALUES ('owner-direct','direct','enabled'), ('owner-direct','warehouse','disabled')`,
		// both-enabled.
		`INSERT INTO supplier_shipping_methods (owner_user_id, method, status) VALUES ('owner-both','direct','enabled'), ('owner-both','warehouse','enabled')`,
		// sup-none deliberately has NO method rows.
		`INSERT INTO warehouses (id, name, province_name, city_name) VALUES ('wh-central','انبار مرکزی تهران','تهران','تهران')`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('p-wh-directonly','کالای انباری مستقیم‌فعال','wh-directonly','digital','sup-direct-only',12000000,10000000,0,TRUE,10,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'برند',50000001)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('p-direct-both','کالای مستقیم هردوفعال','direct-both','digital','sup-both',12000000,10000000,0,TRUE,10,'img','desc','انبار تأمین‌کننده',1,'گارانتی',FALSE,FALSE,'supplier_direct','1day',TRUE,'برند',50000002)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('p-direct-none','کالای مستقیم بدون روش','direct-none','digital','sup-none',12000000,10000000,0,TRUE,10,'img','desc','انبار تأمین‌کننده',1,'گارانتی',FALSE,FALSE,'supplier_direct','1day',TRUE,'برند',50000003)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('p-wh-none','کالای انباری بدون روش','wh-none','digital','sup-none',12000000,10000000,0,TRUE,10,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'برند',50000004)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL/seed failed: %v\n%s", err, q)
		}
	}
	return &gap13Repos{
		db:           db,
		orderRepo:    repository.NewOrderRepository(db),
		catalogRepo:  repository.NewCatalogRepository(db),
		storeRepo:    repository.NewStoreRepository(db),
		shipmentRepo: repository.NewSupplierShipmentRepository(db),
		pricingRepo:  repository.NewPricingRepository(db),
		supplierRepo: repository.NewSupplierRepository(db),
	}
}

const gap13UserID = "11111111-1111-1111-1111-111111111111"

func gap13CreateOrder(t *testing.T, rp *gap13Repos, body string) *httptest.ResponseRecorder {
	t.Helper()
	claims := &auth.UserClaims{UserID: gap13UserID, Mobile: "09120000000", Role: "customer", Capabilities: []string{"customer"}}
	ctx := context.WithValue(context.Background(), userClaimsKey, claims)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handleCreateOrder(rec, req, ctx, rp.orderRepo, rp.catalogRepo, rp.storeRepo, rp.shipmentRepo, rp.pricingRepo, rp.supplierRepo, rp.db)
	return rec
}

func gap13OrderBody(items, shippingQuoteID string) string {
	q := ""
	if shippingQuoteID != "" {
		q = `,"shippingQuoteId":"` + shippingQuoteID + `"`
	}
	return `{"storeId":"central","paymentMethod":"cash","customerInfo":{"fullName":"مشتری تست","phone":"09120000000","province":"تهران","city":"تهران","address":"خیابان تست ۱","postalCode":"1234567890"},"items":[` + items + `]` + q + `}`
}

func gap13OrderID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order status = %d, body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.ID == "" {
		t.Fatalf("decode order response: %v (%s)", err, rec.Body.String())
	}
	return out.ID
}

func gap13AssertOrderRouting(t *testing.T, rp *gap13Repos, orderID, wantFulfillment, wantOriginType, wantShipmentSupplier string) {
	t.Helper()
	ctx := context.Background()
	var fulfillment string
	if err := rp.db.QueryRowContext(ctx, `SELECT fulfillment_method FROM orders WHERE id = $1`, orderID).Scan(&fulfillment); err != nil {
		t.Fatalf("read order fulfillment: %v", err)
	}
	if fulfillment != wantFulfillment {
		t.Fatalf("order fulfillment = %q, want %q", fulfillment, wantFulfillment)
	}
	var originType string
	var originQty int
	if err := rp.db.QueryRowContext(ctx, `SELECT origin_type, quantity FROM order_item_origins WHERE order_id = $1`, orderID).Scan(&originType, &originQty); err != nil {
		t.Fatalf("read origin row: %v", err)
	}
	if originType != wantOriginType || originQty != 1 {
		t.Fatalf("origin = (%q, %d), want (%q, 1)", originType, originQty, wantOriginType)
	}
	var shipments int
	if err := rp.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM supplier_shipments WHERE order_id = $1`, orderID).Scan(&shipments); err != nil {
		t.Fatalf("count shipments: %v", err)
	}
	if wantShipmentSupplier == "" {
		if shipments != 0 {
			t.Fatalf("shipments = %d, want 0", shipments)
		}
		return
	}
	if shipments != 1 {
		t.Fatalf("shipments = %d, want 1", shipments)
	}
	var sup string
	if err := rp.db.QueryRowContext(ctx, `SELECT supplier_id FROM supplier_shipments WHERE order_id = $1`, orderID).Scan(&sup); err != nil {
		t.Fatalf("read shipment supplier: %v", err)
	}
	if sup != wantShipmentSupplier {
		t.Fatalf("shipment supplier = %q, want %q", sup, wantShipmentSupplier)
	}
}

// The headline divergence: direct-only supplier + warehouse-flagged
// product must route direct at creation (the queue always said so).
func TestGap13DirectOnlySupplierWarehouseFlagRoutesDirect(t *testing.T) {
	rp := gap13Setup(t)
	rec := gap13CreateOrder(t, rp, gap13OrderBody(`{"productId":"p-wh-directonly","quantity":1}`, ""))
	orderID := gap13OrderID(t, rec)
	gap13AssertOrderRouting(t, rp, orderID, "supplier_direct", "supplier", "sup-direct-only")
	// The warehouse leg identity must NOT be stamped on a supplier
	// origin, and the origin must carry the supplier id.
	var whID sql.NullString
	var supID string
	if err := rp.db.QueryRowContext(context.Background(), `SELECT warehouse_id, supplier_id FROM order_item_origins WHERE order_id = $1`, orderID).Scan(&whID, &supID); err != nil {
		t.Fatalf("read origin ids: %v", err)
	}
	if whID.Valid || supID != "sup-direct-only" {
		t.Fatalf("origin ids = (warehouse %v, supplier %q), want (NULL, sup-direct-only)", whID, supID)
	}
}

// Both methods enabled: the product flag decides — direct flag
// stays direct.
func TestGap13BothEnabledDirectFlagStaysDirect(t *testing.T) {
	rp := gap13Setup(t)
	rec := gap13CreateOrder(t, rp, gap13OrderBody(`{"productId":"p-direct-both","quantity":1}`, ""))
	orderID := gap13OrderID(t, rec)
	gap13AssertOrderRouting(t, rp, orderID, "supplier_direct", "supplier", "sup-both")
}

// INTENDED BEHAVIOUR CHANGE: a supplier with no shipping-method
// rows used to ship direct-flagged products direct (creation read
// the flag alone). The single resolver's fail-safe routes them to
// the warehouse instead: no shipment, a 'warehouse' origin whose
// warehouse_id resolves by name.
func TestGap13RowlessSupplierDirectFlagFallsToWarehouse(t *testing.T) {
	rp := gap13Setup(t)
	rec := gap13CreateOrder(t, rp, gap13OrderBody(`{"productId":"p-direct-none","quantity":1}`, ""))
	orderID := gap13OrderID(t, rec)
	gap13AssertOrderRouting(t, rp, orderID, "warehouse", "warehouse", "")
}

// Quote/order parity: the standalone quote groups a mixed cart by
// RESOLVED routing (one supplier leg for the direct-only supplier's
// warehouse-flagged product + one warehouse leg), and an order
// placed with that quoteId recomputes the identical quote — no
// shipping_quote_changed.
func TestGap13QuoteOrderParity(t *testing.T) {
	rp := gap13Setup(t)
	items := `{"productId":"p-wh-directonly","quantity":1},{"productId":"p-wh-none","quantity":1}`

	claims := &auth.UserClaims{UserID: gap13UserID, Mobile: "09120000000", Role: "customer", Capabilities: []string{"customer"}}
	ctx := context.WithValue(context.Background(), userClaimsKey, claims)
	qreq := httptest.NewRequest(http.MethodPost, "/api/v1/shipping/quote", strings.NewReader(
		`{"items":[{"productId":"p-wh-directonly","quantity":1},{"productId":"p-wh-none","quantity":1}],"destination":{"province":"تهران","city":"تهران","address":"خیابان تست ۱","postalCode":"1234567890"}}`))
	qreq = qreq.WithContext(ctx)
	qrec := httptest.NewRecorder()
	handleShippingQuote(qrec, qreq, ctx, rp.catalogRepo, rp.supplierRepo, rp.db)
	if qrec.Code != http.StatusOK {
		t.Fatalf("quote status = %d, body %s", qrec.Code, qrec.Body.String())
	}
	var quote struct {
		QuoteID       string `json:"quoteId"`
		TotalShipping int64  `json:"totalShipping"`
		Legs          []struct {
			OriginID string `json:"originId"`
		} `json:"legs"`
	}
	if err := json.Unmarshal(qrec.Body.Bytes(), &quote); err != nil {
		t.Fatalf("decode quote: %v", err)
	}
	if len(quote.Legs) != 2 {
		t.Fatalf("quote legs = %d, want 2 (supplier leg + warehouse leg): %s", len(quote.Legs), qrec.Body.String())
	}
	origins := map[string]bool{}
	for _, leg := range quote.Legs {
		origins[leg.OriginID] = true
	}
	if !origins["supplier:sup-direct-only"] || !origins["warehouse"] {
		t.Fatalf("quote leg origins = %v, want supplier:sup-direct-only + warehouse", origins)
	}

	rec := gap13CreateOrder(t, rp, gap13OrderBody(items, quote.QuoteID))
	orderID := gap13OrderID(t, rec)
	var shippingCost int64
	var storedQuoteID string
	if err := rp.db.QueryRowContext(context.Background(), `SELECT shipping_cost, shipping_quote_id FROM orders WHERE id = $1`, orderID).Scan(&shippingCost, &storedQuoteID); err != nil {
		t.Fatalf("read order shipping: %v", err)
	}
	if shippingCost != quote.TotalShipping || storedQuoteID != quote.QuoteID {
		t.Fatalf("order shipping = (%d, %q), want quote (%d, %q)", shippingCost, storedQuoteID, quote.TotalShipping, quote.QuoteID)
	}
	// The mixed order carries one origin of each type, on distinct
	// legs.
	rows, err := rp.db.QueryContext(context.Background(), `SELECT origin_type, leg_index FROM order_item_origins WHERE order_id = $1 ORDER BY leg_index`, orderID)
	if err != nil {
		t.Fatalf("read origins: %v", err)
	}
	defer rows.Close()
	seen := map[string]int{}
	for rows.Next() {
		var ot string
		var li int
		if err := rows.Scan(&ot, &li); err != nil {
			t.Fatalf("scan origin: %v", err)
		}
		seen[ot] = li
	}
	if len(seen) != 2 {
		t.Fatalf("origin types = %v, want supplier + warehouse", seen)
	}
	if seen["supplier"] == seen["warehouse"] {
		t.Fatalf("origin leg indexes collide: %v", seen)
	}
}
