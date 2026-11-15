package repository

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFIN006MigrationPreservesUnknownHistoryWithoutInventingDebt(t *testing.T) {
	up, err := os.ReadFile("../../migrations/202611150003_storage_receipt_history.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611150003_storage_receipt_history.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	db, f, warehouse := fin006Fixture(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// A fresh owned clone has no history evidence, so rehearse the actual old
	// schema then upgrade a populated, deliberately unknown merged position.
	if _, err = tx.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	id := "old-fee-" + uuid.NewString()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err = tx.Exec(`INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,goods_class,qty,stocked_at,original_stocked_at,total_fee_charged_toman) VALUES($1,$2,$3,$4,'normal',7,$5,$5,123)`, id, f.product, f.supplier, warehouse, old); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO warehouse_storage_fee_charges(id,position_id,supplier_id,product_id,warehouse_id,fee_date,qty,fee_unit_day_toman,amount_toman,goods_class) VALUES($1,$2,$3,$4,$5,'2026-02-01',1,123,123,'normal')`, "old-charge-"+uuid.NewString(), id, f.supplier, f.product, warehouse); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	var qty, charges, lots int
	var stocked time.Time
	var status string
	if err = tx.QueryRow(`SELECT qty,original_stocked_at FROM warehouse_storage_positions WHERE id=$1`, id).Scan(&qty, &stocked); err != nil || qty != 7 || !stocked.Equal(old) {
		t.Fatalf("old counters changed qty=%d date=%v err=%v", qty, stocked, err)
	}
	if err = tx.QueryRow(`SELECT COALESCE(SUM(amount_toman),0) FROM warehouse_storage_fee_charges WHERE position_id=$1`, id).Scan(&charges); err != nil || charges != 123 {
		t.Fatalf("old debt changed=%d err=%v", charges, err)
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_receipt_lots WHERE id=$1`, id).Scan(&lots); err != nil || lots != 0 {
		t.Fatalf("invented old receipt history=%d err=%v", lots, err)
	}
	if err = tx.QueryRow(`SELECT history_status FROM warehouse_storage_history_audit WHERE position_id=$1`, id).Scan(&status); err != nil || status != "legacy_unreconciled" {
		t.Fatalf("old history silently certified=%q err=%v", status, err)
	}
	if _, err = tx.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
