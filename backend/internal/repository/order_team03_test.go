package repository

// Team 03 live-parity tests (MASTER-AUDIT D34/D35/D36/D37/D38).
//
// Opt-in integration tests, following the Team 09 pattern: they are
// skipped unless TEAM03_DATABASE_URL points at a DISPOSABLE Postgres
// database. The test creates and drops its own minimal tables and
// must never run against a live or shared database.
//
// Covered:
//   - confirmation does NOT release profit, but records the return
//     window (confirmed_at + return_window_days, live = 7);
//   - release after the window closes DOES release profit and writes
//     the seller/credit + platform/debit ledger pair, while a
//     NULL-window order is skipped (TODO U19, no invented fallback);
//   - the D38 snapshot columns round-trip through create + read;
//   - the customer returns flow (register -> approve -> QC receive ->
//     wallet refund) without changing the order (TODO U23);
//   - cancel preserves profit fields (TODO U18);
//   - a refund dispute resolution moves order + payment transaction
//     to refunded, while a release_to_seller resolution does not.

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func openTeam03DB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEAM03_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEAM03_DATABASE_URL is not set")
	}
	db := openPrivateFinanceFixture(t, databaseURL)
	var err error
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("disposable database unavailable: %v", err)
	}
	stmts := []string{
		`DROP TABLE IF EXISTS stores,customer_return_financial_events,order_inventory_captures,order_inventory_returns,return_items, customer_returns, customer_wallets, payment_transactions, disputes, ledger_entries, referral_bonuses, order_item_origins, order_items, orders, products, supplier_warehouses, warehouses, platform_settings, customer_return_shipping_charges CASCADE`,
		`CREATE TABLE stores(id TEXT PRIMARY KEY,owner_type TEXT DEFAULT 'dropshipper'::character varying ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			name VARCHAR(255) NOT NULL DEFAULT '',
			slug VARCHAR(255) NOT NULL DEFAULT '',
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
			owner_user_id uuid,
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
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`INSERT INTO stores(id,owner_type) VALUES('store-1','dropshipper')`,
		`CREATE TABLE IF NOT EXISTS users(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,mobile TEXT,full_name TEXT,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			email VARCHAR(255),
			username VARCHAR(255),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			failed_login_attempts integer NOT NULL DEFAULT 0,
			is_active boolean NOT NULL DEFAULT true,
			locked_until timestamp with time zone,
			password_hash text,
			role character varying(30) NOT NULL DEFAULT 'customer'::character varying,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_city character varying(50) NOT NULL DEFAULT 'تهران'::character varying)`,
		`DROP SEQUENCE IF EXISTS customer_return_code_seq`,
		`CREATE SEQUENCE customer_return_code_seq`,
		`CREATE TABLE platform_settings (id INT  DEFAULT 1 PRIMARY KEY, return_window_days INT DEFAULT 7 , shipping_base_rate BIGINT DEFAULT 0 , shipping_per_kg BIGINT DEFAULT 0 , volumetric_divisor BIGINT DEFAULT 5000 , default_item_weight_grams BIGINT DEFAULT 500 ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			early_withdrawal_fee_pct NUMERIC,
			platform_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
			settlement_day INTEGER,
			supplier_contract_version INTEGER NOT NULL DEFAULT 2,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribution_model character varying(20) NOT NULL DEFAULT 'last_touch'::character varying,
			base_fee_f bigint NOT NULL DEFAULT 100000,
			central_campaign_budget bigint NOT NULL DEFAULT 0,
			central_profit_pct double precision NOT NULL DEFAULT 10,
			central_store_markup_percent numeric(5,2) NOT NULL DEFAULT 10,
			cookie_days integer NOT NULL DEFAULT 30,
			default_margin numeric(5,2) NOT NULL DEFAULT 20.00,
			default_markup_type character varying(10) NOT NULL DEFAULT 'percent'::character varying,
			default_markup_value numeric(10,2) NOT NULL DEFAULT 30,
			delivery_followup_trigger_days integer NOT NULL DEFAULT 10,
			delivery_hold_period_days integer NOT NULL DEFAULT 20,
			increase_cap_pct double precision NOT NULL DEFAULT 10,
			increase_locked boolean NOT NULL DEFAULT true,
			ippanel_api_key text NOT NULL DEFAULT ''::text,
			ippanel_base_url text NOT NULL DEFAULT 'https://edge.ippanel.com/v1'::text,
			ippanel_enabled boolean NOT NULL DEFAULT false,
			ippanel_pattern_code text NOT NULL DEFAULT ''::text,
			ippanel_sender text NOT NULL DEFAULT '+983000505'::text,
			max_dropshipper_margin_percent numeric(5,2) NOT NULL DEFAULT 100,
			max_profit_margin numeric(5,2) NOT NULL DEFAULT 35.00,
			min_profit_margin numeric(5,2) NOT NULL DEFAULT 5.00,
			packaging_tariffs jsonb,
			product_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
			referral_commission_percent numeric(5,2) NOT NULL DEFAULT 2.00,
			referral_is_enabled boolean NOT NULL DEFAULT true,
			referral_subject_to_delivery_lock boolean NOT NULL DEFAULT true,
			seller_contract_text text NOT NULL DEFAULT ''::text,
			seller_contract_version integer NOT NULL DEFAULT 2,
			storage_fee_enabled boolean NOT NULL DEFAULT false,
			storage_fee_free_days integer NOT NULL DEFAULT 30,
			storage_fee_per_kg_day bigint NOT NULL DEFAULT 2000,
			storage_fee_per_m3_day bigint NOT NULL DEFAULT 8000,
			stuck_order_expiry_enabled boolean NOT NULL DEFAULT false,
			supplier_contract_text text NOT NULL DEFAULT ''::text,
			supplier_fee_overrides jsonb NOT NULL DEFAULT '{}'::jsonb,
			supplier_return_postal_charge bigint,
			tapin_default_box_id text NOT NULL DEFAULT '4'::text,
			tapin_platform_shop_id text,
			tapin_platform_token text,
			tapin_tariff_url text,
			trial_catalog_quota integer NOT NULL DEFAULT 25,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_min_withdrawal bigint NOT NULL DEFAULT 0,
			wallet_spend_enabled boolean NOT NULL DEFAULT true,
			wallet_withdraw_enabled boolean NOT NULL DEFAULT true,
			warehouse_tariffs jsonb NOT NULL DEFAULT '{}'::jsonb)`,
		`INSERT INTO platform_settings (id, return_window_days) VALUES (1, 7)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title TEXT, stock INT NOT NULL DEFAULT 0, supplier_id TEXT, fulfillment_method TEXT DEFAULT 'warehouse'::character varying , origin_supplier_warehouse_id TEXT, warehouse_name TEXT DEFAULT 'انبار مرکزی تهران'::character varying , weight_grams INT, length_mm INT, width_mm INT, height_mm INT,
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
			image_url text,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			is_active boolean NOT NULL DEFAULT true,
			is_featured boolean DEFAULT false,
			is_original boolean NOT NULL DEFAULT true,
			is_special_deal boolean DEFAULT false,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
			measurement_source character varying(20),
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
			warranty_property_id character varying(60))`,
		`CREATE TABLE product_variants(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,product_id TEXT REFERENCES products(id),spnc TEXT,variant_name TEXT DEFAULT 'پیش‌فرض'::text ,stock INT NOT NULL DEFAULT 0,
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
			spnc_number integer,
			supplier_base_cost bigint,
			tax_class text,
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE orders (
			id VARCHAR(50) PRIMARY KEY, customer_user_id UUID, order_number VARCHAR(30), store_id TEXT, store_name TEXT,
			total_amount BIGINT NOT NULL DEFAULT 0, discount_amount BIGINT NOT NULL DEFAULT 0, seller_discount_amount BIGINT NOT NULL DEFAULT 0, discount_code TEXT, coupon_discount_amount BIGINT,
			payable_amount BIGINT NOT NULL DEFAULT 0, payment_method TEXT DEFAULT 'cash'::character varying , payment_status TEXT DEFAULT 'pending'::character varying ,
			customer_full_name TEXT, customer_phone TEXT, customer_province TEXT, customer_city TEXT,
			customer_address TEXT, customer_postal_code TEXT, status TEXT DEFAULT 'processing'::character varying , tracking_code VARCHAR(50),
			shipping_cost BIGINT NOT NULL DEFAULT 0, dropshipper_profit BIGINT NOT NULL DEFAULT 0,
			platform_profit BIGINT NOT NULL DEFAULT 0, referral_commission BIGINT NOT NULL DEFAULT 0,
			referral_commission_released BOOLEAN NOT NULL DEFAULT FALSE, referrer_seller_id TEXT,
			profit_released BOOLEAN NOT NULL DEFAULT FALSE, profit_released_at TIMESTAMPTZ,
			shipped_at TIMESTAMPTZ, confirmed_at TIMESTAMPTZ, disputed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			fulfillment_method TEXT DEFAULT 'warehouse'::character varying , wallet_amount BIGINT NOT NULL DEFAULT 0, gateway_amount BIGINT NOT NULL DEFAULT 0,
			platform_addon_amount BIGINT NOT NULL DEFAULT 0, supplier_addon_amount BIGINT NOT NULL DEFAULT 0,
			inventory_captured BOOLEAN NOT NULL DEFAULT FALSE, packaging_length_mm INT, packaging_width_mm INT,
			packaging_height_mm INT, dispute_reason TEXT, carrier TEXT, tracking_number TEXT,
			shipping_label TEXT, packed_at TIMESTAMPTZ, box_size TEXT, weight_grams INT,
			idempotency_key TEXT, return_window_ends_at TIMESTAMPTZ, shipping_quote_id TEXT
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			payment_expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval))`,
		`CREATE TABLE order_items (
			id TEXT  DEFAULT gen_random_uuid() PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT DEFAULT 1 ,
			wholesale_cost BIGINT, platform_fee BIGINT, seller_margin BIGINT, final_unit_price BIGINT, discount_amount BIGINT NOT NULL DEFAULT 0,
			variant_id UUID, variant_name TEXT, variant_spnc TEXT
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text)`,
		`CREATE TABLE ledger_entries (
			id VARCHAR(50) PRIMARY KEY, date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			target_account VARCHAR(150) NOT NULL, target_type VARCHAR(20) NOT NULL,
			type VARCHAR(10) NOT NULL, amount BIGINT NOT NULL, reason TEXT NOT NULL,
			operator VARCHAR(100) NOT NULL, related_order_or_dispute VARCHAR(50),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE referral_bonuses (id TEXT PRIMARY KEY,referrer_store_id TEXT, referred_store_id TEXT, status TEXT DEFAULT 'locked'::character varying , released_at TIMESTAMPTZ, amount BIGINT DEFAULT 100000 ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE customer_returns (
			id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, order_id VARCHAR(50) NOT NULL, customer_id UUID NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'requested', reason TEXT NOT NULL DEFAULT '',
			admin_note TEXT NOT NULL DEFAULT '', warehouse_id TEXT, prepaid BOOLEAN NOT NULL DEFAULT FALSE,
			tracking_number TEXT DEFAULT ''::text , shipped_at TIMESTAMPTZ, return_code TEXT,
			cod_refused BOOLEAN NOT NULL DEFAULT FALSE,
			return_shipping_amount BIGINT, return_shipping_bearer VARCHAR(20),
			return_shipping_settled_at TIMESTAMPTZ,
			destination_supplier_warehouse_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			return_shipping_quote_at timestamp with time zone,
			return_shipping_quote_groups jsonb,
			return_shipping_quote_source text,
			return_shipping_quote_toman bigint)`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY, name TEXT, province_name TEXT, city_name TEXT, priority INT NOT NULL DEFAULT 100, is_active BOOLEAN NOT NULL DEFAULT TRUE,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tapin_province_id INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			capacity_m3_total numeric(10,3),
			capacity_units_total integer,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			is_receiving_open boolean NOT NULL DEFAULT true,
			tapin_city_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE supplier_warehouses (id TEXT PRIMARY KEY, supplier_id TEXT, name TEXT, province_name TEXT, city_name TEXT, address TEXT, is_active BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			phone character varying(20),
			postal_code character varying(10),
			tapin_city_id integer,
			tapin_province_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id TEXT, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE customer_return_shipping_charges (
			id TEXT PRIMARY KEY, return_id UUID NOT NULL, return_item_id UUID NOT NULL,
			supplier_id VARCHAR(50) NOT NULL, bearer VARCHAR(20) NOT NULL,
			amount BIGINT NOT NULL, ledger_entry_id VARCHAR(80),
			idempotency_key VARCHAR(150) NOT NULL UNIQUE, reason TEXT NOT NULL,
			actor_user_id VARCHAR(80) NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE return_items (
			id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, return_id UUID NOT NULL, order_item_id VARCHAR(50) NOT NULL,
			variant_id UUID, qty INT NOT NULL, qc_status VARCHAR(20) NOT NULL DEFAULT 'pending', received_at TIMESTAMPTZ
		)`,
		`CREATE TABLE customer_wallets (customer_id UUID PRIMARY KEY, balance BIGINT NOT NULL DEFAULT 0, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			locked_balance bigint NOT NULL DEFAULT 0)`,
		`CREATE TABLE disputes (
			id TEXT PRIMARY KEY, order_id TEXT, order_number TEXT, type TEXT, customer_name TEXT,
			product_title TEXT, claim_amount BIGINT, reason TEXT, store_id TEXT, supplier_name TEXT,
			status TEXT DEFAULT 'pending'::character varying , responsible_party TEXT, resolution_action TEXT, resolution_date TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			financial_decision text)`,
		`CREATE TABLE payment_transactions (
			id TEXT PRIMARY KEY, order_id TEXT, order_number TEXT, gateway_token TEXT,
			rrn TEXT, card_mask TEXT, amount BIGINT, status TEXT DEFAULT 'pending'::character varying , created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			compensation_capture boolean NOT NULL DEFAULT false)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			db.Close()
			t.Fatalf("create disposable schema: %v (stmt %.60s)", err, stmt)
		}
	}
	installRefundFixtureSchema(t, db)
	// TEST-02 round 4: the CASCADE drop above also dropped the inventory_effective_measurements view; re-create it.
	createEffectiveMeasurementsView(t, db)
	return db
}

func team03InsertOrder(t *testing.T, db *sql.DB, id, customerID, status string, profit int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name) VALUES($1::uuid,$2,'Finance fixture') ON CONFLICT(id) DO NOTHING`, customerID, "t"+customerID[:12]); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO orders (id, customer_user_id, order_number, store_id, store_name, payable_amount, payment_method, payment_status, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, status, tracking_code, dropshipper_profit, profit_released, shipped_at, created_at, updated_at)
		VALUES ($1, $2::uuid, $3, 'store-1', 'Store 1', 100000, 'cash', 'paid', 'Test Customer', '09120000000', 'Tehran', 'Tehran', 'Test St 1', '1234567890', $4, 'TRK-1', $5, FALSE, CASE WHEN $4 IN ('shipped','delivered','delivered_confirmed') THEN NOW() - INTERVAL '1 day' END, NOW(), NOW())`,
		id, customerID, "ORD-"+id, status, profit)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
}

func TestTeam03ConfirmDoesNotReleaseAndReleaseAfterWindowCloseDoes(t *testing.T) {
	db := openTeam03DB(t)
	defer db.Close()
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS suppliers(id TEXT PRIMARY KEY,
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
			owner_user_id uuid,
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
			warehouse_city_source character varying(20))`); err != nil {
		t.Fatal(err)
	}
	installFIN005PrivateFixture(t, db)
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()
	orderID := "ord-team03-a"
	team03InsertOrder(t, db, orderID, customerID, "shipped", 7000)

	if err := repo.ConfirmOrderDelivery(ctx, orderID, customerID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	order, err := repo.GetOrderByID(ctx, orderID)
	if err != nil || order == nil {
		t.Fatalf("get confirmed order: %v", err)
	}
	if order.Status != "delivered_confirmed" {
		t.Fatalf("status = %q, want delivered_confirmed", order.Status)
	}
	if order.ProfitReleased {
		t.Fatalf("confirmation released profit; live staged release must keep it locked")
	}
	if order.ConfirmedAt == nil || order.ReturnWindowEndsAt == nil {
		t.Fatalf("confirmed_at or return_window_ends_at is nil after confirmation")
	}
	window := order.ReturnWindowEndsAt.Sub(*order.ConfirmedAt)
	if window < 7*24*time.Hour-2*time.Minute || window > 7*24*time.Hour+2*time.Minute {
		t.Fatalf("return window = %v after confirmation, want ~7 days", window)
	}

	// A NULL-window delivered_confirmed order must be skipped, not
	// released on an invented fallback (TODO U19).
	nullOrderID := "ord-team03-null"
	team03InsertOrder(t, db, nullOrderID, customerID, "delivered_confirmed", 5000)
	if _, err := db.Exec(`UPDATE orders SET return_window_ends_at = NULL WHERE id = $1`, nullOrderID); err != nil {
		t.Fatalf("null window: %v", err)
	}
	if released, err := repo.ReleaseClosedReturnWindows(ctx); err != nil || released != 0 {
		t.Fatalf("release with open/NULL windows = %d, %v; want 0", released, err)
	}

	if _, err := db.Exec(`UPDATE orders SET confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, orderID); err != nil {
		t.Fatalf("close window: %v", err)
	}
	released, err := repo.ReleaseClosedReturnWindows(ctx)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released != 1 {
		t.Fatalf("released = %d, want 1 (NULL-window order must stay skipped)", released)
	}
	order, _ = repo.GetOrderByID(ctx, orderID)
	if !order.ProfitReleased || order.ProfitReleasedAt == nil {
		t.Fatalf("order not released after window close")
	}
	var credit, debit int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute = $1 AND target_type = 'seller' AND type = 'credit' AND amount = 7000 AND operator = 'system:delivery-lock-releaser'`, orderID).Scan(&credit); err != nil || credit != 1 {
		t.Fatalf("seller credit ledger rows = %d, err %v; want 1", credit, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute = $1 AND target_type = 'platform' AND type = 'debit' AND amount = 7000 AND operator = 'system:delivery-lock-releaser'`, orderID).Scan(&debit); err != nil || debit != 1 {
		t.Fatalf("platform debit ledger rows = %d, err %v; want 1", debit, err)
	}
	if released, err := repo.ReleaseClosedReturnWindows(ctx); err != nil || released != 0 {
		t.Fatalf("second release = %d, %v; want 0 (no double release)", released, err)
	}
}

func TestTeam03SnapshotColumnsRoundTrip(t *testing.T) {
	db := openTeam03DB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name) VALUES($1::uuid,$2,'Snapshot customer')`, customerID, "snap-"+customerID[:12]); err != nil {
		t.Fatalf("seed customer parent: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO products (id, title, stock) VALUES ('prod-1', 'Test Product', 100)`); err != nil {
		t.Fatalf("seed product: %v", err)
	}
	packedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	windowEnds := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)
	length, width, height, weight := 100, 200, 300, 500
	disputeReason, carrier, tracking, label, box, idem := "damaged", "post", "TN-1", "LBL-1", "M", "idem-1"
	variantID, variantName, variantSPNC := uuid.NewString(), "Red", "ABC12345"
	if _, err := db.Exec(`INSERT INTO product_variants(id,product_id,spnc,variant_name) VALUES($1,'prod-1',$2,$3)`, variantID, variantSPNC, variantName); err != nil {
		t.Fatal(err)
	}
	order := domain.Order{
		ID: "ord-team03-snap", CustomerUserID: &customerID, OrderNumber: "ORD-SNAP",
		StoreID: "store-1", StoreName: "Store 1", TotalAmount: 50000, DiscountAmount: 5000,
		PayableAmount: 45000, PaymentMethod: "cash", PaymentStatus: "paid", Status: "processing",
		TrackingCode: "TRK-SNAP", DropshipperProfit: 8000, PlatformProfit: 1000,
		FulfillmentMethod: "warehouse", WalletAmount: 1000, GatewayAmount: 2000,
		PlatformAddonAmount: 300, SupplierAddonAmount: 400, InventoryCaptured: true,
		PackagingLengthMM: &length, PackagingWidthMM: &width, PackagingHeightMM: &height,
		DisputeReason: &disputeReason, Carrier: &carrier, TrackingNumber: &tracking,
		ShippingLabel: &label, PackedAt: &packedAt, BoxSize: &box, WeightGrams: &weight,
		IdempotencyKey: &idem, ReturnWindowEndsAt: &windowEnds,
		Items: []domain.OrderItem{{
			ProductID: "prod-1", Quantity: 2, WholesaleCost: 10000, PlatformFee: 0,
			SellerMargin: 4000, FinalUnitPrice: 20000,
			VariantID: &variantID, VariantName: &variantName, VariantSPNC: &variantSPNC,
		}},
	}
	if err := repo.CreateOrderWithShipments(ctx, order, nil); err != nil {
		t.Fatalf("create with snapshot columns: %v", err)
	}
	got, err := repo.GetOrderByID(ctx, order.ID)
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	if got.WalletAmount != 1000 || got.GatewayAmount != 2000 || got.PlatformAddonAmount != 300 || got.SupplierAddonAmount != 400 {
		t.Fatalf("wallet/gateway/addon round-trip = %d/%d/%d/%d", got.WalletAmount, got.GatewayAmount, got.PlatformAddonAmount, got.SupplierAddonAmount)
	}
	if !got.InventoryCaptured || got.PackagingLengthMM == nil || *got.PackagingLengthMM != 100 || got.WeightGrams == nil || *got.WeightGrams != 500 {
		t.Fatalf("packaging/inventory round-trip failed: captured=%v len=%v weight=%v", got.InventoryCaptured, got.PackagingLengthMM, got.WeightGrams)
	}
	if got.Carrier == nil || *got.Carrier != "post" || got.TrackingNumber == nil || *got.TrackingNumber != "TN-1" || got.BoxSize == nil || *got.BoxSize != "M" || got.IdempotencyKey == nil || *got.IdempotencyKey != "idem-1" {
		t.Fatalf("carrier/tracking/box/idempotency round-trip failed")
	}
	if got.ReturnWindowEndsAt == nil || got.PackedAt == nil {
		t.Fatalf("packed_at / return_window_ends_at round-trip failed")
	}
	if len(got.Items) != 1 || got.Items[0].VariantID == nil || *got.Items[0].VariantID != variantID || got.Items[0].VariantName == nil || *got.Items[0].VariantName != "Red" || got.Items[0].VariantSPNC == nil || *got.Items[0].VariantSPNC != "ABC12345" {
		t.Fatalf("order item variant round-trip failed: %+v", got.Items)
	}
}

func TestTeam03ReturnsFlowCancelAndDisputeRefund(t *testing.T) {
	db := openTeam03DB(t)
	defer db.Close()
	ctx := context.Background()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	// Returns flow.
	orderID := "ord-team03-ret"
	if _, err := db.Exec(`CREATE TABLE suppliers(id TEXT PRIMARY KEY,
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
			owner_user_id uuid,
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
			warehouse_city_source character varying(20)); INSERT INTO suppliers(id) VALUES('sup-return'); CREATE TABLE supplier_shipments(id TEXT PRIMARY KEY,order_id TEXT,status TEXT DEFAULT 'pending'::character varying ,shipped_at TIMESTAMPTZ,delivered_at TIMESTAMPTZ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tracking_number VARCHAR(255),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier character varying(100),
			created_at timestamp with time zone DEFAULT now(),
			customer_name character varying(255),
			customer_phone character varying(20),
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			send_price bigint,
			shipping_address jsonb,
			supplier_id character varying(255),
			supplier_notes text,
			tapin_box_id text,
			tapin_last_status text,
			tapin_register_attempted_at timestamp with time zone,
			tapin_registered_at timestamp with time zone,
			tapin_service text,
			updated_at timestamp with time zone DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO products(id,title,stock,weight_grams,supplier_id) VALUES('prod-1','Purchased return product',0,500,'sup-return')`); err != nil {
		t.Fatal(err)
	}
	team03InsertOrder(t, db, orderID, customerID, "delivered_confirmed", 6000)
	if _, err := db.Exec(`UPDATE orders SET return_window_ends_at = NOW() + INTERVAL '7 days', confirmed_at = NOW() WHERE id = $1`, orderID); err != nil {
		t.Fatalf("seed window: %v", err)
	}
	var itemID string
	if err := db.QueryRow(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1, $2, 'prod-1', 2, 10000, 0, 4000, 20000) RETURNING id`, uuid.NewString(), orderID).Scan(&itemID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO warehouses(id,name,province_name,city_name,is_active) VALUES('wh-return','Return warehouse','Tehran','Tehran',TRUE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,warehouse_id,origin_label,quantity) VALUES($1,$2,$3,0,'warehouse','sup-return','wh-return','Purchased origin',2)`, uuid.NewString(), orderID, itemID); err != nil {
		t.Fatal(err)
	}
	seedVerifiedRefundFixturePayment(t, db, orderID)
	ret, err := repo.CreateCustomerReturn(ctx, customerID, orderID, "wrong size", []CustomerReturnItemRequest{{OrderItemID: itemID, Qty: 1}}, CustomerReturnOptions{})
	if err != nil {
		t.Fatalf("create return: %v", err)
	}
	if ret.Status != "requested" {
		t.Fatalf("return status = %q, want requested", ret.Status)
	}
	if err := repo.ApproveCustomerReturn(ctx, ret.ID, "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// The customer ships the parcel back prepaid (FIX 14 ship action);
	// a post-paid parcel would be refused at receive (202610250202).
	prepaid := true
	if err := repo.ShipCustomerReturn(ctx, ret.ID, "TRK-RET-1", &prepaid); err != nil {
		t.Fatalf("ship return: %v", err)
	}
	if _, err := repo.ReceiveCustomerReturn(ctx, ret.ID, []CustomerReturnItemQC{{ReturnItemID: ret.Items[0].ID, QCStatus: "ok"}}, "warehouse-team03"); err != nil {
		t.Fatalf("receive QC: %v", err)
	}
	amount, _, err := repo.RefundCustomerReturn(ctx, ret.ID, "finance-team03")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if amount != 20000 {
		t.Fatalf("refund amount = %d, want 20000 (1 x snapshot final price)", amount)
	}
	var balance int64
	if err := db.QueryRow(`SELECT balance FROM customer_wallets WHERE customer_id = $1::uuid`, customerID).Scan(&balance); err != nil || balance != 20000 {
		t.Fatalf("wallet balance = %d, err %v; want 20000", balance, err)
	}
	// TODO(U23): the order is deliberately untouched by a return refund.
	order, _ := repo.GetOrderByID(ctx, orderID)
	if order.Status != "delivered_confirmed" || order.ProfitReleased {
		t.Fatalf("return refund changed the order: status=%q profitReleased=%v", order.Status, order.ProfitReleased)
	}

	// Cancel preserves profit fields (TODO U18).
	cancelID := "ord-team03-cancel"
	team03InsertOrder(t, db, cancelID, customerID, "processing", 9000)
	if err := repo.CancelOrder(ctx, cancelID, customerID, false); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	cancelled, _ := repo.GetOrderByID(ctx, cancelID)
	if cancelled.Status != "cancelled" || cancelled.PaymentStatus != "paid" || cancelled.DropshipperProfit != 9000 {
		t.Fatalf("cancel result: status=%q payment=%q profit=%d", cancelled.Status, cancelled.PaymentStatus, cancelled.DropshipperProfit)
	}

	// Dispute refund effect (D37) vs non-refund resolution.
	disputeID := "ord-team03-disp"
	team03InsertOrder(t, db, disputeID, customerID, "processing", 3000)
	if _, err := db.Exec(`INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, amount, status) VALUES ($1, $2, 'ORD-DISP', 'tok-1', 100000, 'paid')`, "pay-1", disputeID); err != nil {
		t.Fatalf("seed payment tx: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO disputes (id, order_id, order_number, type, status, resolution_action, responsible_party,claim_amount) VALUES ('disp-1', $1, 'ORD-DISP', 'delivery_undelivered', 'resolved', 'refund_carrier', 'carrier',100000)`, disputeID); err != nil {
		t.Fatalf("seed dispute: %v", err)
	}
	applied, err := repo.ApplyDisputeRefundEffect(ctx, "disp-1")
	if err != nil || !applied {
		t.Fatalf("dispute refund effect applied=%v err=%v; want true", applied, err)
	}
	disputed, _ := repo.GetOrderByID(ctx, disputeID)
	if disputed.Status != "processing" || disputed.PaymentStatus != "paid" {
		t.Fatalf("dispute refund order: status=%q payment=%q", disputed.Status, disputed.PaymentStatus)
	}
	var txStatus string
	if err := db.QueryRow(`SELECT status FROM payment_transactions WHERE order_id = $1`, disputeID).Scan(&txStatus); err != nil || txStatus != "paid" {
		t.Fatalf("payment tx status = %q, err %v; want preserved paid capture", txStatus, err)
	}
	var review int64
	if err := db.QueryRow(`SELECT SUM(amount_toman) FROM order_refund_obligations WHERE order_id=$1 AND destination='review' AND status='unknown'`, disputeID).Scan(&review); err != nil || review != 100000 {
		t.Fatalf("old unresolved dispute source lost %d %v", review, err)
	}
	if _, err := db.Exec(`INSERT INTO disputes (id, order_id, order_number, type, status, resolution_action, responsible_party) VALUES ('disp-2', $1, 'ORD-CANCEL', 'delivery_undelivered', 'resolved', 'release_to_seller', 'seller')`, cancelID); err != nil {
		t.Fatalf("seed non-refund dispute: %v", err)
	}
	applied, err = repo.ApplyDisputeRefundEffect(ctx, "disp-2")
	if err != nil || applied {
		t.Fatalf("non-refund dispute applied=%v err=%v; want false", applied, err)
	}
}
