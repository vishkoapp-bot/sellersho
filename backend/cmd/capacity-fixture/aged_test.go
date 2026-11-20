package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	_ "time/tzdata"

	"github.com/drup/platform/internal/repository"
)

func seedAndVerifyAgedHistory(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `
INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES('cap-aged-warehouse','Synthetic capacity aged warehouse',1,1,'Tehran','Tehran');
INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,length_mm,width_mm,height_mm,weight_grams,measurement_status)
SELECT 'cap-aged-product-'||i,'Synthetic aged product '||i,'cap-aged-product-'||i,'cap-category','cap-supplier-'||i,120000,100000,10000,100,100,100,100,'verified' FROM generate_series(1,100)i;
INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled,warehouse_id)
SELECT 'cap-aged-balance-'||i,'cap-aged-product-'||i,'cap-supplier-'||i,'supplier','warehouse','warehouse:cap-aged-warehouse','sellable',20,true,'cap-aged-warehouse' FROM generate_series(1,100)i;
INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,to_status,to_location_key,reference_type,reference_id,idempotency_key,metadata,created_at)
SELECT 'cap-aged-receipt-'||i,'cap-aged-product-'||i,'cap-supplier-'||i,20,'sellable','warehouse:cap-aged-warehouse','capacity_aged_receipt','cap-aged-position-'||i,'cap-aged-receipt-'||i,'{"synthetic":true}'::jsonb,
 ((now() AT TIME ZONE 'Asia/Tehran')::date-(10+(i%6)*20)+time '12:00') AT TIME ZONE 'Asia/Tehran' FROM generate_series(1,100)i;
INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,goods_class,qty,stocked_at,original_stocked_at)
SELECT 'cap-aged-position-'||i,'cap-aged-product-'||i,'cap-supplier-'||i,'cap-aged-warehouse','normal',20,
 ((now() AT TIME ZONE 'Asia/Tehran')::date-(10+(i%6)*20)+time '12:00') AT TIME ZONE 'Asia/Tehran',
 ((now() AT TIME ZONE 'Asia/Tehran')::date-(10+(i%6)*20)+time '12:00') AT TIME ZONE 'Asia/Tehran' FROM generate_series(1,100)i;
`)
	if err != nil {
		t.Fatal(err)
	}
	suppliers := repository.NewSupplierRepository(db)
	settings, err := suppliers.GetStorageFeeSettings(ctx)
	if err != nil || settings.Enabled {
		t.Fatalf("default disabled fee settings: %+v %v", settings, err)
	}
	if _, err := suppliers.RunStorageFeeAssessment(ctx); !errors.Is(err, repository.ErrStorageFeeDisabled) {
		t.Fatalf("default-off assessment: %v", err)
	}
	// This variant changes only this disposable database, then restores default OFF.
	if _, err := db.ExecContext(ctx, `UPDATE platform_settings SET storage_fee_enabled=true WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `UPDATE platform_settings SET storage_fee_enabled=false WHERE id=1`)
	// CAP-01 (2026-11-20): expectation corrected for the immutable
	// receipt-history contract (4524ee3, FIN-006). The assessment now
	// charges ONLY certified receipt lots in
	// warehouse_storage_receipt_lots; a lot can only be created by a
	// real post-cutover QC receipt or customer return (migration
	// 202611150003 guard_storage_receipt_lot_insert), and "a missing
	// receipt lot is explicit unknown history, never invented old
	// debt". This synthetic aged cohort has positions/balances/
	// movements but no certified lots, so the correct outcome is
	// zero examined receipts and zero charges — NOT the pre-4524ee3
	// expectation of 3320 charged days at 4160 Toman/day, which the
	// old position-scanning assessment produced. Physical
	// conservation (2000 units) is still asserted below. The
	// fee-charging path itself is covered by
	// storage_history_integration_test.go; the aged cohort's role
	// here is the data floor + conservation, not fee computation.
	run, err := suppliers.RunStorageFeeAssessment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run.ReceiptsExamined != 0 || run.ChargesCreated != 0 || run.TotalCharged != 0 || run.MoreWork {
		t.Fatalf("aged cohort without certified receipt lots must be unscored: %+v", run)
	}
	replay, err := suppliers.RunStorageFeeAssessment(ctx)
	if err != nil || replay.ChargesCreated != 0 || replay.TotalCharged != 0 {
		t.Fatalf("fee replay: %+v %v", replay, err)
	}
	var receipts, physical, positions, charged, checkpoint int64
	if err := db.QueryRowContext(ctx, `SELECT
 (SELECT COALESCE(sum(quantity),0) FROM inventory_movements WHERE reference_type='capacity_aged_receipt'),
 (SELECT COALESCE(sum(quantity),0) FROM inventory_balances WHERE product_id LIKE 'cap-aged-product-%'),
 (SELECT COALESCE(sum(qty),0) FROM warehouse_storage_positions WHERE id LIKE 'cap-aged-position-%'),
 (SELECT COALESCE(sum(amount_toman),0) FROM warehouse_storage_fee_charges WHERE position_id LIKE 'cap-aged-position-%'),
 (SELECT COALESCE(sum(total_fee_charged_toman),0) FROM warehouse_storage_positions WHERE id LIKE 'cap-aged-position-%')`).Scan(&receipts, &physical, &positions, &charged, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if receipts != 2000 || physical != receipts || positions != physical || charged != run.TotalCharged || checkpoint != charged {
		t.Fatalf("aged physical/position/fee conservation: %d %d %d %d %d", receipts, physical, positions, charged, checkpoint)
	}
	if _, err := db.ExecContext(ctx, `UPDATE platform_settings SET storage_fee_enabled=false WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	t.Logf("aged cohort 100 positions/2000 units ages 10..110 days; no certified receipt lots, so assessment correctly scores 0 (unknown history, never invented debt); physical conservation holds; flag restored OFF")
}
