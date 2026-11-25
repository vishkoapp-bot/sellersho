package repository

import (
	"context"
	"testing"
	"time"
)

func TestSupplierStorageAgingUsesCertifiedReceiptCalendar(t *testing.T) {
	ctx := context.Background()
	settingsDB := operationsDB(t)
	before, err := NewSupplierRepository(settingsDB).GetStorageFeeSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := settingsDB.Exec(`UPDATE platform_settings SET storage_fee_enabled=$1,storage_fee_free_days=$2,
			storage_fee_per_kg_day=$3,storage_fee_per_m3_day=$4 WHERE id=1`,
			before.Enabled, before.FreeDays, before.PerKgDay, before.PerM3Day); err != nil {
			t.Errorf("restore storage fee fixture settings: %v", err)
		}
	})
	db, f, warehouse := fin006Fixture(t)
	now := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	_, near := fin006KnownReceipt(t, db, f, warehouse, 4, now.AddDate(0, 0, -27))
	_, due := fin006KnownReceipt(t, db, f, warehouse, 2, now.AddDate(0, 0, -32))
	_, far := fin006KnownReceipt(t, db, f, warehouse, 1, now.AddDate(0, 0, -5))
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_free_days=45 WHERE id=1`)
	repo := NewSupplierRepository(db)
	report, err := repo.getSupplierStorageAging(ctx, f.supplier, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if !report.FeesEnabled || report.AsOf != "2026-10-01" || report.WarningCount != 2 || len(report.Rows) != 3 {
		t.Fatalf("aging report: %+v", report)
	}
	if report.Rows[0].ID != due || report.Rows[0].FeeStartsOn != "2026-09-29" || report.Rows[0].Quantity != 2 ||
		report.Rows[1].ID != near || report.Rows[1].FeeStartsOn != "2026-10-04" || report.Rows[1].AgeDays != 27 ||
		report.Rows[2].ID != far {
		t.Fatalf("aging rows: %+v", report.Rows)
	}
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_enabled=FALSE WHERE id=1`)
	disabled, err := repo.getSupplierStorageAging(ctx, f.supplier, 0, now)
	if err != nil || disabled.FeesEnabled || disabled.WarningCount != 0 || len(disabled.Rows) == 0 || disabled.Rows[0].FeeStartsOn != "" {
		t.Fatalf("disabled assessment report: %+v (%v)", disabled, err)
	}
	crossSupplier, err := repo.getSupplierStorageAging(ctx, "other", 0, now)
	if err != nil || len(crossSupplier.Rows) != 0 {
		t.Fatalf("another supplier's report: %+v (%v)", crossSupplier, err)
	}
}
