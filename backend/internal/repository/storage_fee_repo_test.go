package repository

// Storage-fee engine tests (gap 7). DB-gated like the sibling suites:
// they run ONLY when TEAM04_TEST_DATABASE_URL points at a DISPOSABLE
// Postgres; otherwise they skip. The DDL below is a minimal subset of
// the real migrations (202610280005 positions/charges, 202611050005
// settings, plus the balance inputs), sufficient for the engine and
// the balance read under test.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func storageFeeSeed(t *testing.T, db *sql.DB) {
	t.Helper()
	ddl := []string{
		`DROP TABLE IF EXISTS order_item_origins,warehouse_storage_fee_charges, warehouse_storage_positions, customer_return_shipping_charges, supplier_return_financial_events, customer_return_financial_events, ledger_entries, payout_requests, order_items, orders, products, warehouses, suppliers, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT  DEFAULT 1 PRIMARY KEY, storage_fee_enabled BOOLEAN NOT NULL DEFAULT false, storage_fee_free_days INT NOT NULL DEFAULT 30, storage_fee_per_kg_day BIGINT NOT NULL DEFAULT 2000, storage_fee_per_m3_day BIGINT NOT NULL DEFAULT 8000,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			early_withdrawal_fee_pct NUMERIC,
			early_withdrawal_minimum_toman BIGINT NOT NULL DEFAULT 1000000 CONSTRAINT platform_settings_early_withdrawal_minimum_toman_check CHECK (early_withdrawal_minimum_toman BETWEEN 1 AND 9007199254740991),
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
		// prod-measured: 1500 g (1.5 kg), 500x400x300 mm (0.06 m3) ->
		// unit day at defaults = 1.5*2000 + 0.06*8000 = 3480 Toman.
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, supplier_id VARCHAR(50), length_mm INT, width_mm INT, height_mm INT, weight_grams INT,
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
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			supplier_warranty character varying(100),
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			tax_class text NOT NULL DEFAULT 'standard'::text,
			title character varying(255),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60))`,
		`INSERT INTO products (id, supplier_id, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-measured', 'sup-1', 500, 400, 300, 1500)`,
		`INSERT INTO products (id, supplier_id, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-unmeasured', 'sup-1', NULL, NULL, NULL, NULL)`,
		`CREATE TABLE warehouse_storage_positions (id TEXT PRIMARY KEY, product_id VARCHAR(50) NOT NULL, supplier_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, inbound_id TEXT, goods_class VARCHAR(20) NOT NULL DEFAULT 'normal', qty INT NOT NULL, stocked_at TIMESTAMPTZ NOT NULL, original_stocked_at TIMESTAMPTZ NOT NULL, last_fee_date DATE, last_assessed_date DATE, total_fee_charged_toman BIGINT NOT NULL DEFAULT 0, closed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE warehouse_storage_fee_charges (id TEXT PRIMARY KEY, position_id TEXT NOT NULL, supplier_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, fee_date DATE NOT NULL, qty INT NOT NULL, fee_unit_day_toman BIGINT NOT NULL, amount_toman BIGINT NOT NULL CHECK (amount_toman > 0), goods_class VARCHAR(20) NOT NULL, estimated BOOLEAN NOT NULL DEFAULT FALSE, journal_debit_id TEXT, journal_credit_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (position_id, fee_date))`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, payment_status VARCHAR(20) DEFAULT 'pending'::character varying , status VARCHAR(30) DEFAULT 'processing'::character varying ,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			order_number VARCHAR(50) NOT NULL DEFAULT '',
			payable_amount BIGINT NOT NULL DEFAULT 0,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			customer_user_id UUID,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			customer_address text,
			customer_city character varying(50),
			customer_full_name character varying(100),
			customer_phone character varying(20),
			customer_postal_code character varying(20),
			customer_province character varying(50),
			discount_amount bigint NOT NULL DEFAULT 0,
			discount_code text,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
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
			shipped_at timestamp with time zone,
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			store_id character varying(50),
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_code character varying(50),
			tracking_number character varying(100),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE order_items (id VARCHAR(50)  DEFAULT gen_random_uuid() PRIMARY KEY, order_id VARCHAR(50), product_id VARCHAR(50), wholesale_cost BIGINT, quantity INT DEFAULT 1 ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			discount_amount bigint NOT NULL DEFAULT 0,
			final_unit_price bigint,
			platform_fee bigint,
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text,
			seller_margin bigint,
			variant_id uuid,
			variant_name text,
			variant_spnc character varying(12))`,
		`CREATE TABLE order_item_origins(id TEXT PRIMARY KEY,order_id TEXT,order_item_id TEXT,supplier_id TEXT,quantity INT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			leg_index integer,
			origin_label text,
			origin_type text,
			supplier_warehouse_id text,
			warehouse_id text)`,
		`CREATE TABLE payout_requests (id VARCHAR(50) PRIMARY KEY, supplier_id VARCHAR(50), amount BIGINT, status VARCHAR(20) DEFAULT 'pending'::character varying ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			available_balance bigint NOT NULL DEFAULT 0,
			bank_name character varying(50),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			payout_method character varying(30) NOT NULL DEFAULT 'manual_bank_transfer'::character varying,
			processed_at timestamp with time zone,
			recipient_name character varying(100),
			recipient_type character varying(20),
			rejection_reason text,
			shaba character varying(30),
			store_id character varying(50),
			transfer_confirmed_at timestamp with time zone,
			transfer_confirmed_by character varying(100),
			transfer_date timestamp with time zone,
			transfer_reference character varying(100))`,
		`CREATE TABLE supplier_return_financial_events (supplier_id VARCHAR(50), operation VARCHAR(10), amount BIGINT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			actor_user_id character varying(80),
			charge_component text,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			event_identity text,
			id character varying(80),
			idempotency_key character varying(150),
			ledger_entry_id character varying(80),
			parcel_id character varying(80),
			payment_method character varying(20),
			reason text,
			return_request_id character varying(60))`,
		`CREATE TABLE customer_return_shipping_charges (supplier_id VARCHAR(50), bearer VARCHAR(20), amount BIGINT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			actor_user_id character varying(80),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id text,
			idempotency_key character varying(150),
			ledger_entry_id character varying(80),
			reason text,
			return_id uuid,
			return_item_id uuid)`,
		`CREATE TABLE customer_return_financial_events (supplier_id VARCHAR(50), order_id VARCHAR(50), component VARCHAR(30), operation VARCHAR(10), amount BIGINT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			actor_user_id character varying(80),
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			id character varying(80),
			idempotency_key character varying(150),
			ledger_entry_id character varying(80),
			reason text,
			return_id uuid,
			return_item_id uuid,
			store_id text)`,
		`CREATE TABLE ledger_entries (target_account TEXT, target_type TEXT, reason TEXT, type VARCHAR(10), amount BIGINT,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			date timestamp with time zone NOT NULL DEFAULT now(),
			id character varying(50),
			operator character varying(100),
			related_order_or_dispute character varying(50))`,
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
			t.Fatalf("storage-fee seed DDL failed: %v", err)
		}
	}
	// TEST-02 round 4: the CASCADE drop above also dropped the inventory_effective_measurements view; re-create it.
	createEffectiveMeasurementsView(t, db)
}

// storageStockedAt returns a stocking instant (Tehran noon) whose
// Tehran calendar date is daysAgo days before today.
func storageStockedAt(daysAgo int) time.Time {
	d := tehranDay(time.Now()).AddDate(0, 0, -daysAgo)
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, storageFeeTehranLoc)
}

func storageInsertPosition(t *testing.T, db *sql.DB, id, productID string, qty, daysAgo int) {
	t.Helper()
	ts := storageStockedAt(daysAgo)
	if _, err := db.Exec(`INSERT INTO warehouse_storage_positions (id, product_id, supplier_id, warehouse_id, qty, stocked_at, original_stocked_at)
		VALUES ($1, $2, 'sup-1', 'wh-1', $3, $4, $4)`, id, productID, qty, ts); err != nil {
		t.Fatalf("insert position: %v", err)
	}
}

func storageEnable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`UPDATE platform_settings SET storage_fee_enabled = true WHERE id = 1`); err != nil {
		t.Fatalf("enable storage fees: %v", err)
	}
}

func storageChargeCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_fee_charges`).Scan(&n); err != nil {
		t.Fatalf("count charges: %v", err)
	}
	return n
}

func TestStorageFeeSettingsDefaults(t *testing.T) {
	db := operationsDB(t)
	// TEST-02 round 5: platform_settings is a GLOBAL row — other
	// tests mutate it (fin006Fixture enables fees, TariffOverride
	// changes the rate, RefusesWhileDisabled turns it off), so
	// asserting it equals the Go default depends on which tests ran
	// first (passed in the r5c full run, failed in a subset run).
	// Reset the row to the defaults first; the test then proves the
	// READ path returns those defaults, independent of order.
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_enabled=FALSE,storage_fee_free_days=30,storage_fee_per_kg_day=2000,storage_fee_per_m3_day=8000 WHERE id=1`)
	settings, err := NewSupplierRepository(db).GetStorageFeeSettings(context.Background())
	if err != nil || settings != DefaultStorageFeeSettings() {
		t.Fatalf("defaults %+v %v", settings, err)
	}
}
func TestStorageFeeRunRefusesWhileDisabled(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	_, position := fin006KnownReceipt(t, db, f, wh, 1, tehranDay(time.Now()).AddDate(0, 0, -60))
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_enabled=FALSE WHERE id=1`)
	if _, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background()); !errors.Is(err, ErrStorageFeeDisabled) {
		t.Fatalf("run while OFF: %v", err)
	}
	// TEST-02 round 5: the charge count is GLOBAL — earlier tests'
	// charges exist in the shared DB (got 5 in the full run). Scope
	// to this test's own (never assessed, fee OFF) position.
	if n := storageChargeCountFor(t, db, position); n != 0 {
		t.Fatalf("charges while OFF=%d", n)
	}
}
func storageChargeCountFor(t *testing.T, db *sql.DB, positionID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_fee_charges WHERE position_id=$1`, positionID).Scan(&n); err != nil {
		t.Fatalf("count charges for position: %v", err)
	}
	return n
}

func storageChargeSumFor(t *testing.T, db *sql.DB, positionID string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount_toman),0) FROM warehouse_storage_fee_charges WHERE position_id=$1`, positionID).Scan(&n); err != nil {
		t.Fatalf("sum charges for position: %v", err)
	}
	return n
}

// assessmentUntil runs the storage-fee assessment repeatedly (bounded
// passes — the production contract: each pass is bounded to 100 lots
// and MoreWork requests another idempotent pass) until done() is true
// or maxRuns passes have run, and returns the last summary. Use it
// when a test needs its own lot charged in a shared DB whose backlog
// (earlier tests' lots, plus an advancing cursor) may put the lot
// beyond one pass's 100-lot window, so a single run cannot guarantee
// reaching it. The caller scopes done() to its own rows (e.g. its
// position's charges) — the global summary is not a per-test signal.
func assessmentUntil(t *testing.T, db *sql.DB, maxRuns int, done func() bool) StorageFeeRunSummary {
	t.Helper()
	repo := NewSupplierRepository(db)
	var last StorageFeeRunSummary
	for i := 0; i < maxRuns; i++ {
		r, err := repo.RunStorageFeeAssessment(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		last = r
		if done() {
			return last
		}
	}
	return last
}

func TestStorageFeeFreeWindowChargesNothing(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	_, position := fin006KnownReceipt(t, db, f, wh, 3, tehranDay(time.Now()).AddDate(0, 0, -10))
	r, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background())
	// TEST-02 round 4: the assessment is global — it bills every
	// chargeable receipt in the shared DB, including earlier tests'
	// receipts (e.g. the 60-day-old receipt the disabled-run test left
	// behind). The test's intent is that THIS receipt, inside the
	// 30-day free window, is not charged — so assert on this
	// position's charges, not the global totals.
	if err != nil || storageChargeCountFor(t, db, position) != 0 {
		t.Fatalf("free window %+v %v", r, err)
	}
}
func TestStorageFeeDay31ChargesExactlyOneDayAndDropsBalance(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	_, position := fin006KnownReceipt(t, db, f, wh, 2, tehranDay(time.Now()).AddDate(0, 0, -31))
	order, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `UPDATE order_items SET quantity=2,wholesale_cost=500000,final_unit_price=500000 WHERE order_id=$1`, order)
	opsExec(t, db, `UPDATE orders SET status='delivered' WHERE id=$1`, order)
	seedCanonicalWarehouseOrderOrigins(t, db, order, wh)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	before, err := repo.GetSettlementBalance(ctx, f.supplier)
	if err != nil || before.Available != 1000000 || before.StorageFees != 0 {
		t.Fatalf("balance before %+v %v", before, err)
	}
	const want = 6960
	// TEST-02 round 5: a single assessment run may not reach this
	// lot in a shared DB whose backlog puts it beyond one pass's
	// 100-lot window, and the run summary is GLOBAL (earlier tests'
	// lots are charged too). Loop (bounded) until this position has
	// its one charge, then assert on this position's evidence and on
	// this supplier's balance — not the global summary.
	assessmentUntil(t, db, 10, func() bool { return storageChargeCountFor(t, db, position) == 1 })
	var unit, amount int64
	var date time.Time
	if err = db.QueryRow(`SELECT fee_unit_day_toman,amount_toman,fee_date FROM warehouse_storage_fee_charges WHERE position_id=$1`, position).Scan(&unit, &amount, &date); err != nil || unit != 3480 || amount != want || !date.Equal(tehranDay(time.Now()).AddDate(0, 0, -1)) {
		t.Fatalf("daily evidence %d/%d/%v %v", unit, amount, date, err)
	}
	after, err := repo.GetSettlementBalance(ctx, f.supplier)
	if err != nil || after.StorageFees != want || after.Available != 1000000-want {
		t.Fatalf("balance after %+v %v", after, err)
	}
	again, err := repo.RunStorageFeeAssessment(ctx)
	// TEST-02 round 5: the global charge count accumulates earlier
	// tests' charges in the shared DB — scope the replay assertion to
	// this test's own position (exactly its 1 charge).
	if err != nil || storageChargeCountFor(t, db, position) != 1 {
		t.Fatalf("replay %+v %v", again, err)
	}
}
func TestStorageFeeUnmeasuredPositionSkipped(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	opsExec(t, db, `UPDATE products SET measurement_revision_id=NULL,length_mm=NULL,width_mm=NULL,height_mm=NULL,weight_grams=NULL WHERE id=$1`, f.product)
	_, position := fin006KnownReceipt(t, db, f, wh, 5, tehranDay(time.Now()).AddDate(0, 0, -60))
	repo := NewSupplierRepository(db)
	for i := 0; i < 2; i++ {
		r, err := repo.RunStorageFeeAssessment(context.Background())
		// TEST-02 round 5: the summary counts are GLOBAL — earlier
		// tests' unresolved lots inflate PositionsSkippedNoMeasurement
		// (got 6 in the full run) and earlier tests' chargeable lots
		// inflate ChargesCreated. The intent is that THIS unmeasured
		// position is skipped (never charged): assert at least one
		// lot is reported skipped and this position has no charges.
		if err != nil || r.PositionsSkippedNoMeasurement < 1 || storageChargeCountFor(t, db, position) != 0 {
			t.Fatalf("unmeasured %+v %v", r, err)
		}
		if i == 0 {
			if err = repo.RecordProductMeasurement(context.Background(), f.product, f.user, 500, 400, 300, 1500, "later measurement cannot certify past custody"); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func TestStorageFeeTariffOverrideRespected(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_per_kg_day=5000 WHERE id=1`)
	_, position := fin006KnownReceipt(t, db, f, wh, 2, tehranDay(time.Now()).AddDate(0, 0, -31))
	// TEST-02 round 5: a single run may not reach this lot in a
	// shared DB backlog, and the run summary is GLOBAL. Loop
	// (bounded) until this position is charged, then assert on its
	// own charges (the tariff override: 2 units x 31 days at the
	// overridden rate = 15960), not the global total.
	assessmentUntil(t, db, 10, func() bool { return storageChargeCountFor(t, db, position) >= 1 })
	if sum := storageChargeSumFor(t, db, position); sum != 15960 {
		t.Fatalf("receipt tariff position sum=%d want15960", sum)
	}
}
