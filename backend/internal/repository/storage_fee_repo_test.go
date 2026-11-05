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
		`DROP TABLE IF EXISTS warehouse_storage_fee_charges, warehouse_storage_positions, customer_return_shipping_charges, supplier_return_financial_events, customer_return_financial_events, ledger_entries, payout_requests, order_items, orders, products, warehouses, suppliers, platform_settings CASCADE`,
		`CREATE TABLE platform_settings (id INT PRIMARY KEY, storage_fee_enabled BOOLEAN NOT NULL DEFAULT false, storage_fee_free_days INT NOT NULL DEFAULT 30, storage_fee_per_kg_day BIGINT NOT NULL DEFAULT 2000, storage_fee_per_m3_day BIGINT NOT NULL DEFAULT 8000)`,
		`INSERT INTO platform_settings (id) VALUES (1)`,
		`CREATE TABLE suppliers (id VARCHAR(50) PRIMARY KEY)`,
		`INSERT INTO suppliers (id) VALUES ('sup-1')`,
		`CREATE TABLE warehouses (id TEXT PRIMARY KEY)`,
		`INSERT INTO warehouses (id) VALUES ('wh-1')`,
		// prod-measured: 1500 g (1.5 kg), 500x400x300 mm (0.06 m3) ->
		// unit day at defaults = 1.5*2000 + 0.06*8000 = 3480 Toman.
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, supplier_id VARCHAR(50), length_mm INT, width_mm INT, height_mm INT, weight_grams INT)`,
		`INSERT INTO products (id, supplier_id, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-measured', 'sup-1', 500, 400, 300, 1500)`,
		`INSERT INTO products (id, supplier_id, length_mm, width_mm, height_mm, weight_grams) VALUES ('prod-unmeasured', 'sup-1', NULL, NULL, NULL, NULL)`,
		`CREATE TABLE warehouse_storage_positions (id TEXT PRIMARY KEY, product_id VARCHAR(50) NOT NULL, supplier_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, inbound_id TEXT, goods_class VARCHAR(20) NOT NULL DEFAULT 'normal', qty INT NOT NULL, stocked_at TIMESTAMPTZ NOT NULL, original_stocked_at TIMESTAMPTZ NOT NULL, last_fee_date DATE, last_assessed_date DATE, total_fee_charged_toman BIGINT NOT NULL DEFAULT 0, closed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE warehouse_storage_fee_charges (id TEXT PRIMARY KEY, position_id TEXT NOT NULL, supplier_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, warehouse_id TEXT NOT NULL, fee_date DATE NOT NULL, qty INT NOT NULL, fee_unit_day_toman BIGINT NOT NULL, amount_toman BIGINT NOT NULL CHECK (amount_toman > 0), goods_class VARCHAR(20) NOT NULL, estimated BOOLEAN NOT NULL DEFAULT FALSE, journal_debit_id TEXT, journal_credit_id TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE (position_id, fee_date))`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, payment_status VARCHAR(20), status VARCHAR(30))`,
		`CREATE TABLE order_items (id VARCHAR(50) PRIMARY KEY, order_id VARCHAR(50), product_id VARCHAR(50), wholesale_cost BIGINT, quantity INT)`,
		`CREATE TABLE payout_requests (id VARCHAR(50) PRIMARY KEY, supplier_id VARCHAR(50), amount BIGINT, status VARCHAR(20))`,
		`CREATE TABLE supplier_return_financial_events (supplier_id VARCHAR(50), operation VARCHAR(10), amount BIGINT)`,
		`CREATE TABLE customer_return_shipping_charges (supplier_id VARCHAR(50), bearer VARCHAR(20), amount BIGINT)`,
		`CREATE TABLE customer_return_financial_events (supplier_id VARCHAR(50), order_id VARCHAR(50), component VARCHAR(30), operation VARCHAR(10), amount BIGINT)`,
		`CREATE TABLE ledger_entries (target_account TEXT, target_type TEXT, reason TEXT, type VARCHAR(10), amount BIGINT)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("storage-fee seed DDL failed: %v", err)
		}
	}
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
	db := team04TestDB(t)
	storageFeeSeed(t, db)
	repo := NewSupplierRepository(db)
	settings, err := repo.GetStorageFeeSettings(context.Background())
	if err != nil {
		t.Fatalf("GetStorageFeeSettings: %v", err)
	}
	want := DefaultStorageFeeSettings()
	if settings != want {
		t.Fatalf("defaults = %+v, want %+v (flag OFF, 30 free days, 2000/kg, 8000/m3)", settings, want)
	}
	if settings.Enabled {
		t.Fatal("storage fees must ship disabled")
	}
}

func TestStorageFeeRunRefusesWhileDisabled(t *testing.T) {
	db := team04TestDB(t)
	storageFeeSeed(t, db)
	storageInsertPosition(t, db, "WSP-old", "prod-measured", 1, 60)
	repo := NewSupplierRepository(db)
	if _, err := repo.RunStorageFeeAssessment(context.Background()); !errors.Is(err, ErrStorageFeeDisabled) {
		t.Fatalf("run while OFF: err = %v, want ErrStorageFeeDisabled", err)
	}
	if n := storageChargeCount(t, db); n != 0 {
		t.Fatalf("charges while OFF = %d, want 0", n)
	}
}

func TestStorageFeeFreeWindowChargesNothing(t *testing.T) {
	db := team04TestDB(t)
	storageFeeSeed(t, db)
	storageEnable(t, db)
	storageInsertPosition(t, db, "WSP-young", "prod-measured", 3, 10)
	repo := NewSupplierRepository(db)
	summary, err := repo.RunStorageFeeAssessment(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.ChargesCreated != 0 || summary.TotalCharged != 0 || summary.PositionsCharged != 0 {
		t.Fatalf("free-window summary = %+v, want all zero charges", summary)
	}
	if n := storageChargeCount(t, db); n != 0 {
		t.Fatalf("charges = %d, want 0", n)
	}
}

func TestStorageFeeDay31ChargesExactlyOneDayAndDropsBalance(t *testing.T) {
	db := team04TestDB(t)
	storageFeeSeed(t, db)
	storageEnable(t, db)
	// Earned baseline: one paid+delivered order, 500000 x 2 = 1,000,000.
	if _, err := db.Exec(`INSERT INTO orders (id, payment_status, status) VALUES ('ord-1', 'paid', 'delivered')`); err != nil {
		t.Fatalf("seed order: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO order_items (id, order_id, product_id, wholesale_cost, quantity) VALUES ('oi-1', 'ord-1', 'prod-measured', 500000, 2)`); err != nil {
		t.Fatalf("seed order item: %v", err)
	}
	// Position stocked 31 days ago, qty 2: day 31 is the first
	// chargeable elapsed day (30 free); exactly 1 day x 3480 x 2.
	storageInsertPosition(t, db, "WSP-31", "prod-measured", 2, 31)

	repo := NewSupplierRepository(db)
	ctx := context.Background()
	before, err := repo.GetSettlementBalance(ctx, "sup-1")
	if err != nil {
		t.Fatalf("balance before: %v", err)
	}
	if before.Available != 1000000 || before.StorageFees != 0 {
		t.Fatalf("balance before = %+v, want available 1000000 storageFees 0", before)
	}

	summary, err := repo.RunStorageFeeAssessment(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	const wantDay = 3480 * 2
	if summary.PositionsCharged != 1 || summary.ChargesCreated != 1 || summary.TotalCharged != wantDay {
		t.Fatalf("summary = %+v, want 1 position / 1 charge / %d Toman", summary, wantDay)
	}
	var unitDay, amount int64
	var feeDate time.Time
	if err := db.QueryRow(`SELECT fee_unit_day_toman, amount_toman, fee_date FROM warehouse_storage_fee_charges WHERE position_id = 'WSP-31'`).Scan(&unitDay, &amount, &feeDate); err != nil {
		t.Fatalf("read charge: %v", err)
	}
	if unitDay != 3480 || amount != wantDay {
		t.Fatalf("charge unitDay=%d amount=%d, want 3480/%d", unitDay, amount, wantDay)
	}
	if !feeDate.Equal(tehranDay(time.Now()).AddDate(0, 0, -1)) {
		t.Fatalf("fee_date = %v, want yesterday (Tehran)", feeDate)
	}

	after, err := repo.GetSettlementBalance(ctx, "sup-1")
	if err != nil {
		t.Fatalf("balance after: %v", err)
	}
	if after.StorageFees != wantDay || after.Available != 1000000-wantDay {
		t.Fatalf("balance after = %+v, want storageFees %d and available %d", after, wantDay, 1000000-wantDay)
	}

	// Idempotency: a same-day re-run creates nothing new.
	again, err := repo.RunStorageFeeAssessment(ctx)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if again.ChargesCreated != 0 || again.TotalCharged != 0 {
		t.Fatalf("re-run summary = %+v, want no new charges", again)
	}
	if n := storageChargeCount(t, db); n != 1 {
		t.Fatalf("charges after re-run = %d, want 1", n)
	}
}

func TestStorageFeeUnmeasuredPositionSkipped(t *testing.T) {
	db := team04TestDB(t)
	storageFeeSeed(t, db)
	storageEnable(t, db)
	storageInsertPosition(t, db, "WSP-nomeasure", "prod-unmeasured", 5, 60)
	repo := NewSupplierRepository(db)
	summary, err := repo.RunStorageFeeAssessment(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.PositionsSkippedNoMeasurement != 1 {
		t.Fatalf("skipped = %d, want 1", summary.PositionsSkippedNoMeasurement)
	}
	if summary.ChargesCreated != 0 || storageChargeCount(t, db) != 0 {
		t.Fatalf("unmeasured position was charged: summary %+v", summary)
	}
}

func TestStorageFeeTariffOverrideRespected(t *testing.T) {
	db := team04TestDB(t)
	storageFeeSeed(t, db)
	storageEnable(t, db)
	if _, err := db.Exec(`UPDATE platform_settings SET storage_fee_per_kg_day = 5000 WHERE id = 1`); err != nil {
		t.Fatalf("override tariff: %v", err)
	}
	storageInsertPosition(t, db, "WSP-override", "prod-measured", 2, 31)
	repo := NewSupplierRepository(db)
	summary, err := repo.RunStorageFeeAssessment(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// unit day = 1.5*5000 + 0.06*8000 = 7500 + 480 = 7980; x qty 2 = 15960.
	if summary.TotalCharged != 15960 {
		t.Fatalf("totalCharged = %d, want 15960 with overridden kg tariff", summary.TotalCharged)
	}
}
