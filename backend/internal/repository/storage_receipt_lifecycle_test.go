package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestINV10QCCreatesExactReceiptLotBeforePutawayAndNeverCreditsTwice(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy_asn", true: "tracked_carton"}[tracked], func(t *testing.T) {
			var db *sql.DB
			var f opsFixture
			var warehouse, inbound, sku string
			if tracked {
				db, f, warehouse, inbound, sku, _ = inv09ReceiveTracked(t, 2)
			} else {
				db = operationsDB(t)
				f = opsFixtureNew(t, db)
				warehouse = "history-wh-" + uuid.NewString()
				inbound = inv09ReceiveLegacy(t, db, f, warehouse, 2)
			}
			ctx := context.Background()
			wh := NewWarehouseRepository(db)
			if err := wh.RecordQCPass(ctx, f.product, inbound, f.user, 2); err != nil {
				t.Fatal(err)
			}
			var count, qty int
			var variant, origin, sourceEvent, revision string
			var physical, billing time.Time
			var unit int64
			if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(p.qty),0) FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots l ON l.id=p.id WHERE p.product_id=$1`, f.product).Scan(&count, &qty); err != nil || count != 1 || qty != 2 {
				t.Fatalf("QC receipt lots=%d qty=%d want1/2 err=%v", count, qty, err)
			}
			if err := db.QueryRow(`SELECT variant_id,warehouse_id,source_event_id,measurement_revision_id,physical_received_at,billing_start_at,unit_day_toman FROM warehouse_storage_receipt_lots WHERE product_id=$1`, f.product).Scan(&variant, &origin, &sourceEvent, &revision, &physical, &billing, &unit); err != nil || variant != sku || origin != warehouse || revision == "" || unit != 208 || physical.After(billing) {
				t.Fatalf("exact receipt SKU=%s wh=%s event=%s revision=%s physical=%v billing=%v unit=%d err=%v", variant, origin, sourceEvent, revision, physical, billing, unit, err)
			}
			var sourceMatches bool
			if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots l JOIN inbound_qc_dispositions d ON d.id=l.source_event_id JOIN inventory_movements m ON m.id=l.receipt_movement_id WHERE l.product_id=$1 AND l.billing_start_at=d.created_at AND l.physical_received_at=m.created_at AND m.reference_id=l.receipt_line_id AND m.to_status='quarantine' AND l.source_kind='inbound_qc')`, f.product).Scan(&sourceMatches); err != nil || !sourceMatches {
				t.Fatalf("receipt/QC dates invented: %v", err)
			}
			if err := wh.RecordQCPass(ctx, f.product, inbound, f.user, 2); err != nil {
				t.Fatal(err)
			}
			inv09PlacePending(t, db, f, inbound, warehouse)
			if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(p.qty),0) FROM warehouse_storage_positions p WHERE p.product_id=$1`, f.product).Scan(&count, &qty); err != nil || count != 1 || qty != 2 {
				t.Fatalf("putaway credited another position count=%d qty=%d err=%v", count, qty, err)
			}
			var mapped int
			if err := db.QueryRow(`SELECT COALESCE(SUM(m.quantity),0) FROM warehouse_storage_placement_lots m JOIN warehouse_storage_receipt_lots l ON l.id=m.position_id WHERE l.product_id=$1`, f.product).Scan(&mapped); err != nil || mapped != 2 {
				t.Fatalf("placement receipt lineage qty=%d err=%v", mapped, err)
			}
		})
	}
}

func TestFIN006SQLCannotInventAReceiptEvent(t *testing.T) {
	db, f, _, inbound, _, _ := inv09ReceiveTracked(t, 2)
	if err := NewWarehouseRepository(db).RecordQCPass(context.Background(), f.product, inbound, f.user, 2); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	fake := "forged-storage-" + uuid.NewString()
	if _, err = tx.Exec(`INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,inbound_id,goods_class,qty,stocked_at,original_stocked_at) SELECT $1,p.product_id,p.supplier_id,p.warehouse_id,p.inbound_id,p.goods_class,p.qty,p.stocked_at,p.original_stocked_at FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots l ON l.id=p.id WHERE l.product_id=$2`, fake, f.product); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`INSERT INTO warehouse_storage_receipt_lots(id,source_kind,source_event_id,receipt_line_id,receipt_movement_id,source_balance_id,product_id,variant_id,supplier_id,warehouse_id,initial_quantity,physical_received_at,billing_start_at,held_from,measurement_revision_id,length_mm,width_mm,height_mm,weight_grams,free_days,per_kg_day_toman,per_m3_day_toman,unit_day_toman,policy_version,history_status,unresolved_reason) SELECT $1,source_kind,'nonexistent-qc-event',receipt_line_id,receipt_movement_id,source_balance_id,product_id,variant_id,supplier_id,warehouse_id,initial_quantity,physical_received_at,billing_start_at,held_from,measurement_revision_id,length_mm,width_mm,height_mm,weight_grams,free_days,per_kg_day_toman,per_m3_day_toman,unit_day_toman,policy_version,history_status,unresolved_reason FROM warehouse_storage_receipt_lots WHERE product_id=$2`, fake, f.product)
	if err == nil {
		t.Fatal("direct SQL certified a nonexistent receipt and invented billable age")
	}
}

func TestFIN006CustomerReturnOwnsFreshReceiptBeforeShelving(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "tracked"}[tracked], func(t *testing.T) {
			db, f, warehouse, bin, sku, ret, item := inv06ReceivedReturn(t, tracked, 2)
			var position string
			var qty int
			var billing, physical time.Time
			if err := db.QueryRow(`SELECT l.id,p.qty,l.billing_start_at,l.physical_received_at FROM warehouse_storage_receipt_lots l JOIN warehouse_storage_positions p ON p.id=l.id WHERE l.source_kind='customer_return' AND l.source_event_id=$1 AND l.variant_id=$2 AND l.warehouse_id=$3 AND p.goods_class='returned'`, item, sku, warehouse).Scan(&position, &qty, &billing, &physical); err != nil || qty != 2 || physical.After(billing) {
				t.Fatalf("QC returned cohort missing: qty=%d err=%v", qty, err)
			}
			ctx := context.Background()
			wh := NewWarehouseRepository(db)
			if _, err := wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 1, "returned cohort", f.user, uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			var positions, mapped int
			if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_positions WHERE product_id=$1 AND goods_class='returned'`, f.product).Scan(&positions); err != nil || positions != 1 {
				t.Fatalf("shelving duplicated fee receipt: %d %v", positions, err)
			}
			if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM warehouse_storage_placement_lots WHERE position_id=$1`, position).Scan(&mapped); err != nil || mapped != 1 {
				t.Fatalf("return placement lineage=%d %v", mapped, err)
			}
			order := inv06PaidOrder(t, db, f, warehouse, sku, 1)
			opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
			if err := wh.ShipOrderWithActor(ctx, order, "post", "return-fee-"+order, f.user); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT qty FROM warehouse_storage_positions WHERE id=$1`, position).Scan(&qty); err != nil || qty != 1 {
				t.Fatalf("exact return departure qty=%d %v", qty, err)
			}
		})
	}
}

func TestFIN006InvalidPolicyDoesNotBlockPhysicalQCCustody(t *testing.T) {
	// F-01 (TEST-02 round 5 supervisor): this test mutates the GLOBAL
	// platform_settings storage tariff to invalid values. Without a
	// restore, every later lot creation in the shared DB writes
	// unit_day_toman=NULL + unresolved_policy and poisons later tests
	// (INV10). Capture the pre-test row and restore it in t.Cleanup;
	// this does not assume seed values.
	db := operationsDB(t)
	var before StorageFeeSettings
	if err := db.QueryRow(`SELECT storage_fee_enabled,storage_fee_free_days,storage_fee_per_kg_day,storage_fee_per_m3_day FROM platform_settings WHERE id=1`).Scan(&before.Enabled, &before.FreeDays, &before.PerKgDay, &before.PerM3Day); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		opsExec(t, db, `UPDATE platform_settings SET storage_fee_enabled=$1,storage_fee_free_days=$2,storage_fee_per_kg_day=$3,storage_fee_per_m3_day=$4 WHERE id=1`, before.Enabled, before.FreeDays, before.PerKgDay, before.PerM3Day)
	})
	for _, config := range []string{`storage_fee_per_kg_day=9007199254740991`, `storage_fee_free_days=9999999`, `storage_fee_per_kg_day=-1`} {
		t.Run(config, func(t *testing.T) {
			db := operationsDB(t)
			f := opsFixtureNew(t, db)
			wh := "policy-" + uuid.NewString()
			inbound := inv09ReceiveLegacy(t, db, f, wh, 2)
			if err := NewSupplierRepository(db).RecordProductMeasurement(context.Background(), f.product, f.user, 500, 400, 300, 1500, "policy test"); err != nil {
				t.Fatal(err)
			}
			opsExec(t, db, `UPDATE platform_settings SET `+config+` WHERE id=1`)
			if err := NewWarehouseRepository(db).RecordQCPass(context.Background(), f.product, inbound, f.user, 2); err != nil {
				t.Fatalf("invalid fee setting blocked physical custody: %v", err)
			}
			var qty int
			var status string
			if err := db.QueryRow(`SELECT p.qty,l.history_status FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots l ON l.id=p.id WHERE p.product_id=$1`, f.product).Scan(&qty, &status); err != nil || qty != 2 || status != "unresolved_policy" {
				t.Fatalf("invalid tariff became billable %d %s %v", qty, status, err)
			}
			inv09PlacePending(t, db, f, inbound, wh)
		})
	}
}

func TestINV10TwoWarehousesAndSKUsKeepExactCustody(t *testing.T) {
	db, f, one, inbound, red, blue := inv09ReceiveTracked(t, 2)
	ctx := context.Background()
	sup, wh := NewSupplierRepository(db), NewWarehouseRepository(db)
	if _, err := sup.RecordSKUPhysicalMeasurement(ctx, f.product, blue, f.user, 100, 100, 100, 500, "blue receipt snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := wh.RecordQCPass(ctx, f.product, inbound, f.user, 2); err != nil {
		t.Fatal(err)
	}
	inv09PlacePending(t, db, f, inbound, one)
	two := "history-two-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, two)
	var existing int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if _, err := sup.AdjustProductStock(ctx, f.supplier, f.product, red, existing+1, "new supplier unit for second warehouse", f.user); err != nil {
		t.Fatal(err)
	}
	sh, err := sup.CreateCartonShipment(ctx, f.supplier, []CartonShipmentItem{{VariantID: red, ExpectedQty: 1}, {VariantID: blue, ExpectedQty: 1}}, two, "history-two-key")
	if err != nil {
		t.Fatal(err)
	}
	if err = sup.ChangeCartonDispatch(ctx, f.supplier, sh.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = sup.ReceiveWarehouseShipment(ctx, sh.ShipmentCode, []WarehouseReceiveItem{{VariantID: red, ReceivedQty: 1, QCStatus: "pass"}, {VariantID: blue, ReceivedQty: 1, QCStatus: "pass"}}); err != nil {
		t.Fatal(err)
	}
	for _, sku := range []string{red, blue} {
		var line string
		if err = db.QueryRow(`SELECT id FROM inbound_inventory_lines WHERE shipment_id=$1 AND variant_id=$2`, sh.ID, sku).Scan(&line); err != nil {
			t.Fatal(err)
		}
		if err = wh.RecordQCPass(ctx, f.product, sh.ID, f.user, 1, line); err != nil {
			t.Fatal(err)
		}
	}
	inv09PlacePending(t, db, f, sh.ID, two)
	order := inv06PaidOrder(t, db, f, two, blue)
	var total int
	if err = db.QueryRow(`SELECT SUM(qty) FROM warehouse_storage_positions WHERE product_id=$1`, f.product).Scan(&total); err != nil || total != 4 {
		t.Fatalf("paid capture changed held quantity %d %v", total, err)
	}
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err = wh.ShipOrderWithActor(ctx, order, "post", "exact-two-"+order, f.user); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		wh, sku string
		q       int
		unit    int64
	}{{one, red, 2, 208}, {two, red, 1, 208}, {two, blue, 0, 1008}} {
		var q int
		var unit int64
		if err = db.QueryRow(`SELECT p.qty,l.unit_day_toman FROM warehouse_storage_receipt_lots l JOIN warehouse_storage_positions p ON p.id=l.id WHERE l.warehouse_id=$1 AND l.variant_id=$2`, want.wh, want.sku).Scan(&q, &unit); err != nil || q != want.q || unit != want.unit {
			t.Fatalf("wrong sibling/warehouse history %d/%d want%d/%d %v", q, unit, want.q, want.unit, err)
		}
	}
	var mismatch int
	if err = db.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_history_audit WHERE product_id=$1 AND issue<>''`, f.product).Scan(&mismatch); err != nil || mismatch != 0 {
		t.Fatalf("unreconciled exact custody %d %v", mismatch, err)
	}
}

func TestFIN006SQLUnallocatedLegacyReturnCannotCertifyHistory(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	order, _ := opsOrder(t, db, f, true)
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	opsExec(t, db, `UPDATE orders SET inventory_captured=TRUE,status='delivered_confirmed',confirmed_at=NOW(),return_window_ends_at=NOW()+INTERVAL '7 days' WHERE id=$1`, order)
	var orderItem string
	if err := db.QueryRow(`SELECT id::text FROM order_items WHERE order_id=$1`, order).Scan(&orderItem); err != nil {
		t.Fatal(err)
	}
	repo := NewOrderRepository(db)
	ctx := context.Background()
	ret, err := repo.CreateCustomerReturn(ctx, f.user, order, "old order without allocation", []CustomerReturnItemRequest{{OrderItemID: orderItem, Qty: 1}}, CustomerReturnOptions{Prepaid: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ApproveCustomerReturn(ctx, ret.ID, "counted at return"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ReceiveCustomerReturn(ctx, ret.ID, []CustomerReturnItemQC{{ReturnItemID: ret.Items[0].ID, QCStatus: "ok"}}, f.user); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var source storageReceiptSource
	if err = tx.QueryRow(`SELECT r.return_item_id::text,m.id,r.source_balance_id,r.product_id,r.variant_id,r.supplier_id,r.warehouse_id,r.qty,m.created_at,r.received_at FROM warehouse_customer_return_bin_sources r JOIN inventory_movements m ON m.idempotency_key='crstk:'||r.return_id::text||':'||r.return_item_id::text||':sellable' WHERE r.return_id=$1 AND r.source_proven`, ret.ID).Scan(&source.Event, &source.Movement, &source.Balance, &source.Product, &source.Variant, &source.Supplier, &source.Warehouse, &source.Quantity, &source.PhysicalAt, &source.BillingAt); err != nil {
		t.Fatal(err)
	}
	source.Kind, source.GoodsClass, source.Line, source.HeldFrom = "customer_return", "returned", source.Event, source.BillingAt
	if err = insertStorageReceiptLotTx(ctx, tx, source); err == nil {
		t.Fatal("SQL certified unallocated historical order's returned stock")
	}
}
