package repository

// Return-destination derivation tests (owner rule, Oct 28 2026): a
// customer return goes back to where the order shipped FROM.
//
// Opt-in integration tests in the Team 03 pattern: skipped unless
// TEAM03_DATABASE_URL points at a DISPOSABLE Postgres database. The
// setup creates and drops its own minimal tables and must never run
// against a live or shared database.
//
// Covered:
//   - supplier-warehouse origin -> destination_supplier_warehouse_id;
//   - central-warehouse origin -> warehouse_id;
//   - mixed origins -> central consolidation (top-priority active
//     central warehouse), with NO ambiguity admin_note;
//   - legacy orders without origin rows -> product fallback set
//     (origin_supplier_warehouse_id / warehouse_name);
//   - supplier-direct with no recorded warehouse -> the supplier's
//     single active warehouse; several -> consolidation + admin_note;
//   - a client-sent warehouse id (bogus or real-but-wrong) is ignored;
//   - DeriveReturnDestination (the quote read path) for a subset;
//   - the supplier panel list reports destinationType 'supplier';
//   - the warehouse queue destination filter.

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func openReturnDestDB(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("TEAM03_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEAM03_DATABASE_URL is not set")
	}
	db, err := testdb.Open(t, databaseURL)
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
		`DROP TABLE IF EXISTS supplier_return_back_requests, return_items, customer_returns, customer_wallets, payment_transactions, disputes, ledger_entries, referral_bonuses, order_item_origins, order_items, orders, product_variants, products, supplier_warehouses, warehouses, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT  DEFAULT 1 PRIMARY KEY, return_window_days INT DEFAULT 7 ,
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
			default_item_weight_grams integer NOT NULL DEFAULT 500,
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
			shipping_base_rate bigint NOT NULL DEFAULT 0,
			shipping_per_kg bigint NOT NULL DEFAULT 0,
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
			volumetric_divisor integer NOT NULL DEFAULT 5000,
			wallet_min_withdrawal bigint NOT NULL DEFAULT 0,
			wallet_spend_enabled boolean NOT NULL DEFAULT true,
			wallet_withdraw_enabled boolean NOT NULL DEFAULT true,
			warehouse_tariffs jsonb NOT NULL DEFAULT '{}'::jsonb)`,
		`INSERT INTO platform_settings (id, return_window_days) VALUES (1, 7)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title TEXT, stock INT NOT NULL DEFAULT 0, supplier_id TEXT, fulfillment_method TEXT DEFAULT 'warehouse'::character varying , origin_supplier_warehouse_id TEXT, warehouse_name TEXT DEFAULT 'انبار مرکزی تهران'::character varying ,
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
			warranty_property_id character varying(60),
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE product_variants (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, variant_name TEXT DEFAULT 'پیش‌فرض'::text ,
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
			product_id character varying(50),
			sale_from timestamp with time zone,
			sale_price bigint,
			sale_to timestamp with time zone,
			selection jsonb NOT NULL DEFAULT '{}'::jsonb,
			shipping_class text,
			spnc character varying(12),
			spnc_number integer,
			stock integer NOT NULL DEFAULT 0,
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
			id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT DEFAULT 1 ,
			wholesale_cost BIGINT, platform_fee BIGINT, seller_margin BIGINT, final_unit_price BIGINT, discount_amount BIGINT NOT NULL DEFAULT 0,
			variant_id UUID, variant_name TEXT, variant_spnc TEXT
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text)`,
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
		`CREATE TABLE order_item_origins (
			id TEXT PRIMARY KEY, order_id TEXT, order_item_id UUID, leg_index INT, origin_type TEXT,
			supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE ledger_entries (
			id VARCHAR(50) PRIMARY KEY, date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			target_account VARCHAR(150) NOT NULL, target_type VARCHAR(20) NOT NULL,
			type VARCHAR(10) NOT NULL, amount BIGINT NOT NULL, reason TEXT NOT NULL,
			operator VARCHAR(100) NOT NULL, related_order_or_dispute VARCHAR(50),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE referral_bonuses (id TEXT PRIMARY KEY, referred_store_id TEXT, status TEXT DEFAULT 'locked'::character varying , released_at TIMESTAMPTZ, amount BIGINT DEFAULT 100000 ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			referrer_store_id character varying(50))`,
		`CREATE TABLE customer_returns (
			id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, order_id VARCHAR(50) NOT NULL, customer_id UUID NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'requested', reason TEXT NOT NULL DEFAULT '',
			admin_note TEXT NOT NULL DEFAULT '', warehouse_id TEXT, prepaid BOOLEAN NOT NULL DEFAULT FALSE,
			tracking_number TEXT DEFAULT ''::text , shipped_at TIMESTAMPTZ, return_code TEXT,
			return_shipping_amount BIGINT, return_shipping_bearer VARCHAR(20),
			destination_supplier_warehouse_id TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			cod_refused boolean NOT NULL DEFAULT false,
			return_shipping_quote_at timestamp with time zone,
			return_shipping_quote_groups jsonb,
			return_shipping_quote_source text,
			return_shipping_quote_toman bigint,
			return_shipping_settled_at timestamp with time zone)`,
		`CREATE TABLE return_items (
			id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, return_id UUID NOT NULL, order_item_id VARCHAR(50) NOT NULL,
			variant_id UUID, qty INT NOT NULL, qc_status VARCHAR(20) NOT NULL DEFAULT 'pending', received_at TIMESTAMPTZ
		)`,
		`CREATE TABLE supplier_return_back_requests (
			id VARCHAR(60) PRIMARY KEY, return_item_id UUID NOT NULL, quantity INT NOT NULL,
			status VARCHAR(30) NOT NULL DEFAULT 'requested', reason TEXT NOT NULL DEFAULT '',
			review_reason TEXT NOT NULL DEFAULT '', reviewed_at TIMESTAMPTZ, shipped_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ, carrier_code VARCHAR(40), tracking_number VARCHAR(100),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			customer_return_id uuid,
			idempotency_key character varying(150),
			inventory_source character varying(20) NOT NULL DEFAULT 'none'::character varying,
			product_id character varying(50),
			reviewed_by character varying(80),
			supplier_id character varying(50),
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
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
	// TEST-02 round 4: the CASCADE drop above also dropped the inventory_effective_measurements view; re-create it.
	createEffectiveMeasurementsView(t, db)
	return db
}

func rdWarehouse(t *testing.T, db *sql.DB, id, name, city string, priority int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO warehouses (id, name, province_name, city_name, priority) VALUES ($1, $2, 'البرز', $3, $4)`, id, name, city, priority); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
}

func rdSupplierWarehouse(t *testing.T, db *sql.DB, id, supplierID, name, city, address string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO supplier_warehouses (id, supplier_id, name, province_name, city_name, address) VALUES ($1, $2, $3, 'البرز', $4, $5)`, id, supplierID, name, city, address); err != nil {
		t.Fatalf("seed supplier warehouse: %v", err)
	}
}

func rdProduct(t *testing.T, db *sql.DB, id, supplierID, fulfillment, originSwh, warehouseName string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO products (id, title, supplier_id, fulfillment_method, origin_supplier_warehouse_id, warehouse_name) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''))`, id, "Product "+id, supplierID, fulfillment, originSwh, warehouseName); err != nil {
		t.Fatalf("seed product: %v", err)
	}
}

func rdOrderWithItem(t *testing.T, db *sql.DB, orderID, customerID, productID string, qty int) string {
	t.Helper()
	team03InsertOrder(t, db, orderID, customerID, "delivered_confirmed", 0)
	itemID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, $2, $3, $4, 10000, 0, 0, 20000)`, itemID, orderID, productID, qty); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	return itemID
}

func rdOrigin(t *testing.T, db *sql.DB, orderID, itemID, originType, supplierID, warehouseID, supplierWarehouseID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO order_item_origins (id, order_id, order_item_id, leg_index, origin_type, supplier_id, warehouse_id, supplier_warehouse_id, origin_label, quantity) VALUES ($1, $2, $3::uuid, 0, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), '', 1)`,
		uuid.NewString(), orderID, itemID, originType, supplierID, warehouseID, supplierWarehouseID); err != nil {
		t.Fatalf("seed origin: %v", err)
	}
}

func rdCreate(t *testing.T, repo *OrderRepository, customerID, orderID string, itemIDs []string, opts CustomerReturnOptions) *CustomerReturn {
	t.Helper()
	items := make([]CustomerReturnItemRequest, 0, len(itemIDs))
	for _, id := range itemIDs {
		items = append(items, CustomerReturnItemRequest{OrderItemID: id, Qty: 1})
	}
	ret, err := repo.CreateCustomerReturn(context.Background(), customerID, orderID, "test reason", items, opts)
	if err != nil {
		t.Fatalf("create return: %v", err)
	}
	return ret
}

func TestReturnDestinationFromSupplierOrigin(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "جاده مخصوص، پلاک ۱۲")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	itemID := rdOrderWithItem(t, db, "ord-rd-sup", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-sup", itemID, "supplier", "sup-1", "", "swh-1")

	ret := rdCreate(t, repo, customerID, "ord-rd-sup", []string{itemID}, CustomerReturnOptions{})
	if ret.DestinationSupplierWarehouseID != "swh-1" {
		t.Fatalf("destination supplier warehouse = %q, want swh-1", ret.DestinationSupplierWarehouseID)
	}
	if ret.WarehouseID != "" {
		t.Fatalf("warehouse id = %q, want empty for a supplier destination", ret.WarehouseID)
	}
	if ret.DestinationType != "supplier" {
		t.Fatalf("destination type = %q, want supplier", ret.DestinationType)
	}
	if ret.DestinationName != "کارخانه کرج" {
		t.Fatalf("destination name = %q, want the supplier warehouse name", ret.DestinationName)
	}
}

func TestReturnDestinationFromCentralOrigin(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdProduct(t, db, "p-wh", "", "warehouse", "", "")
	itemID := rdOrderWithItem(t, db, "ord-rd-central", customerID, "p-wh", 1)
	rdOrigin(t, db, "ord-rd-central", itemID, "warehouse", "", "wh-1", "")

	ret := rdCreate(t, repo, customerID, "ord-rd-central", []string{itemID}, CustomerReturnOptions{})
	if ret.WarehouseID != "wh-1" {
		t.Fatalf("warehouse id = %q, want wh-1", ret.WarehouseID)
	}
	if ret.DestinationSupplierWarehouseID != "" {
		t.Fatalf("destination supplier warehouse = %q, want empty", ret.DestinationSupplierWarehouseID)
	}
	if ret.DestinationType != "central" || ret.DestinationName != "انبار مرکزی قم" {
		t.Fatalf("destination = %q/%q, want central/انبار مرکزی قم", ret.DestinationType, ret.DestinationName)
	}
}

func TestReturnDestinationMixedOriginsConsolidateToCentral(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdWarehouse(t, db, "wh-2", "انبار مرکزی تهران", "تهران", 90)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	rdProduct(t, db, "p-wh", "", "warehouse", "", "")

	team03InsertOrder(t, db, "ord-rd-mixed", customerID, "delivered_confirmed", 0)
	itemA := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-mixed', 'p-sup', 1, 10000, 0, 0, 20000)`, itemA); err != nil {
		t.Fatalf("seed item A: %v", err)
	}
	itemB := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-mixed', 'p-wh', 1, 10000, 0, 0, 20000)`, itemB); err != nil {
		t.Fatalf("seed item B: %v", err)
	}
	rdOrigin(t, db, "ord-rd-mixed", itemA, "supplier", "sup-1", "", "swh-1")
	rdOrigin(t, db, "ord-rd-mixed", itemB, "warehouse", "", "wh-2", "")

	ret := rdCreate(t, repo, customerID, "ord-rd-mixed", []string{itemA, itemB}, CustomerReturnOptions{})
	if ret.DestinationSupplierWarehouseID != "" {
		t.Fatalf("mixed return kept a supplier destination %q, want consolidation", ret.DestinationSupplierWarehouseID)
	}
	if ret.WarehouseID != "wh-1" {
		t.Fatalf("mixed return warehouse = %q, want wh-1 (top-priority consolidation)", ret.WarehouseID)
	}
	if ret.AdminNote != "" {
		t.Fatalf("mixed return admin note = %q, want empty (mixed is not the ambiguous case)", ret.AdminNote)
	}
}

func TestReturnDestinationLegacyProductFallback(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-9", "sup-9", "انبار تأمین‌کننده قم", "قم", "")

	// Supplier-direct product with a recorded origin warehouse.
	rdProduct(t, db, "p-legacy-sup", "sup-9", "supplier_direct", "swh-9", "")
	itemA := rdOrderWithItem(t, db, "ord-rd-legacy-sup", customerID, "p-legacy-sup", 1)
	retA := rdCreate(t, repo, customerID, "ord-rd-legacy-sup", []string{itemA}, CustomerReturnOptions{})
	if retA.DestinationSupplierWarehouseID != "swh-9" {
		t.Fatalf("legacy supplier fallback = %q, want swh-9", retA.DestinationSupplierWarehouseID)
	}

	// Warehouse product whose warehouse_name names a central warehouse.
	rdProduct(t, db, "p-legacy-wh", "", "warehouse", "", "انبار مرکزی قم")
	itemB := rdOrderWithItem(t, db, "ord-rd-legacy-wh", customerID, "p-legacy-wh", 1)
	retB := rdCreate(t, repo, customerID, "ord-rd-legacy-wh", []string{itemB}, CustomerReturnOptions{})
	if retB.WarehouseID != "wh-1" || retB.DestinationType != "central" {
		t.Fatalf("legacy warehouse fallback = %q/%q, want wh-1/central", retB.WarehouseID, retB.DestinationType)
	}
}

func TestReturnDestinationSupplierWarehouseResolution(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)

	// Exactly one active warehouse -> that one.
	rdSupplierWarehouse(t, db, "swh-2", "sup-2", "انبار یگانه", "قم", "")
	rdProduct(t, db, "p-one", "sup-2", "supplier_direct", "", "")
	itemA := rdOrderWithItem(t, db, "ord-rd-one", customerID, "p-one", 1)
	retA := rdCreate(t, repo, customerID, "ord-rd-one", []string{itemA}, CustomerReturnOptions{})
	if retA.DestinationSupplierWarehouseID != "swh-2" {
		t.Fatalf("single-warehouse supplier fallback = %q, want swh-2", retA.DestinationSupplierWarehouseID)
	}

	// Several active warehouses, none recorded -> ambiguous:
	// consolidation + admin_note.
	rdSupplierWarehouse(t, db, "swh-3a", "sup-3", "انبار الف", "قم", "")
	rdSupplierWarehouse(t, db, "swh-3b", "sup-3", "انبار ب", "قم", "")
	rdProduct(t, db, "p-many", "sup-3", "supplier_direct", "", "")
	itemB := rdOrderWithItem(t, db, "ord-rd-many", customerID, "p-many", 1)
	retB := rdCreate(t, repo, customerID, "ord-rd-many", []string{itemB}, CustomerReturnOptions{})
	if retB.DestinationSupplierWarehouseID != "" || retB.WarehouseID != "wh-1" {
		t.Fatalf("ambiguous supplier destination = swh %q / wh %q, want consolidation wh-1", retB.DestinationSupplierWarehouseID, retB.WarehouseID)
	}
	if retB.AdminNote == "" {
		t.Fatalf("ambiguous supplier destination must record the ambiguity in admin_note")
	}
}

func TestReturnDestinationClientWarehouseIgnored(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")

	// A bogus free-text id: the old code rejected the filing with
	// "warehouse not found"; the derived destination must win now.
	itemA := rdOrderWithItem(t, db, "ord-rd-bogus", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-bogus", itemA, "supplier", "sup-1", "", "swh-1")
	retA := rdCreate(t, repo, customerID, "ord-rd-bogus", []string{itemA}, CustomerReturnOptions{WarehouseID: "انبار دم دریا"})
	if retA.DestinationSupplierWarehouseID != "swh-1" {
		t.Fatalf("bogus client warehouse: destination = %q, want derived swh-1", retA.DestinationSupplierWarehouseID)
	}

	// A real but wrong central warehouse id: derived still wins.
	itemB := rdOrderWithItem(t, db, "ord-rd-wrong", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-wrong", itemB, "supplier", "sup-1", "", "swh-1")
	retB := rdCreate(t, repo, customerID, "ord-rd-wrong", []string{itemB}, CustomerReturnOptions{WarehouseID: "wh-1"})
	if retB.DestinationSupplierWarehouseID != "swh-1" || retB.WarehouseID != "" {
		t.Fatalf("wrong client warehouse: destination = swh %q / wh %q, want derived swh-1 only", retB.DestinationSupplierWarehouseID, retB.WarehouseID)
	}
}

func TestDeriveReturnDestinationForQuoteSubset(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	ctx := context.Background()
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "جاده مخصوص، پلاک ۱۲")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	rdProduct(t, db, "p-wh", "", "warehouse", "", "")

	team03InsertOrder(t, db, "ord-rd-quote", customerID, "delivered_confirmed", 0)
	itemA := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-quote', 'p-sup', 1, 10000, 0, 0, 20000)`, itemA); err != nil {
		t.Fatalf("seed item A: %v", err)
	}
	itemB := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price) VALUES ($1::uuid, 'ord-rd-quote', 'p-wh', 1, 10000, 0, 0, 20000)`, itemB); err != nil {
		t.Fatalf("seed item B: %v", err)
	}
	rdOrigin(t, db, "ord-rd-quote", itemA, "supplier", "sup-1", "", "swh-1")
	rdOrigin(t, db, "ord-rd-quote", itemB, "warehouse", "", "wh-1", "")

	only, err := repo.DeriveReturnDestination(ctx, "ord-rd-quote", []string{itemA})
	if err != nil {
		t.Fatalf("derive subset: %v", err)
	}
	if only.Type != "supplier" || only.SupplierWarehouseID != "swh-1" || only.City != "کرج" || only.Address == "" {
		t.Fatalf("subset destination = %+v, want supplier swh-1 in کرج with an address", only)
	}
	both, err := repo.DeriveReturnDestination(ctx, "ord-rd-quote", []string{itemA, itemB})
	if err != nil {
		t.Fatalf("derive both: %v", err)
	}
	if both.Type != "central" || both.WarehouseID != "wh-1" {
		t.Fatalf("mixed subset destination = %+v, want central consolidation wh-1", both)
	}
}

func TestSupplierCustomerReturnsShowSupplierDestination(t *testing.T) {
	db := openReturnDestDB(t)
	defer db.Close()
	repo := NewOrderRepository(db)
	supplierRepo := NewSupplierRepository(db)
	ctx := context.Background()
	customerID := uuid.NewString()

	rdWarehouse(t, db, "wh-1", "انبار مرکزی قم", "قم", 10)
	rdSupplierWarehouse(t, db, "swh-1", "sup-1", "کارخانه کرج", "کرج", "")
	rdProduct(t, db, "p-sup", "sup-1", "supplier_direct", "", "")
	itemID := rdOrderWithItem(t, db, "ord-rd-panel", customerID, "p-sup", 1)
	rdOrigin(t, db, "ord-rd-panel", itemID, "supplier", "sup-1", "", "swh-1")
	ret := rdCreate(t, repo, customerID, "ord-rd-panel", []string{itemID}, CustomerReturnOptions{})

	list, err := supplierRepo.ListSupplierCustomerReturns(ctx, "sup-1")
	if err != nil {
		t.Fatalf("supplier returns list: %v", err)
	}
	var found *SupplierCustomerReturn
	for i := range list {
		if list[i].ReturnID == ret.ID {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("created return not in the supplier list (%d rows)", len(list))
	}
	if found.DestinationType == nil || *found.DestinationType != "supplier" {
		t.Fatalf("supplier list destinationType = %v, want supplier", found.DestinationType)
	}
	if found.DestinationName == nil || *found.DestinationName != "کارخانه کرج" {
		t.Fatalf("supplier list destinationName = %v, want کارخانه کرج", found.DestinationName)
	}
	if found.WarehouseCity == nil || *found.WarehouseCity != "کرج" {
		t.Fatalf("supplier list warehouseCity = %v, want کرج", found.WarehouseCity)
	}
}

func TestWarehouseQueueDestinationFilter(t *testing.T) {
	// Exercise the production query against the canonical migrated schema.
	// Destination derivation DTO fixtures intentionally omit inventory receipt
	// history and cannot represent refunded-but-unshelved work.
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	repo := NewOrderRepository(db)
	ctx := context.Background()
	warehouse, supplierWarehouse := "wh-"+uuid.NewString(), "swh-"+uuid.NewString()
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'test','test')`, warehouse)
	opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'test','test')`, supplierWarehouse, f.supplier)
	file := func(kind string) *CustomerReturn {
		order, _ := opsOrder(t, db, f, true)
		opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW(),return_window_ends_at=NOW()+INTERVAL '7 days' WHERE id=$1`, order)
		var item string
		if err := db.QueryRow(`SELECT id::text FROM order_items WHERE order_id=$1`, order).Scan(&item); err != nil {
			t.Fatal(err)
		}
		if kind == "warehouse" {
			seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
		} else {
			opsExec(t, db, `INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,supplier_warehouse_id,origin_label,quantity) VALUES($1,$2,$3,0,'supplier',$4,$5,'recorded supplier origin',1)`, uuid.NewString(), order, item, f.supplier, supplierWarehouse)
		}
		ret, err := repo.CreateCustomerReturn(ctx, f.user, order, "destination filter", []CustomerReturnItemRequest{{OrderItemID: item, Qty: 1}}, CustomerReturnOptions{Prepaid: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.ApproveCustomerReturn(ctx, ret.ID, ""); err != nil {
			t.Fatal(err)
		}
		return ret
	}
	retSup, retWh := file("supplier"), file("warehouse")

	all, err := repo.ListCustomerReturnsForWarehouse(ctx, "")
	if err != nil {
		t.Fatalf("unfiltered queue: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered queue = %d returns, want 2", len(all))
	}
	supplierQueue, err := repo.ListCustomerReturnsForWarehouse(ctx, supplierWarehouse)
	if err != nil {
		t.Fatalf("supplier queue: %v", err)
	}
	if len(supplierQueue) != 1 || supplierQueue[0].ID != retSup.ID {
		t.Fatalf("supplier queue = %+v, want only the supplier-destined return", supplierQueue)
	}
	centralQueue, err := repo.ListCustomerReturnsForWarehouse(ctx, warehouse)
	if err != nil {
		t.Fatalf("central queue: %v", err)
	}
	if len(centralQueue) != 1 || centralQueue[0].ID != retWh.ID {
		t.Fatalf("central queue = %+v, want only the central-destined return", centralQueue)
	}
}
