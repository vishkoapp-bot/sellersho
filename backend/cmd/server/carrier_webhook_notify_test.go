package main

// Critic fix 1 (Phase 2): end-to-end carrier-webhook coverage for the
// supplier-direct 'failed' path — through handleWebhookTrackingUpdate
// (not just the repo function). Runs ONLY when
// SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL points at a DISPOSABLE
// local Postgres; otherwise skips, like the other DB-gated suites in
// this package.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

const carrierWebhookNotifyDDL = `
DROP TABLE IF EXISTS order_item_origins, notification_outbox, supplier_shipment_audit_logs, supplier_shipments, order_items, orders, products, stores, suppliers, users CASCADE;
CREATE TABLE users (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	mobile VARCHAR(15) UNIQUE NOT NULL,
	full_name VARCHAR(100) NOT NULL DEFAULT '',
	role VARCHAR(20) NOT NULL DEFAULT 'customer',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	email VARCHAR(255),
	username VARCHAR(255),
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	failed_login_attempts integer NOT NULL DEFAULT 0,
	locked_until timestamp with time zone,
	password_hash text,
	updated_at timestamp with time zone NOT NULL DEFAULT now(),
	warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	name VARCHAR(200) NOT NULL
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	approval_status VARCHAR(20) NOT NULL DEFAULT 'approved',
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
	warehouse_city_source character varying(20));
CREATE TABLE stores (
	id VARCHAR(50) PRIMARY KEY,
	name VARCHAR(200) NOT NULL DEFAULT '',
	slug VARCHAR(200) NOT NULL DEFAULT '',
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL
,
	-- TEST-02: shape-compatible columns (shared-DB pollution repair)
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	owner_name VARCHAR(255) NOT NULL DEFAULT '',
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	active_categories text[] NOT NULL DEFAULT '{}'::text[],
	active_plan_id character varying(20) NOT NULL DEFAULT 'trial'::character varying,
	addon_section_title character varying(200),
	contract_signature_code text,
	custom_domain character varying(150),
	custom_store_margin_override numeric(5,2),
	description text,
	is_margin_locked_by_admin boolean NOT NULL DEFAULT false,
	logo text NOT NULL DEFAULT ''::text,
	owner_phone text NOT NULL DEFAULT ''::text,
	owner_type character varying(20) NOT NULL DEFAULT 'dropshipper'::character varying,
	panel_config jsonb NOT NULL DEFAULT '{}'::jsonb,
	plan_expires_at timestamp with time zone,
	profit_margin_percent numeric(5,2) NOT NULL DEFAULT 12.00,
	referral_code character varying(30),
	referrer_seller_id character varying(50),
	rejection_reason text,
	related_section_title character varying(200),
	seller_deals_widget_config jsonb,
	signed_contract_version integer NOT NULL DEFAULT 1,
	status character varying(20) NOT NULL DEFAULT 'active'::character varying,
	tagline text,
	template_config jsonb NOT NULL DEFAULT '{}'::jsonb,
	template_id character varying(50),
	total_sales bigint NOT NULL DEFAULT 0,
	updated_at timestamp with time zone NOT NULL DEFAULT now());
CREATE TABLE products (
	id VARCHAR(50) PRIMARY KEY,
	supplier_id VARCHAR(50) REFERENCES suppliers(id),
	title VARCHAR(200) NOT NULL DEFAULT '',
	stock INT NOT NULL DEFAULT 0
,
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
	fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
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
	supplier_base_cost bigint,
	supplier_warranty character varying(100),
	supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
	tax_class text NOT NULL DEFAULT 'standard'::text,
	updated_at timestamp with time zone NOT NULL DEFAULT now(),
	warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
	warranty_property_id character varying(60),
	weight_grams integer,
	width_mm integer);
CREATE TABLE orders (
	id VARCHAR(50) PRIMARY KEY,
	customer_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	order_number VARCHAR(30) NOT NULL DEFAULT '',
	store_id VARCHAR(50) REFERENCES stores(id),
	referrer_seller_id VARCHAR(50),
 discount_code TEXT NOT NULL DEFAULT '',
	status VARCHAR(30) NOT NULL DEFAULT 'pending',
	payment_status VARCHAR(30) NOT NULL DEFAULT 'pending',
	payable_amount BIGINT NOT NULL DEFAULT 0,
	fulfillment_method VARCHAR(30) DEFAULT 'supplier_direct',
	shipped_at TIMESTAMPTZ,
	disputed_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	box_size character varying(80),
	carrier character varying(100),
	confirmed_at timestamp with time zone,
	coupon_discount_amount bigint,
	customer_address text,
	customer_city character varying(50),
	customer_full_name character varying(100),
	customer_phone character varying(20),
	customer_postal_code character varying(20),
	customer_province character varying(50),
	discount_amount bigint NOT NULL DEFAULT 0,
	dispute_reason text,
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
	return_window_ends_at timestamp with time zone,
	seller_discount_amount bigint NOT NULL DEFAULT 0,
	shipping_cost bigint NOT NULL DEFAULT 0,
	shipping_label text,
	shipping_quote_id text,
	store_name character varying(100),
	supplier_addon_amount bigint NOT NULL DEFAULT 0,
	total_amount bigint,
	tracking_code character varying(50),
	tracking_number character varying(100),
	wallet_amount bigint NOT NULL DEFAULT 0,
	weight_grams integer);
CREATE TABLE order_items (
	id VARCHAR(50)  DEFAULT gen_random_uuid() PRIMARY KEY,
	order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
	product_id VARCHAR(50) REFERENCES products(id),
	quantity INT NOT NULL DEFAULT 1,
	variant_id VARCHAR(50)
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	discount_amount bigint NOT NULL DEFAULT 0,
	final_unit_price bigint,
	platform_fee bigint,
	pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
	product_title_snapshot text,
	seller_margin bigint,
	variant_name text,
	variant_spnc character varying(12),
	wholesale_cost bigint);
CREATE TABLE supplier_shipments (
	id VARCHAR(50) PRIMARY KEY,
	order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
	supplier_id VARCHAR(50) REFERENCES suppliers(id),
	status VARCHAR(30) NOT NULL DEFAULT 'pending',
	carrier VARCHAR(100) DEFAULT '',
	tracking_number VARCHAR(100) DEFAULT '',
	shipped_at TIMESTAMPTZ,
	delivered_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	customer_name character varying(255),
	customer_phone character varying(20),
	packaging_height_mm integer,
	packaging_length_mm integer,
	packaging_width_mm integer,
	send_price bigint,
	shipping_address jsonb,
	supplier_notes text,
	tapin_box_id text,
	tapin_last_status text,
	tapin_register_attempted_at timestamp with time zone,
	tapin_registered_at timestamp with time zone,
	tapin_service text);
CREATE TABLE order_item_origins (
	order_item_id VARCHAR(50),
	supplier_id VARCHAR(50)
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	created_at timestamp with time zone NOT NULL DEFAULT now(),
	id text,
	leg_index integer,
	order_id character varying(255),
	origin_label text,
	origin_type text,
	quantity integer,
	supplier_warehouse_id text,
	warehouse_id text);
CREATE TABLE supplier_shipment_audit_logs (
	id VARCHAR(50) PRIMARY KEY,
	shipment_id VARCHAR(50) NOT NULL,
	actor_user_id VARCHAR(50) NOT NULL DEFAULT '',
	action VARCHAR(50) NOT NULL DEFAULT '',
	timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE notification_outbox (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	recipient_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind VARCHAR(50) NOT NULL,
	title VARCHAR(200) NOT NULL,
	body TEXT NOT NULL,
	payload JSONB NOT NULL DEFAULT '{}'::jsonb,
	status VARCHAR(20) NOT NULL DEFAULT 'pending'
		CHECK (status IN ('pending', 'sent', 'read')),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	sent_at TIMESTAMPTZ,
	read_at TIMESTAMPTZ
);
`

func carrierWebhookNotifyDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SUPPLIER_NOTIFICATIONS_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	if _, err := db.Exec(carrierWebhookNotifyDDL); err != nil {
		t.Fatalf("seed DDL: %v", err)
	}
	// TEST-02 round 4: the DDL above DROP TABLEs stores, which destroys
	// the migrated pre-seed central store ('store-central', slug
	// 'central') that the catalog list handler needs for its store
	// scope (resolveListStore returns nil without it -> 500 "catalog
	// store scope unavailable"). Restore it — same values as migration
	// 000002_seed_data.up.sql.
	if _, err := db.Exec(`INSERT INTO stores (id, name, slug, owner_type, owner_name, profit_margin_percent, active_plan_id, signed_contract_version, total_sales) VALUES ('store-central', 'فروشگاه رسمی مرکزی سلرشو', 'central', 'central', 'مدیریت مرکزی سلرشو', 0.00, 'vip', 2, 184500000) ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatalf("restore central store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCarrierWebhookFailedNotifiesCustomerEndToEnd(t *testing.T) {
	db := carrierWebhookNotifyDB(t)
	ctx := context.Background()
	var customerID string
	if err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ('09120000020', 'Customer', 'customer') RETURNING id`).Scan(&customerID); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	var supplierOwnerID string
	if err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ('09120000021', 'Supplier', 'supplier') RETURNING id`).Scan(&supplierOwnerID); err != nil {
		t.Fatalf("seed supplier owner: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO suppliers (id, owner_user_id, name) VALUES ('sup-1', $1, 'Test Supplier')`, supplierOwnerID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO stores (id, name, slug, owner_user_id) VALUES ('store-1', 'Test Store', 'test-store', $1)`, supplierOwnerID); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO products (id, supplier_id, title, stock) VALUES ('prod-1', 'sup-1', 'تیشرت', 10)`); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	seed := func(orderID, orderNumber, tracking string) {
		if _, err := db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, status, payment_status, payable_amount, fulfillment_method) VALUES ($1, $2, $3, 'store-1', 'shipped', 'paid', 500000, 'supplier_direct')`, orderID, customerID, orderNumber); err != nil {
			t.Fatalf("seed order %s: %v", orderID, err)
		}
		if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity) VALUES ($1, $2, 'prod-1', 1)`, "item-"+orderID, orderID); err != nil {
			t.Fatalf("seed item %s: %v", orderID, err)
		}
		if _, err := db.Exec(`INSERT INTO supplier_shipments (id, order_id, supplier_id, status, tracking_number) VALUES ($1, $2, 'sup-1', 'shipped', $3)`, "ship-"+orderID, orderID, tracking); err != nil {
			t.Fatalf("seed shipment %s: %v", orderID, err)
		}
	}
	seed("ord-1", "ORD-1", "TRK-FAIL1")
	seed("ord-2", "ORD-2", "TRK-OK2")

	shipmentRepo := repository.NewSupplierShipmentRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	callWebhook := func(tracking, status string) int {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"trackingNumber": tracking, "status": status})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/tracking/update", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Secret", "test-secret")
		w := httptest.NewRecorder()
		handleWebhookTrackingUpdate(w, req, ctx, shipmentRepo, orderRepo, "test-secret")
		return w.Code
	}
	countKind := func(kind string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE kind = $1`, kind).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", kind, err)
		}
		return n
	}

	// 'failed' on an in-flight direct shipment: order flips to
	// delivery_disputed and the customer is told — through the
	// handler, not just the repo function.
	if code := callWebhook("TRK-FAIL1", "failed"); code != http.StatusOK {
		t.Fatalf("failed webhook: status %d, want 200", code)
	}
	var orderStatus, shipStatus string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord-1'`).Scan(&orderStatus); err != nil || orderStatus != "delivery_disputed" {
		t.Fatalf("ord-1 status = %q, want delivery_disputed", orderStatus)
	}
	if err := db.QueryRow(`SELECT status FROM supplier_shipments WHERE tracking_number = 'TRK-FAIL1'`).Scan(&shipStatus); err != nil || shipStatus != "failed" {
		t.Fatalf("shipment status = %q, want failed", shipStatus)
	}
	if n := countKind("carrier_failed"); n != 1 {
		t.Fatalf("carrier_failed rows = %d, want 1", n)
	}
	var recipient string
	if err := db.QueryRow(`SELECT recipient_user_id::text FROM notification_outbox WHERE kind = 'carrier_failed'`).Scan(&recipient); err != nil || recipient != customerID {
		t.Fatalf("carrier_failed recipient = %q, want customer %s", recipient, customerID)
	}

	// Repeat 'failed' webhook: still 200, still exactly one row.
	if code := callWebhook("TRK-FAIL1", "failed"); code != http.StatusOK {
		t.Fatalf("repeated failed webhook: status %d, want 200", code)
	}
	if n := countKind("carrier_failed"); n != 1 {
		t.Fatalf("carrier_failed rows after repeat = %d, want still 1", n)
	}

	// 'delivered' still works through the same path and notifies
	// nobody about a failure.
	if code := callWebhook("TRK-OK2", "delivered"); code != http.StatusOK {
		t.Fatalf("delivered webhook: status %d, want 200", code)
	}
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord-2'`).Scan(&orderStatus); err != nil || orderStatus != "delivered" {
		t.Fatalf("ord-2 status = %q, want delivered", orderStatus)
	}
	if n := countKind("carrier_failed"); n != 1 {
		t.Fatalf("carrier_failed rows after delivered = %d, want still 1", n)
	}
}
