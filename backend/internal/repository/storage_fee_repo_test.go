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
		`CREATE TABLE order_item_origins(id TEXT PRIMARY KEY,order_id TEXT,order_item_id TEXT,supplier_id TEXT,quantity INT)`,
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
	db := operationsDB(t)
	settings, err := NewSupplierRepository(db).GetStorageFeeSettings(context.Background())
	if err != nil || settings != DefaultStorageFeeSettings() {
		t.Fatalf("defaults %+v %v", settings, err)
	}
}
func TestStorageFeeRunRefusesWhileDisabled(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	fin006KnownReceipt(t, db, f, wh, 1, tehranDay(time.Now()).AddDate(0, 0, -60))
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_enabled=FALSE WHERE id=1`)
	if _, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background()); !errors.Is(err, ErrStorageFeeDisabled) {
		t.Fatalf("run while OFF: %v", err)
	}
	if n := storageChargeCount(t, db); n != 0 {
		t.Fatalf("charges while OFF=%d", n)
	}
}
func TestStorageFeeFreeWindowChargesNothing(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	fin006KnownReceipt(t, db, f, wh, 3, tehranDay(time.Now()).AddDate(0, 0, -10))
	r, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background())
	if err != nil || r.ChargesCreated != 0 || r.TotalCharged != 0 || r.PositionsCharged != 0 || storageChargeCount(t, db) != 0 {
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
	summary, err := repo.RunStorageFeeAssessment(ctx)
	const want = 6960
	if err != nil || summary.TotalCharged != want || summary.ChargesCreated != 1 || summary.PositionsCharged != 1 {
		t.Fatalf("day31 %+v %v", summary, err)
	}
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
	if err != nil || again.TotalCharged != 0 || again.ChargesCreated != 0 || storageChargeCount(t, db) != 1 {
		t.Fatalf("replay %+v %v", again, err)
	}
}
func TestStorageFeeUnmeasuredPositionSkipped(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	opsExec(t, db, `UPDATE products SET measurement_revision_id=NULL,length_mm=NULL,width_mm=NULL,height_mm=NULL,weight_grams=NULL WHERE id=$1`, f.product)
	fin006KnownReceipt(t, db, f, wh, 5, tehranDay(time.Now()).AddDate(0, 0, -60))
	repo := NewSupplierRepository(db)
	for i := 0; i < 2; i++ {
		r, err := repo.RunStorageFeeAssessment(context.Background())
		if err != nil || r.PositionsSkippedNoMeasurement != 1 || r.ChargesCreated != 0 || storageChargeCount(t, db) != 0 {
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
	fin006KnownReceipt(t, db, f, wh, 2, tehranDay(time.Now()).AddDate(0, 0, -31))
	summary, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background())
	if err != nil || summary.TotalCharged != 15960 {
		t.Fatalf("receipt tariff %+v %v", summary, err)
	}
}
