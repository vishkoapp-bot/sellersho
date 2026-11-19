package repository

// Phase-2 notification writers coverage (launch-readiness/notifications).
// Runs ONLY when NOTIFY_WRITERS_TEST_DATABASE_URL points at a DISPOSABLE
// local Postgres (never the live database); otherwise skips, like
// reject_notification_test.go. The DDL is the minimal faithful subset
// of the real schema the writer call sites touch.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/domain"
	_ "github.com/lib/pq"
)

const notifyWritersDDL = `
DROP TABLE IF EXISTS refunds,order_item_origins, referral_bonuses, notification_outbox, ledger_entries, support_tickets, payment_transactions, disputes, payout_requests, pending_approvals, supplier_shipments, supplier_shipment_audit_logs, order_items, product_variants, customer_returns, orders, products, stores, suppliers, users CASCADE;
CREATE TABLE referral_bonuses(id TEXT PRIMARY KEY,referrer_store_id TEXT,referred_store_id TEXT,amount BIGINT DEFAULT 100000 ,status TEXT DEFAULT 'locked'::character varying ,released_at TIMESTAMPTZ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now());
CREATE TABLE users (
	id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
	mobile VARCHAR(15) UNIQUE NOT NULL,
	full_name VARCHAR(100) NOT NULL DEFAULT '',
	role VARCHAR(20) NOT NULL DEFAULT 'customer',
	is_active BOOLEAN NOT NULL DEFAULT TRUE,
	username VARCHAR(50) DEFAULT '',
	password_hash TEXT DEFAULT '',
	email VARCHAR(200),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	failed_login_attempts integer NOT NULL DEFAULT 0,
	locked_until timestamp with time zone,
	warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying);
CREATE TABLE suppliers (
	id VARCHAR(50) PRIMARY KEY,
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	name VARCHAR(200) NOT NULL,
	logo_url TEXT,
	rating DECIMAL(3,2) DEFAULT 0,
	total_products INT DEFAULT 0,
	delivery_score INT DEFAULT 100,
	warehouse_city VARCHAR(100) DEFAULT '',
	description TEXT,
	badge VARCHAR(50) DEFAULT '',
	supports_direct_shipping BOOLEAN DEFAULT FALSE,
	default_markup_type VARCHAR(20),
	default_markup_value DECIMAL(10,2),
	signed_contract_version INT DEFAULT 1,
	approval_status VARCHAR(20) DEFAULT 'approved',
	approved_by VARCHAR(50),
	approved_at TIMESTAMPTZ,
	rejection_reason TEXT,
	tapin_shop_id VARCHAR(50),
	tapin_token TEXT,
	warehouse_city_confirmed BOOLEAN DEFAULT FALSE,
	warehouse_city_source VARCHAR(30),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	suspended_at timestamp with time zone,
	suspended_by text,
	suspension_reason text);
CREATE TABLE stores (
	id VARCHAR(50) PRIMARY KEY,
	name VARCHAR(200) NOT NULL DEFAULT '',
	slug VARCHAR(200) NOT NULL DEFAULT '',
	owner_type VARCHAR(30) DEFAULT 'dropshipper',
	owner_name VARCHAR(200) DEFAULT '',
	owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
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
	store_name VARCHAR(200) DEFAULT '',
	total_amount BIGINT NOT NULL DEFAULT 0,
	discount_amount BIGINT NOT NULL DEFAULT 0,
	customer_full_name VARCHAR(200) DEFAULT '',
	customer_phone VARCHAR(20) DEFAULT '',
	customer_province VARCHAR(100) DEFAULT '',
	customer_city VARCHAR(100) DEFAULT '',
	customer_address TEXT DEFAULT '',
	customer_postal_code VARCHAR(20) DEFAULT '',
	status VARCHAR(30) NOT NULL DEFAULT 'pending',
	payment_method VARCHAR(30) DEFAULT '',
	payment_status VARCHAR(30) NOT NULL DEFAULT 'pending',
	payable_amount BIGINT NOT NULL DEFAULT 0,
	inventory_captured BOOLEAN NOT NULL DEFAULT FALSE,
	payment_expires_at TIMESTAMPTZ DEFAULT (now() + '00:15:00'::interval) ,
	fulfillment_method VARCHAR(30) DEFAULT 'warehouse',
	tracking_code VARCHAR(100) DEFAULT '',
	tracking_number VARCHAR(100) DEFAULT '',
	shipping_cost BIGINT NOT NULL DEFAULT 0,
	dropshipper_profit BIGINT NOT NULL DEFAULT 0,
	platform_profit BIGINT NOT NULL DEFAULT 0,
	referral_commission BIGINT NOT NULL DEFAULT 0,
	referral_commission_released BOOLEAN NOT NULL DEFAULT FALSE,
	referrer_seller_id VARCHAR(50) DEFAULT '',
	profit_released BOOLEAN NOT NULL DEFAULT FALSE,
	profit_released_at TIMESTAMPTZ,
	shipped_at TIMESTAMPTZ,
	confirmed_at TIMESTAMPTZ,
	disputed_at TIMESTAMPTZ,
	wallet_amount BIGINT NOT NULL DEFAULT 0,
	gateway_amount BIGINT NOT NULL DEFAULT 0,
	platform_addon_amount BIGINT NOT NULL DEFAULT 0,
	supplier_addon_amount BIGINT NOT NULL DEFAULT 0,
	packaging_length_mm INT,
	packaging_width_mm INT,
	packaging_height_mm INT,
	dispute_reason TEXT,
	carrier VARCHAR(100) DEFAULT '',
	shipping_label TEXT,
	packed_at TIMESTAMPTZ,
	box_size VARCHAR(50) DEFAULT '',
	weight_grams INT,
	idempotency_key VARCHAR(100) DEFAULT '',
	return_window_ends_at TIMESTAMPTZ,
	seller_discount_amount BIGINT NOT NULL DEFAULT 0,
	discount_code VARCHAR(50) DEFAULT '',
 coupon_discount_amount BIGINT,
	shipping_quote_id VARCHAR(100) DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE product_variants (
	id VARCHAR(50)  DEFAULT gen_random_uuid() PRIMARY KEY,
	product_id VARCHAR(50) REFERENCES products(id),
	variant_name VARCHAR(200) DEFAULT '',
	stock INT NOT NULL DEFAULT 0
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
	combination_key text,
	created_at timestamp with time zone NOT NULL DEFAULT now(),
	height_mm integer,
	inventory_mode text NOT NULL DEFAULT 'legacy'::text,
	is_active boolean NOT NULL DEFAULT true,
	is_default boolean NOT NULL DEFAULT false,
	length_mm integer,
	max_cart_qty integer,
	measurement_revision_id text,
	measurement_source text,
	measurement_status text NOT NULL DEFAULT 'unverified'::text,
	measurement_version integer NOT NULL DEFAULT 0,
	min_cart_qty integer NOT NULL DEFAULT 1,
	sale_from timestamp with time zone,
	sale_price bigint,
	sale_to timestamp with time zone,
	selection jsonb NOT NULL DEFAULT '{}'::jsonb,
	shipping_class text,
	spnc character varying(12),
	spnc_number integer,
	supplier_base_cost bigint,
	tax_class text,
	weight_grams integer,
	width_mm integer);
CREATE TABLE customer_returns (
	id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,
	order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
	customer_id UUID REFERENCES users(id) ON DELETE SET NULL,
	status VARCHAR(20) NOT NULL DEFAULT 'requested',
	reason TEXT NOT NULL DEFAULT '',
	admin_note TEXT NOT NULL DEFAULT '',
	warehouse_id TEXT,
	destination_supplier_warehouse_id TEXT,
	prepaid BOOLEAN NOT NULL DEFAULT FALSE,
	tracking_number TEXT DEFAULT ''::text ,
	shipped_at TIMESTAMPTZ,
	return_code TEXT,
	cod_refused BOOLEAN NOT NULL DEFAULT FALSE,
	return_shipping_amount BIGINT,
	return_shipping_bearer VARCHAR(20),
	return_shipping_settled_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	return_shipping_quote_at timestamp with time zone,
	return_shipping_quote_groups jsonb,
	return_shipping_quote_source text,
	return_shipping_quote_toman bigint);
CREATE TABLE order_items (
	id VARCHAR(50)  DEFAULT gen_random_uuid() PRIMARY KEY,
	order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
	product_id VARCHAR(50) REFERENCES products(id),
	quantity INT NOT NULL DEFAULT 1,
	wholesale_cost BIGINT NOT NULL DEFAULT 0,
	platform_fee BIGINT NOT NULL DEFAULT 0,
	seller_margin BIGINT NOT NULL DEFAULT 0,
	final_unit_price BIGINT NOT NULL DEFAULT 0,
	discount_amount BIGINT NOT NULL DEFAULT 0,
	variant_id VARCHAR(50),
	variant_name VARCHAR(200) DEFAULT '',
	variant_spnc VARCHAR(100) DEFAULT ''
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
	product_title_snapshot text);
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
CREATE TABLE disputes (
	id VARCHAR(50) PRIMARY KEY,
	order_id VARCHAR(50),
	order_number VARCHAR(30) NOT NULL DEFAULT '',
	type VARCHAR(50) NOT NULL DEFAULT '',
	customer_name VARCHAR(200) NOT NULL DEFAULT '',
	product_title VARCHAR(200) NOT NULL DEFAULT '',
	claim_amount BIGINT NOT NULL DEFAULT 0,
	reason TEXT NOT NULL DEFAULT '',
	store_id VARCHAR(50),
	supplier_name VARCHAR(200) DEFAULT '',
	status VARCHAR(30) NOT NULL DEFAULT 'pending',
	responsible_party VARCHAR(100) DEFAULT '',
	resolution_action VARCHAR(100) DEFAULT '',
	resolution_date TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	financial_decision text);
CREATE TABLE payout_requests (
	id VARCHAR(50) PRIMARY KEY,
	store_id VARCHAR(50),
	supplier_id VARCHAR(50),
	recipient_type VARCHAR(30) NOT NULL DEFAULT 'seller',
	recipient_name VARCHAR(200) NOT NULL DEFAULT '',
	shaba VARCHAR(50) NOT NULL DEFAULT '',
	bank_name VARCHAR(100) NOT NULL DEFAULT '',
	amount BIGINT NOT NULL DEFAULT 0,
	status VARCHAR(30) NOT NULL DEFAULT 'pending',
	available_balance BIGINT NOT NULL DEFAULT 0,
	rejection_reason TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	processed_at TIMESTAMPTZ,
	transfer_reference VARCHAR(100),
	transfer_confirmed_by VARCHAR(100),
	transfer_confirmed_at TIMESTAMPTZ
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	payout_method character varying(30) NOT NULL DEFAULT 'manual_bank_transfer'::character varying,
	transfer_date timestamp with time zone);
CREATE TABLE pending_approvals (
	id VARCHAR(50) PRIMARY KEY,
	entity_type VARCHAR(50) NOT NULL,
	target_id VARCHAR(50) NOT NULL DEFAULT '',
	title VARCHAR(200) NOT NULL DEFAULT '',
	requested_data JSONB NOT NULL DEFAULT '{}'::jsonb,
	requested_by VARCHAR(100) NOT NULL DEFAULT '',
	status VARCHAR(50) NOT NULL DEFAULT 'pending',
	rejection_reason TEXT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	reviewed_at TIMESTAMPTZ
);
CREATE TABLE payment_transactions (
	id VARCHAR(80) PRIMARY KEY,
	order_id VARCHAR(50) NOT NULL,
	order_number VARCHAR(30) NOT NULL,
	gateway_token VARCHAR(120) NOT NULL,
	rrn VARCHAR(30),
	card_mask VARCHAR(30),
	amount BIGINT NOT NULL,
	status VARCHAR(20) NOT NULL DEFAULT 'pending',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	compensation_capture boolean NOT NULL DEFAULT false);
CREATE TABLE support_tickets (
	id VARCHAR(50) PRIMARY KEY,
	requester_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
	requester_role VARCHAR(30) NOT NULL DEFAULT 'customer',
	subject VARCHAR(200) NOT NULL DEFAULT '',
	status VARCHAR(20) NOT NULL DEFAULT 'open',
	priority VARCHAR(20) NOT NULL DEFAULT 'normal',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
,
	-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
	related_order_number character varying(30),
	section text NOT NULL DEFAULT ''::text);
CREATE TABLE ledger_entries (
	id VARCHAR(80) PRIMARY KEY,
	date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	target_account VARCHAR(200) NOT NULL DEFAULT '',
	target_type VARCHAR(30) NOT NULL DEFAULT '',
	type VARCHAR(20) NOT NULL DEFAULT '',
	amount BIGINT NOT NULL DEFAULT 0,
	reason TEXT NOT NULL DEFAULT '',
	operator VARCHAR(200) NOT NULL DEFAULT '',
	related_order_or_dispute VARCHAR(100) DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

func notifyWritersTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFY_WRITERS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("NOTIFY_WRITERS_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	db := openPrivateFinanceFixture(t, dsn)
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	removeEmptyRefundFixtureSchema(t, db)
	if _, err := db.Exec(notifyWritersDDL); err != nil {
		t.Fatalf("seed DDL: %v", err)
	}
	installRefundFixtureSchema(t, db)
	// TEST-02 round 4: the CASCADE drop above also dropped the inventory_effective_measurements view; re-create it.
	createEffectiveMeasurementsView(t, db)
	t.Cleanup(func() { db.Close() })
	return db
}

func seedNotifyUser(t *testing.T, db *sql.DB, mobile, role string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`INSERT INTO users (mobile, full_name, role) VALUES ($1, 'Test User', $2) RETURNING id`, mobile, role).Scan(&id); err != nil {
		t.Fatalf("seed user %s: %v", mobile, err)
	}
	return id
}

func seedNotifyOrderWorld(t *testing.T, db *sql.DB) (customerID, storeOwnerID, supplierOwnerID, adminID, warehouseID, storeID, supplierID, productID string) {
	t.Helper()
	customerID = seedNotifyUser(t, db, "09120000010", "customer")
	storeOwnerID = seedNotifyUser(t, db, "09120000011", "seller")
	supplierOwnerID = seedNotifyUser(t, db, "09120000012", "supplier")
	adminID = seedNotifyUser(t, db, "09120000013", "admin")
	warehouseID = seedNotifyUser(t, db, "09120000014", "warehouse_staff")
	if _, err := db.Exec(`INSERT INTO suppliers (id, owner_user_id, name) VALUES ('sup-1', $1, 'Test Supplier')`, supplierOwnerID); err != nil {
		t.Fatalf("seed supplier: %v", err)
	}
	supplierID = "sup-1"
	if _, err := db.Exec(`INSERT INTO stores (id, name, slug, owner_user_id) VALUES ('store-1', 'Test Store', 'test-store', $1)`, storeOwnerID); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	storeID = "store-1"
	if _, err := db.Exec(`INSERT INTO products (id, supplier_id, title, stock) VALUES ('prod-1', 'sup-1', 'تیشرت زنانه', 3)`); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	productID = "prod-1"
	return
}

func seedNotifyOrder(t *testing.T, db *sql.DB, orderID, orderNumber, customerID, status, paymentStatus string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, customer_full_name, status, payment_status, payable_amount, fulfillment_method) VALUES ($1, $2, $3, 'store-1', 'مشتری تست', $4, $5, 500000, 'supplier_direct')`, orderID, customerID, orderNumber, status, paymentStatus); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity) VALUES ($1, $2, 'prod-1', 1)`, "item-"+orderID, orderID); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
}

func outboxKinds(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	rows, err := db.Query(`SELECT recipient_user_id::text, kind FROM notification_outbox ORDER BY created_at`)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var recipient, kind string
		if err := rows.Scan(&recipient, &kind); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		out[kind] = append(out[kind], recipient)
	}
	return out
}

func DisputeForTest(id, orderID, orderNumber string) domain.Dispute {
	return domain.Dispute{
		ID: id, OrderID: orderID, OrderNumber: orderNumber,
		Type: "delivery_undelivered", CustomerName: "مشتری تست",
		ProductTitle: "تیشرت زنانه", ClaimAmount: 500000, Reason: "کالا نرسید",
	}
}

func TestNotifyOrderPlacedAndPaymentConfirmed(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, storeOwnerID, supplierOwnerID, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-1", "ORD-1", customerID, "processing", "pending")
	notifyOrderPlaced(ctx, db, "ord-1")
	notifyPaymentConfirmed(ctx, db, "ord-1")
	kinds := outboxKinds(t, db)
	if len(kinds["order_placed"]) != 2 || len(kinds["payment_confirmed"]) != 2 {
		t.Fatalf("outbox kinds = %v, want 2 order_placed + 2 payment_confirmed", kinds)
	}
	for _, want := range []string{supplierOwnerID, storeOwnerID} {
		found := false
		for _, r := range kinds["order_placed"] {
			if r == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("order_placed missing recipient %s: %v", want, kinds)
		}
	}
}

func TestSubmitShippingNotifiesCustomerWithTracking(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-2", "ORD-2", customerID, "processing", "paid")
	if _, err := db.Exec(`INSERT INTO supplier_shipments (id, order_id, supplier_id, status) VALUES ('ship-1', 'ord-2', 'sup-1', 'pending')`); err != nil {
		t.Fatalf("seed shipment: %v", err)
	}
	repo := &SupplierShipmentRepository{db: db}
	if err := repo.SubmitShipping(ctx, "sup-1", "ship-1", "پست", "TRK123", customerID); err != nil {
		t.Fatalf("SubmitShipping: %v", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ord-2'`).Scan(&status); err != nil || status != "shipped" {
		t.Fatalf("order status = %q, want shipped", status)
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM notification_outbox WHERE kind = 'order_status'`).Scan(&body); err != nil {
		t.Fatalf("no order_status row: %v", err)
	}
	if !strings.Contains(body, "TRK123") {
		t.Fatalf("shipped body = %q, want tracking TRK123", body)
	}
}

func TestCancelOrderNotifiesSuppliersAndStore(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, storeOwnerID, supplierOwnerID, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-3", "ORD-3", customerID, "processing", "pending")
	repo := NewOrderRepository(db)
	if err := repo.CancelOrder(ctx, "ord-3", customerID, false); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	kinds := outboxKinds(t, db)
	recipients := kinds["order_cancelled"]
	if len(recipients) != 2 {
		t.Fatalf("order_cancelled recipients = %v, want supplier + store owner", recipients)
	}
	for _, want := range []string{supplierOwnerID, storeOwnerID} {
		found := false
		for _, r := range recipients {
			if r == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("order_cancelled missing %s: %v", want, recipients)
		}
	}
}

func TestDisputeFiledAndResolvedNotify(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, adminID, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-4", "ORD-4", customerID, "shipped", "paid")
	installFIN005PrivateFixture(t, db)
	fin := NewFinanceRepository(db)
	if err := fin.CreateDispute(ctx, DisputeForTest("disp-1", "ord-4", "ORD-4")); err != nil {
		t.Fatalf("CreateDispute: %v", err)
	}
	kinds := outboxKinds(t, db)
	if len(kinds["dispute_filed"]) != 1 || kinds["dispute_filed"][0] != adminID {
		t.Fatalf("dispute_filed = %v, want [admin %s]", kinds["dispute_filed"], adminID)
	}
	if err := fin.ResolveDispute(ctx, "disp-1", "release_to_seller", "seller", "admin", 500000); err != nil {
		t.Fatalf("ResolveDispute: %v", err)
	}
	kinds = outboxKinds(t, db)
	resolved := kinds["dispute_resolved"]
	foundCustomer := false
	for _, r := range resolved {
		if r == customerID {
			foundCustomer = true
		}
	}
	if !foundCustomer {
		t.Fatalf("dispute_resolved = %v, want customer %s among recipients", resolved, customerID)
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM notification_outbox WHERE kind = 'dispute_resolved' AND recipient_user_id = $1`, customerID).Scan(&body); err != nil {
		t.Fatalf("read resolved body: %v", err)
	}
	if !strings.Contains(body, "رد شد") {
		t.Fatalf("resolved body = %q, want claim-rejected wording", body)
	}
}

func TestReviewSettlementAndTransferNotifyStoreOwner(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	_, storeOwnerID, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	if _, err := db.Exec(`INSERT INTO payout_requests (id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status) VALUES ('pay-1', 'store-1', 'seller', 'store-1', 'IR000', 'بانک تست', 250000, 'pending')`); err != nil {
		t.Fatalf("seed payout: %v", err)
	}
	fin := NewFinanceRepository(db)
	if err := fin.ReviewSettlement(ctx, "pay-1", "approved", "", "admin"); err != nil {
		t.Fatalf("ReviewSettlement: %v", err)
	}
	if err := fin.ConfirmManualTransfer(ctx, "pay-1", "REF-1", "admin"); err != nil {
		t.Fatalf("ConfirmManualTransfer: %v", err)
	}
	kinds := outboxKinds(t, db)
	if len(kinds["payout_reviewed"]) != 1 || kinds["payout_reviewed"][0] != storeOwnerID {
		t.Fatalf("payout_reviewed = %v, want store owner %s", kinds["payout_reviewed"], storeOwnerID)
	}
	if len(kinds["payout_transfer_confirmed"]) != 1 || kinds["payout_transfer_confirmed"][0] != storeOwnerID {
		t.Fatalf("payout_transfer_confirmed = %v, want store owner %s", kinds["payout_transfer_confirmed"], storeOwnerID)
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM notification_outbox WHERE kind = 'payout_transfer_confirmed'`).Scan(&body); err != nil {
		t.Fatalf("read transfer body: %v", err)
	}
	if !strings.Contains(body, "REF-1") || !strings.Contains(body, "250,000") {
		t.Fatalf("transfer body = %q, want reference REF-1 + amount", body)
	}
}

func TestFinanceAdjustmentAndTicketReplyNotify(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	_, storeOwnerID, supplierOwnerID, adminID, _, _, _, _ := seedNotifyOrderWorld(t, db)
	NotifyFinanceAdjustment(ctx, db, "supplier", "sup-1", "debit", 100000, "تعدیل تست", "ADJ-1")
	NotifyFinanceAdjustment(ctx, db, "seller", "store-1", "credit", 200000, "تعدیل فروشگاه", "ADJ-2")
	if _, err := db.Exec(`INSERT INTO support_tickets (id, requester_user_id, requester_role, subject) VALUES ('tick-1', $1, 'seller', 'مشکل پرداخت')`, storeOwnerID); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	NotifyTicketReply(ctx, db, "tick-1", adminID, false, "پاسخ پشتیبانی تست")
	kinds := outboxKinds(t, db)
	if len(kinds["finance_adjustment"]) != 2 {
		t.Fatalf("finance_adjustment rows = %v, want 2", kinds["finance_adjustment"])
	}
	if len(kinds["ticket_reply"]) != 1 || kinds["ticket_reply"][0] != storeOwnerID {
		t.Fatalf("ticket_reply = %v, want requester %s", kinds["ticket_reply"], storeOwnerID)
	}
	_ = supplierOwnerID
}

func TestExpireUnpaidOrdersNotifiesCustomer(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	if _, err := db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, status, payment_status, payable_amount, payment_expires_at, fulfillment_method) VALUES ('ord-5', $1, 'ORD-5', 'store-1', 'pending', 'pending', 100000, NOW() - INTERVAL '1 hour', 'warehouse')`, customerID); err != nil {
		t.Fatalf("seed expired order: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, amount, status) VALUES ('paytx-1', 'ord-5', 'ORD-5', 'tok-1', 100000, 'pending')`); err != nil {
		t.Fatalf("seed payment tx: %v", err)
	}
	repo := NewOrderRepository(db)
	expired, err := repo.ExpireUnpaidOrders(ctx)
	if err != nil {
		t.Fatalf("ExpireUnpaidOrders: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != "ord-5" {
		t.Fatalf("expired = %v, want [ord-5]", expired)
	}
	kinds := outboxKinds(t, db)
	if len(kinds["order_cancelled"]) != 1 || kinds["order_cancelled"][0] != customerID {
		t.Fatalf("order_cancelled = %v, want customer %s", kinds["order_cancelled"], customerID)
	}
}

func TestRecordPaymentFailureNotifiesCustomer(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-6", "ORD-6", customerID, "processing", "pending")
	if _, err := db.Exec(`INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, amount, status) VALUES ('paytx-2', 'ord-6', 'ORD-6', 'tok-2', 500000, 'pending')`); err != nil {
		t.Fatalf("seed payment tx: %v", err)
	}
	repo := NewOrderRepository(db)
	if err := repo.RecordPaymentFailure(ctx, "tok-2"); err != nil {
		t.Fatalf("RecordPaymentFailure: %v", err)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM payment_transactions WHERE gateway_token = 'tok-2'`).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("payment tx status = %q, want failed", status)
	}
	kinds := outboxKinds(t, db)
	if len(kinds["payment_failed"]) != 1 || kinds["payment_failed"][0] != customerID {
		t.Fatalf("payment_failed = %v, want customer %s", kinds["payment_failed"], customerID)
	}
	// Idempotent: a second failure record writes no duplicate row.
	if err := repo.RecordPaymentFailure(ctx, "tok-2"); err != nil {
		t.Fatalf("RecordPaymentFailure retry: %v", err)
	}
	if n := len(outboxKinds(t, db)["payment_failed"]); n != 1 {
		t.Fatalf("payment_failed rows after retry = %d, want 1", n)
	}
}

func TestAdminAndWarehouseResolution(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	_, _, _, adminID, warehouseID, _, _, _ := seedNotifyOrderWorld(t, db)
	admins := AdminUserIDs(ctx, db)
	if len(admins) != 1 || admins[0] != adminID {
		t.Fatalf("AdminUserIDs = %v, want [%s]", admins, adminID)
	}
	wh := WarehouseUserIDs(ctx, db)
	if len(wh) != 1 || wh[0] != warehouseID {
		t.Fatalf("WarehouseUserIDs = %v, want [%s]", wh, warehouseID)
	}
}

func TestLowStockAndReturnDecisionNotify(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, storeOwnerID, supplierOwnerID, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-7", "ORD-7", customerID, "processing", "paid")
	// Critic fix 7: notify only on the CROSSING. Post-order stock 5
	// with qty 1 means pre-order stock was 6 (> 5): notify.
	if _, err := db.Exec(`UPDATE products SET stock = 5 WHERE id = 'prod-1'`); err != nil {
		t.Fatalf("set stock 5: %v", err)
	}
	notifyLowStock(ctx, db, "ord-7")
	kinds := outboxKinds(t, db)
	if len(kinds["low_stock"]) != 2 {
		t.Fatalf("low_stock rows = %v, want supplier + store owner (crossed 6 -> 5)", kinds["low_stock"])
	}
	for _, want := range []string{supplierOwnerID, storeOwnerID} {
		found := false
		for _, r := range kinds["low_stock"] {
			if r == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("low_stock missing %s", want)
		}
	}
	// Same order, stock already low before it (3 + qty 1 = 4, never
	// above the threshold): a second paid order must NOT re-notify.
	if _, err := db.Exec(`UPDATE products SET stock = 3 WHERE id = 'prod-1'`); err != nil {
		t.Fatalf("set stock 3: %v", err)
	}
	notifyLowStock(ctx, db, "ord-7")
	if n := len(outboxKinds(t, db)["low_stock"]); n != 2 {
		t.Fatalf("low_stock rows after already-low order = %d, want still 2", n)
	}
	notifyReturnDecision(ctx, db, "ret-1", "ORD-7", customerID, "rejected", "کالا آسیب ندیده است")
	var body string
	if err := db.QueryRow(`SELECT body FROM notification_outbox WHERE kind = 'return_rejected'`).Scan(&body); err != nil {
		t.Fatalf("no return_rejected row: %v", err)
	}
	if !strings.Contains(body, "کالا آسیب ندیده است") {
		t.Fatalf("return_rejected body = %q, want admin_note reason", body)
	}
}

// Critic fix 7 (variant leg): a variant crossing >5 -> <=5 notifies
// per variant even when the product total stays healthy.
func TestLowStockVariantCrossingNotifies(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	if _, err := db.Exec(`INSERT INTO products (id, supplier_id, title, stock) VALUES ('prod-2', 'sup-1', 'کفش', 100)`); err != nil {
		t.Fatalf("seed prod-2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO product_variants (id, product_id, variant_name, stock) VALUES ('var-1', 'prod-2', 'مشکی 42', 5)`); err != nil {
		t.Fatalf("seed variant: %v", err)
	}
	seedNotifyOrder(t, db, "ord-8", "ORD-8", customerID, "processing", "paid")
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, variant_id) VALUES ('item-ord-8b', 'ord-8', 'prod-2', 1, 'var-1')`); err != nil {
		t.Fatalf("seed variant item: %v", err)
	}
	notifyLowStock(ctx, db, "ord-8")
	rows, err := db.Query(`SELECT body, payload->>'variant_id' FROM notification_outbox WHERE kind = 'low_stock'`)
	if err != nil {
		t.Fatalf("read low_stock: %v", err)
	}
	defer rows.Close()
	variantRows := 0
	for rows.Next() {
		var body, variantID string
		if err := rows.Scan(&body, &variantID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if variantID == "var-1" {
			variantRows++
			if !strings.Contains(body, "مشکی 42") {
				t.Fatalf("variant body = %q, want variant name", body)
			}
		}
	}
	if variantRows != 2 {
		t.Fatalf("variant low_stock rows = %d, want 2 (supplier + store)", variantRows)
	}
}

// Critic fix 3: a repeated warehouse 'failed' webhook is a no-op —
// the order comes back, the customer is told exactly once.
func TestWarehouseCarrierFailedRepeatIsNoop(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	if _, err := db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, status, payment_status, payable_amount, fulfillment_method, tracking_number) VALUES ('ord-wh', $1, 'ORD-WH', 'store-1', 'shipped', 'paid', 500000, 'warehouse', 'TRK-WH1')`, customerID); err != nil {
		t.Fatalf("seed warehouse order: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity) VALUES ('item-ord-wh', 'ord-wh', 'prod-1', 1)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	installFIN005PrivateFixture(t, db)
	repo := NewOrderRepository(db)
	order, err := repo.UpdateWarehouseOrderStatusFromCarrier(ctx, "TRK-WH1", "failed")
	if err != nil || order == nil {
		t.Fatalf("first failed: order=%v err=%v", order, err)
	}
	if order.Status != "delivery_disputed" {
		t.Fatalf("order status = %q, want delivery_disputed", order.Status)
	}
	if n := len(outboxKinds(t, db)["carrier_failed"]); n != 1 {
		t.Fatalf("carrier_failed rows after first failed = %d, want 1", n)
	}
	order2, err := repo.UpdateWarehouseOrderStatusFromCarrier(ctx, "TRK-WH1", "failed")
	if err != nil || order2 == nil {
		t.Fatalf("repeated failed: order=%v err=%v (want no-op success)", order2, err)
	}
	if n := len(outboxKinds(t, db)["carrier_failed"]); n != 1 {
		t.Fatalf("carrier_failed rows after repeated failed = %d, want still 1", n)
	}
}

// Critic fix 4: a repeated COD-refused receive notifies the customer
// exactly once — the second attempt returns the same refusal error
// with no second row.
func TestReceiveCustomerReturnCODRefusedRepeatNotifiesOnce(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-9", "ORD-9", customerID, "delivered", "paid")
	if _, err := db.Exec(`INSERT INTO customer_returns (id, order_id, customer_id, status, prepaid) VALUES ('550e8400-e29b-41d4-a716-446655440000', 'ord-9', $1, 'approved', FALSE)`, customerID); err != nil {
		t.Fatalf("seed return: %v", err)
	}
	repo := NewOrderRepository(db)
	qc := []CustomerReturnItemQC{{ReturnItemID: "ri-1", QCStatus: "ok"}}
	if _, err := repo.ReceiveCustomerReturn(ctx, "550e8400-e29b-41d4-a716-446655440000", qc, "wh-1"); !errors.Is(err, ErrReturnCODRefused) {
		t.Fatalf("first COD receive err = %v, want ErrReturnCODRefused", err)
	}
	if n := len(outboxKinds(t, db)["return_cod_refused"]); n != 1 {
		t.Fatalf("return_cod_refused rows after first refusal = %d, want 1", n)
	}
	if _, err := repo.ReceiveCustomerReturn(ctx, "550e8400-e29b-41d4-a716-446655440000", qc, "wh-1"); !errors.Is(err, ErrReturnCODRefused) {
		t.Fatalf("second COD receive err = %v, want ErrReturnCODRefused again", err)
	}
	if n := len(outboxKinds(t, db)["return_cod_refused"]); n != 1 {
		t.Fatalf("return_cod_refused rows after repeat = %d, want still 1", n)
	}
}

// Critic fix 5: ApplyDisputeRefundEffect notifies with the CLAIM
// amount (not the full payable_amount), only on a real order
// transition, and never a second customer row after ResolveDispute
// already told them.
func TestApplyDisputeRefundEffectSingleClaimFigure(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "ord-10", "ORD-10", customerID, "shipped", "paid")
	// (a) Dispute already resolved (the live order: ResolveDispute ran
	// first and already notified via notifyDisputeResolved) — the
	// effect moves state but writes NO second customer row.
	if _, err := db.Exec(`INSERT INTO disputes (id, order_id, order_number, type, status, resolution_action, responsible_party, claim_amount) VALUES ('disp-10a', 'ord-10', 'ORD-10', 'delivery_undelivered', 'resolved', 'refund_carrier', 'carrier', 100000)`); err != nil {
		t.Fatalf("seed dispute a: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, amount, status) VALUES ('paytx-10', 'ord-10', 'ORD-10', 'tok-10', 500000, 'paid')`); err != nil {
		t.Fatalf("seed payment tx: %v", err)
	}
	repo := NewOrderRepository(db)
	applied, err := repo.ApplyDisputeRefundEffect(ctx, "disp-10a")
	if err != nil || !applied {
		t.Fatalf("ApplyDisputeRefundEffect(a) = %v, %v, want true/nil", applied, err)
	}
	if n := len(outboxKinds(t, db)["refund_issued"]); n != 0 {
		t.Fatalf("refund_issued rows after resolved-dispute effect = %d, want 0 (ResolveDispute already told the customer)", n)
	}
	// (b) Retry on the already-cancelled order: no new row either.
	applied, err = repo.ApplyDisputeRefundEffect(ctx, "disp-10a")
	if err != nil || !applied {
		t.Fatalf("ApplyDisputeRefundEffect(a retry) = %v, %v, want true/nil", applied, err)
	}
	if n := len(outboxKinds(t, db)["refund_issued"]); n != 0 {
		t.Fatalf("refund_issued rows after retry = %d, want 0", n)
	}
	// (c) A dispute whose resolution_action is set but whose status is
	// not yet 'resolved' (effect called standalone): exactly one row,
	// quoting the CLAIM (100,000) — never the full payable (500,000) —
	// and honest wording (no promised PSP reversal).
	seedNotifyOrder(t, db, "ord-11", "ORD-11", customerID, "shipped", "paid")
	if _, err := db.Exec(`INSERT INTO disputes (id, order_id, order_number, type, status, resolution_action, responsible_party, claim_amount) VALUES ('disp-10b', 'ord-11', 'ORD-11', 'delivery_undelivered', 'pending', 'refund_carrier', 'carrier', 100000)`); err != nil {
		t.Fatalf("seed dispute b: %v", err)
	}
	applied, err = repo.ApplyDisputeRefundEffect(ctx, "disp-10b")
	if applied || !errors.Is(err, ErrRefundPrincipalUnresolved) {
		t.Fatalf("pending verdict pretended refund resolution %v %v", applied, err)
	}
	if _, err = db.Exec(`INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES('paytx-11','ord-11','ORD-11','tok-11',500000,'paid')`); err != nil {
		t.Fatal(err)
	}
	installFIN005PrivateFixture(t, db)
	if err = NewFinanceRepository(db).ResolveDispute(ctx, "disp-10b", "refund_carrier", "carrier", "test", 100000); err != nil {
		t.Fatal(err)
	}
	if len(outboxKinds(t, db)["refund_issued"]) != 0 {
		t.Fatal("unexecuted original-source request asserted refund success")
	}
	var body string
	if err = db.QueryRow(`SELECT body FROM notification_outbox WHERE kind='dispute_resolved' AND recipient_user_id=$1 ORDER BY created_at DESC LIMIT 1`, customerID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "100,000") || strings.Contains(body, "500,000") {
		t.Fatalf("verdict notice must quote exact claim: %s", body)
	}

}

// Critic fix 6: the dispute/payout queue is admins + the specialists
// who actually work it; warehouse recipients include logistics.
func TestFinanceQueueAndWarehouseRecipients(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	_, _, _, adminID, warehouseID, _, _, _ := seedNotifyOrderWorld(t, db)
	logisticsID := seedNotifyUser(t, db, "09120000015", "logistics_specialist")
	financeID := seedNotifyUser(t, db, "09120000016", "finance_specialist")
	supportID := seedNotifyUser(t, db, "09120000017", "support_specialist")
	wh := WarehouseUserIDs(ctx, db)
	if len(wh) != 2 {
		t.Fatalf("WarehouseUserIDs = %v, want warehouse_staff + logistics_specialist", wh)
	}
	for _, want := range []string{warehouseID, logisticsID} {
		found := false
		for _, id := range wh {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("WarehouseUserIDs missing %s", want)
		}
	}
	queue := FinanceQueueUserIDs(ctx, db)
	if len(queue) != 3 {
		t.Fatalf("FinanceQueueUserIDs = %v, want admin + finance_specialist + support_specialist", queue)
	}
	for _, want := range []string{adminID, financeID, supportID} {
		found := false
		for _, id := range queue {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("FinanceQueueUserIDs missing %s", want)
		}
	}
	notifyDisputeFiled(ctx, db, "disp-q", "ORD-Q", "delivery_undelivered", 100000)
	kinds := outboxKinds(t, db)
	if len(kinds["dispute_filed"]) != 3 {
		t.Fatalf("dispute_filed recipients = %v, want the 3 queue members", kinds["dispute_filed"])
	}
}

// Critic fix 8: one person who is both the store owner and the
// supplying owner gets exactly one row per event, not two.
func TestOrderPlacedSameRecipientDeduped(t *testing.T) {
	db := notifyWritersTestDB(t)
	ctx := context.Background()
	customerID, storeOwnerID, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	if _, err := db.Exec(`INSERT INTO suppliers (id, owner_user_id, name) VALUES ('sup-2', $1, 'Same Owner Supplier')`, storeOwnerID); err != nil {
		t.Fatalf("seed sup-2: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO products (id, supplier_id, title, stock) VALUES ('prod-3', 'sup-2', 'کلاه', 10)`); err != nil {
		t.Fatalf("seed prod-3: %v", err)
	}
	seedNotifyOrder(t, db, "ord-12", "ORD-12", customerID, "processing", "pending")
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity) VALUES ('item-ord-12b', 'ord-12', 'prod-3', 1)`); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	notifyOrderPlaced(ctx, db, "ord-12")
	notifyPaymentConfirmed(ctx, db, "ord-12")
	kinds := outboxKinds(t, db)
	// Two suppliers fulfil this order (sup-1 + sup-2, whose owner IS
	// the store owner): 2 rows, not 3 — the store owner appears once
	// (via their supplier row), never twice for one event.
	for _, kind := range []string{"order_placed", "payment_confirmed"} {
		recipients := kinds[kind]
		if len(recipients) != 2 {
			t.Fatalf("%s rows = %v, want 2 (one per distinct recipient)", kind, recipients)
		}
		seen := 0
		for _, r := range recipients {
			if r == storeOwnerID {
				seen++
			}
		}
		if seen != 1 {
			t.Fatalf("%s: store owner appears %d times, want exactly 1", kind, seen)
		}
	}
}
