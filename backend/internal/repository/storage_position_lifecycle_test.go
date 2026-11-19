package repository

// Storage-position lifecycle tests (gap 7 follow-up). DB-gated like
// the sibling suites: they run ONLY when TEAM04_TEST_DATABASE_URL
// points at a DISPOSABLE Postgres; otherwise they skip. The DDL is a
// minimal subset of the real migrations, sufficient for the flows
// under test: inbound QC pass (RecordQCPass / CompleteQC), order
// ShipOrder, supplier stock-return HandoverReturn, and the fee
// engine's RunStorageFeeAssessment.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func storagePositionSeed(t *testing.T, db *sql.DB) {
	t.Helper()
	ddl := []string{
		`DROP TABLE IF EXISTS warehouse_storage_fee_charges, warehouse_storage_positions, supplier_return_events, supplier_return_parcels, supplier_return_requests, inventory_reservations, notification_outbox, supplier_shipments, order_item_origins, order_items, orders, putaway_tasks, qc_results, inbound_shipments, inventory_movements, inventory_balances, products, warehouses, suppliers, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT  DEFAULT 1 PRIMARY KEY, storage_fee_enabled BOOLEAN NOT NULL DEFAULT false, storage_fee_free_days INT NOT NULL DEFAULT 30, storage_fee_per_kg_day BIGINT NOT NULL DEFAULT 2000, storage_fee_per_m3_day BIGINT NOT NULL DEFAULT 8000,
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
			return_window_days integer NOT NULL DEFAULT 7,
			seller_contract_text text NOT NULL DEFAULT ''::text,
			seller_contract_version integer NOT NULL DEFAULT 2,
			shipping_base_rate bigint NOT NULL DEFAULT 0,
			shipping_per_kg bigint NOT NULL DEFAULT 0,
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
		`INSERT INTO platform_settings (id) VALUES (1)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY,
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
			warehouse_city_source character varying(20))`,
		`INSERT INTO suppliers (id) VALUES ('sup-1')`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tapin_province_id INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			capacity_m3_total numeric(10,3),
			capacity_units_total integer,
			city_name text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			is_active boolean NOT NULL DEFAULT true,
			is_receiving_open boolean NOT NULL DEFAULT true,
			name text,
			priority integer NOT NULL DEFAULT 100,
			province_name text,
			tapin_city_id integer,
			updated_at timestamp with time zone NOT NULL DEFAULT now())`,
		`INSERT INTO warehouses (id) VALUES ('wh-1')`,
		// prod-measured: 1500 g, 500x400x300 mm -> unit day 3480 Toman
		// at the default tariffs (see storage_fee_repo_test.go).
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, supplier_id VARCHAR(50), fulfillment_method TEXT DEFAULT 'warehouse'::character varying , stock INT NOT NULL DEFAULT 0, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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
			title character varying(255),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60))`,
		`INSERT INTO products (id, supplier_id, fulfillment_method, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-measured', 'sup-1', 'warehouse', 500, 400, 300, 1500)`,
		`INSERT INTO products (id, supplier_id, fulfillment_method, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-b', 'sup-1', 'warehouse', 500, 400, 300, 1500)`,
		`INSERT INTO products (id, supplier_id, fulfillment_method) VALUES ('prod-legacy', 'sup-1', 'warehouse')`,
		`CREATE TABLE inbound_shipments (id TEXT PRIMARY KEY, supplier_id VARCHAR(50), items_count INT NOT NULL DEFAULT 0, status TEXT  DEFAULT 'in_transit'::character varying NOT NULL, warehouse_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier character varying(100),
			cartons_count integer,
			completed_at timestamp with time zone,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			dispatch_at timestamp with time zone,
			dispatched_at timestamp with time zone,
			expected_arrival timestamp with time zone,
			items jsonb DEFAULT '[]'::jsonb,
			notes text,
			received_at timestamp with time zone,
			scanned_items jsonb DEFAULT '[]'::jsonb,
			started_receiving_at timestamp with time zone,
			supplier_name character varying(100),
			supplier_warehouse_id text,
			truck_plate character varying(50),
			warehouse_name character varying(100) NOT NULL DEFAULT 'انبار مرکزی تهران'::character varying)`,
		`CREATE TABLE qc_results (id TEXT PRIMARY KEY, product_id VARCHAR(50), inbound_id TEXT, inspector_user_id TEXT, status TEXT, defect_reason TEXT, quantity_passed INT DEFAULT 0 , quantity_failed INT DEFAULT 0 , created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE inventory_movements (id TEXT PRIMARY KEY, product_id VARCHAR(50), supplier_id VARCHAR(50), quantity INT, from_status TEXT, to_status TEXT, from_location_key TEXT, to_location_key TEXT, reference_type TEXT, reference_id TEXT, idempotency_key TEXT UNIQUE, actor_user_id TEXT, metadata JSONB DEFAULT '{}'::jsonb,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			variant_id uuid)`,
		`CREATE TABLE inventory_balances (id TEXT PRIMARY KEY, product_id VARCHAR(50), supplier_id VARCHAR(50), owner_type TEXT, location_type TEXT, location_key TEXT, status TEXT, quantity INT NOT NULL DEFAULT 0, reconciled BOOLEAN NOT NULL DEFAULT FALSE, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), 
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			warehouse_id TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			bin_code character varying(30),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			supplier_warehouse_id text,
			variant_id uuid,
			warehouse_name character varying(100))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS inventory_balances_unallocated_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS inventory_balances_sku_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL`,
		`CREATE TABLE putaway_tasks (id TEXT PRIMARY KEY, product_id VARCHAR(50), inbound_id TEXT, bin_location TEXT, quantity INT DEFAULT 0 , status TEXT DEFAULT 'pending'::character varying , created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			assigned_to character varying(50),
			completed_at timestamp with time zone,
			inventory_source_location_key text,
			variant_id uuid,
			warehouse_id text)`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, order_number VARCHAR(30), store_name TEXT, customer_full_name TEXT, customer_user_id UUID, store_id TEXT, total_amount BIGINT NOT NULL DEFAULT 0, status TEXT DEFAULT 'processing'::character varying , fulfillment_method TEXT DEFAULT 'warehouse'::character varying , created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), discount_amount BIGINT NOT NULL DEFAULT 0, shipping_cost BIGINT NOT NULL DEFAULT 0, payable_amount BIGINT NOT NULL DEFAULT 0, payment_status TEXT DEFAULT 'pending'::character varying , customer_phone TEXT, customer_province TEXT, customer_city TEXT, customer_address TEXT, customer_postal_code TEXT, tracking_code VARCHAR(50), tracking_number TEXT, carrier TEXT, shipping_label TEXT, packed_at TIMESTAMPTZ, shipped_at TIMESTAMPTZ, box_size TEXT, weight_grams INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			discount_code text,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
			gateway_amount bigint NOT NULL DEFAULT 0,
			idempotency_key text,
			inventory_captured boolean NOT NULL DEFAULT false,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
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
			shipping_quote_id text,
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0)`,
		`CREATE TABLE order_items (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, order_id TEXT, product_id TEXT, quantity INT DEFAULT 1 , final_unit_price BIGINT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			discount_amount bigint NOT NULL DEFAULT 0,
			platform_fee bigint,
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text,
			seller_margin bigint,
			variant_id uuid,
			variant_name text,
			variant_spnc character varying(12),
			wholesale_cost bigint)`,
		`CREATE TABLE order_item_origins (id TEXT PRIMARY KEY, order_id TEXT, order_item_id UUID, leg_index INT, origin_type TEXT, supplier_id TEXT, warehouse_id TEXT, supplier_warehouse_id TEXT, origin_label TEXT, quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now())`,
		`CREATE TABLE supplier_shipments (id TEXT PRIMARY KEY, order_id TEXT, status TEXT DEFAULT 'pending'::character varying ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			tracking_number VARCHAR(255),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier character varying(100),
			created_at timestamp with time zone DEFAULT now(),
			customer_name character varying(255),
			customer_phone character varying(20),
			delivered_at timestamp with time zone,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			send_price bigint,
			shipped_at timestamp with time zone,
			shipping_address jsonb,
			supplier_id character varying(255),
			supplier_notes text,
			tapin_box_id text,
			tapin_last_status text,
			tapin_register_attempted_at timestamp with time zone,
			tapin_registered_at timestamp with time zone,
			tapin_service text,
			updated_at timestamp with time zone DEFAULT now())`,
		`CREATE TABLE notification_outbox (recipient_user_id TEXT, kind TEXT, title TEXT, body TEXT, payload JSONB DEFAULT '{}'::jsonb ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id uuid NOT NULL DEFAULT gen_random_uuid(),
			read_at timestamp with time zone,
			sent_at timestamp with time zone,
			status character varying(20) NOT NULL DEFAULT 'pending'::character varying)`,
		`CREATE TABLE supplier_return_requests (id TEXT PRIMARY KEY, supplier_id VARCHAR(50), product_id VARCHAR(50), requested_quantity INT, status TEXT DEFAULT 'awaiting_return_test'::character varying , handover_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			accepted_quantity integer NOT NULL DEFAULT 0,
			cancelled_by character varying(80),
			collection_method character varying(30),
			completed_at timestamp with time zone,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			failed_quantity integer NOT NULL DEFAULT 0,
			idempotency_key character varying(150),
			inspected_at timestamp with time zone,
			inspection_status character varying(20),
			inventory_reference_id character varying(80),
			postal_charge bigint NOT NULL DEFAULT 0,
			reason text,
			received_at timestamp with time zone,
			rejection_reason text,
			reviewed_at timestamp with time zone,
			reviewed_by character varying(80),
			variant_id uuid,
			warehouse_bin_code character varying(30))`,
		`CREATE TABLE supplier_return_parcels (id TEXT PRIMARY KEY, return_request_id TEXT, status TEXT DEFAULT 'draft'::character varying , handed_over_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			carrier_code character varying(50),
			confirmed_by character varying(80),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			created_by character varying(80),
			destination character varying(200),
			estimate_reference character varying(150),
			estimate_source character varying(20),
			estimated_charge bigint,
			final_charge bigint,
			final_charge_reference character varying(150),
			final_charge_source character varying(20),
			height_mm integer,
			idempotency_key character varying(150),
			length_mm integer,
			notes text,
			origin character varying(200),
			parcel_number integer,
			payment_method character varying(20),
			tracking_number character varying(150),
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE inventory_reservations (id TEXT PRIMARY KEY, product_id VARCHAR(50), supplier_id VARCHAR(50), location_key TEXT, quantity INT, reference_type TEXT, reference_id TEXT, status TEXT DEFAULT 'active'::character varying , idempotency_key TEXT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			released_at timestamp with time zone,
			variant_id uuid)`,
		`CREATE TABLE supplier_return_events (id TEXT PRIMARY KEY, return_request_id TEXT, from_status TEXT, to_status TEXT, actor_user_id TEXT, idempotency_key TEXT UNIQUE,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			reason text)`,
		`CREATE TABLE warehouse_storage_positions (id TEXT PRIMARY KEY, product_id VARCHAR(50) NOT NULL, supplier_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, inbound_id TEXT, goods_class VARCHAR(20) NOT NULL DEFAULT 'normal', qty INT NOT NULL, stocked_at TIMESTAMPTZ NOT NULL, original_stocked_at TIMESTAMPTZ NOT NULL, last_fee_date DATE, last_assessed_date DATE, total_fee_charged_toman BIGINT NOT NULL DEFAULT 0, closed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE warehouse_storage_fee_charges (id TEXT PRIMARY KEY, position_id TEXT NOT NULL, supplier_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, fee_date DATE NOT NULL, qty INT NOT NULL, fee_unit_day_toman BIGINT NOT NULL, amount_toman BIGINT NOT NULL CHECK (amount_toman > 0), goods_class VARCHAR(20) NOT NULL, estimated BOOLEAN NOT NULL DEFAULT FALSE, journal_debit_id TEXT, journal_credit_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (position_id, fee_date))`,
		// TEST-02 round 5: re-seed the migration-canonical suppliers
		// (sup-ali from 000011_direct_dropship; sup-khanoumi /
		// sup-technolife / sup-filimo from 000002_seed_data). This
		// fixture DROP TABLEs suppliers, deleting those rows for every
		// later test in the shared disposable DB; tests that lock those
		// accounts then fail at the row guard ("no rows in result
		// set"). ON CONFLICT DO NOTHING keeps it safe if a row already
		// exists. The fixture's own suppliers use different ids.
		`INSERT INTO suppliers (id, name) VALUES ('sup-ali',''),('sup-filimo',''),('sup-khanoumi',''),('sup-technolife','') ON CONFLICT (id) DO NOTHING`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("storage-position seed DDL failed: %v", err)
		}
	}
	for _, q := range []string{
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS discount_code TEXT NOT NULL DEFAULT '',ADD COLUMN IF NOT EXISTS seller_discount_amount BIGINT NOT NULL DEFAULT 0,ADD COLUMN IF NOT EXISTS coupon_discount_amount BIGINT`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS schema_version INT NOT NULL DEFAULT 0`,
		`ALTER TABLE inventory_balances ADD COLUMN IF NOT EXISTS variant_id UUID`,
		`CREATE TABLE IF NOT EXISTS product_variants(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,product_id TEXT,inventory_mode TEXT DEFAULT 'legacy'::text ,is_active BOOLEAN DEFAULT TRUE,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attributes jsonb NOT NULL DEFAULT '{}'::jsonb,
			combination_key text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			height_mm integer,
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
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			tax_class text,
			variant_name text NOT NULL DEFAULT 'پیش‌فرض'::text,
			weight_grams integer,
			width_mm integer)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// seedInboundForQC inserts a 'received' inbound plus the physical
// balance RecordQCPass/CompleteQC will move into sellable.
func seedInboundForQC(t *testing.T, db *sql.DB, inboundID, productID, warehouseID string, physicalQty int) {
	t.Helper()
	wh := "NULL"
	args := []interface{}{inboundID, physicalQty}
	if warehouseID != "" {
		wh = "$3"
		args = append(args, warehouseID)
	}
	if _, err := db.Exec(`INSERT INTO inbound_shipments (id, supplier_id, items_count, status, warehouse_id) VALUES ($1, 'sup-1', $2, 'received', `+wh+`)`, args...); err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
		VALUES ('BAL-PHYS-' || $1, $2, 'sup-1', 'supplier', 'inbound', 'inbound:' || $1, 'physical', $3, TRUE)`, inboundID, productID, physicalQty); err != nil {
		t.Fatalf("seed physical balance: %v", err)
	}
}

type positionState struct {
	id                string
	qty               int
	closed            bool
	inboundID         string
	originalStockedAt time.Time
}

func openPositions(t *testing.T, db *sql.DB, productID string) []positionState {
	t.Helper()
	rows, err := db.Query(`SELECT id, qty, closed_at IS NOT NULL, COALESCE(inbound_id, ''), original_stocked_at
		FROM warehouse_storage_positions WHERE product_id = $1 ORDER BY stocked_at ASC, id ASC`, productID)
	if err != nil {
		t.Fatalf("read positions: %v", err)
	}
	defer rows.Close()
	out := []positionState{}
	for rows.Next() {
		var p positionState
		if err := rows.Scan(&p.id, &p.qty, &p.closed, &p.inboundID, &p.originalStockedAt); err != nil {
			t.Fatalf("scan position: %v", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("positions rows: %v", err)
	}
	return out
}

func TestStoragePositionCreatedByQCPassAndToppedUp(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := "w-" + uuid.NewString()
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	first := inv09ReceiveLegacy(t, db, f, warehouse, 10)
	if err := repo.RecordQCPass(ctx, f.product, first, f.user, 6); err != nil {
		t.Fatalf("RecordQCPass #1: %v", err)
	}
	if positions := openPositions(t, db, f.product); len(positions) != 1 {
		t.Fatalf("QC must own one receipt position: %+v", positions)
	}
	inv09PlacePending(t, db, f, first, warehouse)
	positions := openPositions(t, db, f.product)
	if len(positions) != 1 {
		t.Fatalf("positions after first QC pass = %d, want 1", len(positions))
	}
	p := positions[0]
	if p.qty != 6 || p.closed || p.inboundID != first {
		t.Fatalf("position = %+v, want qty 6, open, inbound INB-1", p)
	}
	firstOriginal := p.originalStockedAt

	// A second receipt has its own age, source and initial units.
	second := inv09ReceiveLegacy(t, db, f, warehouse, 5)
	if err := repo.RecordQCPass(ctx, f.product, second, f.user, 5); err != nil {
		t.Fatal(err)
	}
	inv09PlacePending(t, db, f, second, warehouse)
	positions = openPositions(t, db, f.product)
	if len(positions) != 2 || positions[0].qty != 6 || positions[1].qty != 5 || positions[0].inboundID != first || positions[1].inboundID != second || !positions[0].originalStockedAt.Equal(firstOriginal) || !positions[1].originalStockedAt.After(firstOriginal) {
		t.Fatalf("receipt cohorts merged/re-aged: %+v", positions)
	}
	if err := repo.RecordQCPass(ctx, f.product, second, f.user, 5); err != nil {
		t.Fatal(err)
	}
	positions = openPositions(t, db, f.product)
	if len(positions) != 2 || positions[0].qty+positions[1].qty != 11 {
		t.Fatalf("QC replay duplicated quantity: %+v", positions)
	}

}

func TestStoragePositionCreatedByCompleteQC(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := "w-" + uuid.NewString()
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	inbound := inv09ReceiveLegacy(t, db, f, warehouse, 4)
	if err := repo.CompleteQC(ctx, f.product, inbound, f.user, 4, 0, 0, "ok"); err != nil {
		t.Fatalf("CompleteQC: %v", err)
	}
	if positions := openPositions(t, db, f.product); len(positions) != 1 {
		t.Fatalf("QC must own one receipt position: %+v", positions)
	}
	inv09PlacePending(t, db, f, inbound, warehouse)
	positions := openPositions(t, db, f.product)
	if len(positions) != 1 || positions[0].qty != 4 || positions[0].closed {
		t.Fatalf("positions = %+v, want one open position of 4", positions)
	}
	// Replay: CompleteQC's own completed-guard makes it a no-op.
	if err := repo.CompleteQC(ctx, f.product, inbound, f.user, 4, 0, 0, "ok"); err != nil {
		t.Fatalf("CompleteQC replay: %v", err)
	}
	positions = openPositions(t, db, f.product)
	if len(positions) != 1 || positions[0].qty != 4 {
		t.Fatalf("after replay positions = %+v, want one position of 4", positions)
	}
}

func TestStoragePositionQCWithoutWarehouseFailsClosed(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	seedInboundForQC(t, db, "INB-4", "prod-b", "", 3)
	if err := repo.RecordQCPass(ctx, "prod-b", "INB-4", "inspector-1", 3); !errors.Is(err, ErrInboundProvenance) {
		t.Fatalf("RecordQCPass (no warehouse): %v", err)
	}
	if positions := openPositions(t, db, "prod-b"); len(positions) != 0 {
		t.Fatalf("positions = %+v, want none (inbound has no warehouse to attribute)", positions)
	}
	// Missing immutable origin requires reconciliation and no credit.
	var sellable int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity), 0) FROM inventory_balances WHERE product_id = 'prod-b' AND status = 'sellable'`).Scan(&sellable); err != nil || sellable != 0 {
		t.Fatalf("sellable balance = %d (err %v), want 0", sellable, err)
	}
}

func seedPackedOrder(t *testing.T, db *sql.DB, orderID, itemID, productID string, qty int, originType, originWarehouse string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO orders (id, order_number, customer_user_id, status, payment_status, fulfillment_method)
		VALUES ($1, $1, '11111111-1111-1111-1111-111111111111', 'packed', 'paid', 'warehouse')`, orderID); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, quantity, final_unit_price) VALUES ($1, $2, $3, $4, 1000)`, itemID, orderID, productID, qty); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	wh := "NULL"
	args := []interface{}{"ORG-" + orderID, orderID, itemID, originType, qty}
	if originWarehouse != "" {
		wh = "$6"
		args = append(args, originWarehouse)
	}
	if _, err := db.Exec(`INSERT INTO order_item_origins (id, order_id, order_item_id, leg_index, origin_type, supplier_id, warehouse_id, origin_label, quantity)
		VALUES ($1, $2, $3, 0, $4, 'sup-1', `+wh+`, 'test', $5)`, args...); err != nil {
		t.Fatalf("seed origin: %v", err)
	}
}

func TestStoragePositionShipOrderFIFOSplit(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	// Two open positions for prod-measured: oldest 3, newer 4.
	old := storageStockedAt(10)
	newer := storageStockedAt(5)
	if _, err := db.Exec(`INSERT INTO warehouse_storage_positions (id, product_id, supplier_id, warehouse_id, qty, stocked_at, original_stocked_at)
		VALUES ('WSP-OLD', 'prod-measured', 'sup-1', 'wh-1', 3, $1, $1), ('WSP-NEW', 'prod-measured', 'sup-1', 'wh-1', 4, $2, $2)`, old, newer); err != nil {
		t.Fatalf("seed positions: %v", err)
	}
	// A supplier-direct product with a position that must NOT move.
	if _, err := db.Exec(`INSERT INTO warehouse_storage_positions (id, product_id, supplier_id, warehouse_id, qty, stocked_at, original_stocked_at)
		VALUES ('WSP-DIRECT', 'prod-b', 'sup-1', 'wh-1', 7, $1, $1)`, newer); err != nil {
		t.Fatalf("seed direct position: %v", err)
	}

	seedPackedOrder(t, db, "ORD-1", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "prod-measured", 5, "warehouse", "wh-1")
	if err := repo.ShipOrder(ctx, "ORD-1", "post", "TRK-1"); err != nil {
		t.Fatalf("ShipOrder: %v", err)
	}
	positions := openPositions(t, db, "prod-measured")
	if len(positions) != 2 {
		t.Fatalf("positions = %+v, want both rows retained", positions)
	}
	if positions[0].id != "WSP-OLD" || positions[0].qty != 0 || !positions[0].closed {
		t.Fatalf("oldest position = %+v, want qty 0 and closed (FIFO drains it first)", positions[0])
	}
	if positions[1].id != "WSP-NEW" || positions[1].qty != 2 || positions[1].closed {
		t.Fatalf("newer position = %+v, want qty 2 and open (split remainder)", positions[1])
	}

	// Supplier-direct item ships without touching its position.
	seedPackedOrder(t, db, "ORD-2", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "prod-b", 2, "supplier", "")
	if err := repo.ShipOrder(ctx, "ORD-2", "post", "TRK-2"); err != nil {
		t.Fatalf("ShipOrder direct: %v", err)
	}
	direct := openPositions(t, db, "prod-b")
	if len(direct) != 1 || direct[0].qty != 7 || direct[0].closed {
		t.Fatalf("supplier-direct position = %+v, want untouched qty 7 open", direct)
	}
}

func TestStoragePositionShipLegacyStockWithoutPositionsFailsClosed(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewWarehouseRepository(db)

	seedPackedOrder(t, db, "ORD-3", "cccccccc-cccc-cccc-cccc-cccccccccccc", "prod-legacy", 2, "warehouse", "wh-1")
	if err := repo.ShipOrder(ctx, "ORD-3", "post", "TRK-3"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("ShipOrder with unresolved named-origin coverage: %v", err)
	}
	if positions := openPositions(t, db, "prod-legacy"); len(positions) != 0 {
		t.Fatalf("positions = %+v, want none (legacy stock has no positions to decrement)", positions)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM orders WHERE id = 'ORD-3'`).Scan(&status); err != nil || status != "packed" {
		t.Fatalf("order status = %q (err %v), want unchanged packed", status, err)
	}
}

func TestStoragePositionHandoverReturnDecrement(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	ctx := context.Background()
	repo := NewSupplierRepository(db)

	if _, err := db.Exec(`INSERT INTO warehouse_storage_positions (id, product_id, supplier_id, warehouse_id, qty, stocked_at, original_stocked_at)
		VALUES ('WSP-RET', 'prod-measured', 'sup-1', 'wh-1', 5, NOW(), NOW())`); err != nil {
		t.Fatalf("seed position: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO supplier_return_requests (id, supplier_id, product_id, requested_quantity, status) VALUES ('RET-1', 'sup-1', 'prod-measured', 3, 'packed')`); err != nil {
		t.Fatalf("seed return: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO supplier_return_parcels (id, return_request_id, status) VALUES ('PAR-1', 'RET-1', 'confirmed')`); err != nil {
		t.Fatalf("seed parcel: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
		VALUES ('BAL-RR', 'prod-measured', 'sup-1', 'supplier', 'warehouse', 'warehouse:INB-9', 'return_requested', 3, TRUE)`); err != nil {
		t.Fatalf("seed reserved balance: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO inventory_reservations (id, product_id, supplier_id, location_key, quantity, reference_type, reference_id, status, idempotency_key)
		VALUES ('RES-1', 'prod-measured', 'sup-1', 'warehouse:INB-9', 3, 'supplier_return', 'RET-1', 'active', 'res-key-1')`); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}

	if err := repo.HandoverReturn(ctx, "RET-1", "warehouse-1", "handover-key-1"); err != nil {
		t.Fatalf("HandoverReturn: %v", err)
	}
	positions := openPositions(t, db, "prod-measured")
	if len(positions) != 1 || positions[0].qty != 2 || positions[0].closed {
		t.Fatalf("positions after handover = %+v, want one open position of 2", positions)
	}
	// Replay: the in_transit state guard makes handover a no-op.
	if err := repo.HandoverReturn(ctx, "RET-1", "warehouse-1", "handover-key-1"); err != nil {
		t.Fatalf("HandoverReturn replay: %v", err)
	}
	positions = openPositions(t, db, "prod-measured")
	if len(positions) != 1 || positions[0].qty != 2 {
		t.Fatalf("positions after replay = %+v, want unchanged qty 2", positions)
	}
}

func TestStoragePositionPutawayAgedPositionIsChargedByEngine(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	task, position := fin006KnownReceipt(t, db, f, warehouse, 2, tehranDay(time.Now()).AddDate(0, 0, -31))
	fin006PlaceTask(t, db, f, warehouse, task, 2)
	var qty, count int
	if err := db.QueryRow(`SELECT COUNT(*),SUM(qty) FROM warehouse_storage_positions WHERE product_id=$1`, f.product).Scan(&count, &qty); err != nil || count != 1 || qty != 2 {
		t.Fatalf("putaway double credit %d/%d %v", count, qty, err)
	}
	// TEST-02 round 5: a single run may not reach this lot in a
	// shared DB backlog (r5b: the 100-lot window was full of
	// unmeasured lots, 0 charged), and the run summary is GLOBAL.
	// Loop (bounded) until this position is charged, then assert on
	// its own charges (2 units x 1 day x 3480 = 6960).
	assessmentUntil(t, db, 10, func() bool { return storageChargeCountFor(t, db, position) >= 1 })
	if sum := storageChargeSumFor(t, db, position); sum != 6960 {
		t.Fatalf("known aged putaway position sum=%d want6960", sum)
	}
	var identity bool
	if err := db.QueryRow(`SELECT p.stocked_at=l.billing_start_at AND p.original_stocked_at=l.billing_start_at AND l.physical_received_at<l.billing_start_at FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots l ON l.id=p.id WHERE p.id=$1`, position).Scan(&identity); err != nil || !identity {
		t.Fatalf("receipt dates lost: %v", err)
	}
}
