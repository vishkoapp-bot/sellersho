package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func inv06ReceivedReturn(t *testing.T, tracked bool, quantity int, split ...int) (*sql.DB, opsFixture, string, string, string, string, string) {
	t.Helper()
	db, f, warehouse, bin, sku := inv06Placed(t, tracked, quantity)
	ctx := context.Background()
	order := inv06PaidOrder(t, db, f, warehouse, sku, quantity)
	seedVerifiedRefundFixturePayment(t, db, order)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err := NewWarehouseRepository(db).ShipOrderWithActor(ctx, order, "post", "original-"+order, f.user); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 0, 0, 0)
	// Delivery confirmation is external to this warehouse test; file and receive
	// the return through the actual business APIs and their financial settlement.
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW(),return_window_ends_at=NOW()+INTERVAL '7 days' WHERE id=$1`, order)
	var item string
	if err := db.QueryRow(`SELECT id::text FROM order_items WHERE order_id=$1`, order).Scan(&item); err != nil {
		t.Fatal(err)
	}
	repo := NewOrderRepository(db)
	if len(split) == 0 {
		split = []int{quantity}
	}
	requests := []CustomerReturnItemRequest{}
	for _, q := range split {
		requests = append(requests, CustomerReturnItemRequest{OrderItemID: item, Qty: q})
	}
	ret, err := repo.CreateCustomerReturn(ctx, f.user, order, "size changed", requests, CustomerReturnOptions{Prepaid: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ApproveCustomerReturn(ctx, ret.ID, "QC on receipt"); err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) != len(requests) {
		t.Fatalf("return items=%+v", ret.Items)
	}
	verdicts := []CustomerReturnItemQC{}
	for _, ri := range ret.Items {
		verdicts = append(verdicts, CustomerReturnItemQC{ReturnItemID: ri.ID, QCStatus: "ok"})
	}
	if _, err = repo.ReceiveCustomerReturn(ctx, ret.ID, verdicts, f.user); err != nil {
		t.Fatal(err)
	}
	return db, f, warehouse, bin, sku, ret.ID, ret.Items[0].ID
}

func TestINV06ReturnBinRejectsAmbiguousLegacyPool(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "ambiguous_legacy", true: "exact_sku_item"}[tracked], func(t *testing.T) {
			db, f, _, bin, _, ret, item := inv06ReceivedReturn(t, tracked, 2, 1, 1)
			wh := NewWarehouseRepository(db)
			sources, err := wh.ListCustomerReturnBinSources(context.Background(), ret)
			if err != nil || len(sources) != 2 {
				t.Fatalf("sources=%+v err=%v", sources, err)
			}
			for _, s := range sources {
				if s.SourceProven != tracked || (tracked && s.AvailableQuantity != 1) || (!tracked && s.AvailableQuantity != 0) {
					t.Fatalf("incorrect source attribution=%+v", s)
				}
			}
			_, err = wh.PlaceCustomerReturnItemInBin(context.Background(), ret, item, bin, 1, "one exact returned item", f.user, uuid.NewString())
			if tracked && err != nil {
				t.Fatal(err)
			}
			if !tracked && !errors.Is(err, ErrReturnBinConflict) {
				t.Fatalf("ambiguous legacy pool moved: %v", err)
			}
		})
	}
}

func TestINV06RefundedReturnStillEligibleForPhysicalBinEntry(t *testing.T) {
	db, f, warehouse, bin, _, ret, item := inv06ReceivedReturn(t, true, 1)
	ctx := context.Background()
	orderRepo := NewOrderRepository(db)
	if _, _, err := orderRepo.RefundCustomerReturn(ctx, ret, f.user); err != nil {
		t.Fatal(err)
	}
	queue, err := orderRepo.ListCustomerReturnsForWarehouse(ctx, warehouse)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range queue {
		if r.ID == ret {
			found = true
		}
	}
	if !found {
		t.Fatal("refunded physical return disappeared before bin entry")
	}
	if _, err = NewWarehouseRepository(db).PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 1, "refund does not remove goods", f.user, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 1, 1000, 100)
}

func TestINV06ReturnBinHonorsActiveInboundCapacityReservation(t *testing.T) {
	db, f, warehouse, bin, sku, ret, item := inv06ReceivedReturn(t, true, 2)
	ctx := context.Background()
	sup := NewSupplierRepository(db)
	wh := NewWarehouseRepository(db)
	if _, err := sup.AdjustProductStock(ctx, f.supplier, f.product, sku, 4, "additional receipt", f.user); err != nil {
		t.Fatal(err)
	}
	shipment, err := sup.CreateCartonShipment(ctx, f.supplier, []CartonShipmentItem{{VariantID: sku, ExpectedQty: 2}}, warehouse, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err = sup.ChangeCartonDispatch(ctx, f.supplier, shipment.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = sup.ReceiveWarehouseShipment(ctx, shipment.ShipmentCode, []WarehouseReceiveItem{{VariantID: sku, ReceivedQty: 2, QCStatus: "pass"}}); err != nil {
		t.Fatal(err)
	}
	if err = wh.RecordQCPass(ctx, f.product, shipment.ID, f.user, 2); err != nil {
		t.Fatal(err)
	}
	var task string
	if err = db.QueryRow(`SELECT id FROM putaway_tasks WHERE inbound_id=$1`, shipment.ID).Scan(&task); err != nil {
		t.Fatal(err)
	}
	if err = wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err = wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 1, "must preserve other reservation", f.user, uuid.NewString()); !errors.Is(err, ErrReturnBinConflict) {
		t.Fatalf("reserved bin capacity reused: %v", err)
	}
	inv06AssertOccupancy(t, db, bin, 0, 0, 0)
	if err = wh.ConfirmPutaway(ctx, task, f.user); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 2, 2000, 200)
}

func TestINV06ReturnBinMigrationPreservesExistingPlacement(t *testing.T) {
	db, _, _, bin, _ := inv06Placed(t, true, 1)
	up, err := os.ReadFile("../../migrations/202611150002_customer_return_bin_entry.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611150002_customer_return_bin_entry.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(down)); err != nil {
		t.Fatalf("empty entry downgrade: %v", err)
	}
	if _, err = tx.Exec(string(up)); err != nil {
		t.Fatalf("entry upgrade: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 1, 1000, 100)
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM warehouse_placements WHERE bin_code=$1 AND source_kind='inbound_putaway' AND task_id IS NOT NULL AND return_entry_id IS NULL`, bin).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preserved placements=%d err=%v", count, err)
	}
}

func TestINV06ReturnBinPartialCaptureAndChangedReplay(t *testing.T) {
	db, f, warehouse, bin, sku, ret, item := inv06ReceivedReturn(t, true, 3)
	wh := NewWarehouseRepository(db)
	ctx := context.Background()
	key := uuid.NewString()
	order := inv06PaidOrder(t, db, f, warehouse, sku, 3)
	if _, err := wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 1, "cannot move captured floor unit", f.user, uuid.NewString()); !errors.Is(err, ErrReturnBinConflict) {
		t.Fatalf("captured unit moved: %v", err)
	}
	if err := NewOrderRepository(db).CancelOrder(ctx, order, f.user, true); err != nil {
		t.Fatal(err)
	}
	first, err := wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 1, "one measured unit", f.user, key)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := wh.ListCustomerReturnBinSources(ctx, ret)
	if err != nil || len(sources) != 1 || sources[0].AvailableQuantity != 2 {
		t.Fatalf("sources=%+v err=%v", sources, err)
	}
	if _, err = wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 3, "cannot move more than survives", f.user, uuid.NewString()); !errors.Is(err, ErrReturnBinConflict) {
		t.Fatalf("excess quantity moved: %v", err)
	}
	for _, change := range []struct {
		q      int
		reason string
	}{{2, "one measured unit"}, {1, "different reason"}} {
		if _, err = wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, change.q, change.reason, f.user, key); !errors.Is(err, ErrReturnBinConflict) {
			t.Fatalf("changed replay=%v", err)
		}
	}
	if _, err = wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 2, "last surviving units", f.user, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = NewSupplierRepository(db).RecordProductMeasurement(ctx, f.product, f.user, 200, 100, 100, 100, "later edit"); err != nil {
		t.Fatal(err)
	}
	replay, err := wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 1, "one measured unit", f.user, key)
	if err != nil || replay.ID != first.ID || replay.MeasurementRevision != first.MeasurementRevision {
		t.Fatalf("archived replay=%+v %v", replay, err)
	}
	inv06AssertOccupancy(t, db, bin, 3, 3000, 300)
	down, err := os.ReadFile("../../migrations/202611150002_customer_return_bin_entry.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(down)); err == nil {
		t.Fatal("populated downgrade destroyed return evidence")
	}
	inv06AssertOccupancy(t, db, bin, 3, 3000, 300)
}

func TestINV06ConcurrentIdenticalReturnBinEntry(t *testing.T) {
	db, f, _, bin, _, ret, item := inv06ReceivedReturn(t, true, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan ReturnBinPlacementResult, 8)
	failures := make(chan error, 8)
	key := uuid.NewString()
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := NewWarehouseRepository(db).PlaceCustomerReturnItemInBin(context.Background(), ret, item, bin, 2, "same physical command", f.user, key)
			results <- result
			failures <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Errorf("identical replay failed: %v", err)
		}
	}
	var id string
	for result := range results {
		if id == "" {
			id = result.ID
		}
		if result.ID != id {
			t.Errorf("different replay entry: %s != %s", result.ID, id)
		}
	}
	inv06AssertOccupancy(t, db, bin, 2, 2000, 200)
}

func TestINV06ReturnBinSourceAndCapacityConflictsAreAtomic(t *testing.T) {
	for _, kind := range []string{"unverified_sku", "wrong_warehouse", "disputed", "counter_mismatch", "capacity", "defective", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			db, f, warehouse, bin, sku, ret, item := inv06ReceivedReturn(t, true, 2)
			ctx := context.Background()
			switch kind {
			case "unverified_sku":
				g, l := 100, 100
				if _, err := NewSupplierRepository(db).UpdateVariantPhysical(ctx, f.supplier, f.product, sku, &g, &l, &l, &l); err != nil {
					t.Fatal(err)
				}
			case "wrong_warehouse":
				other := "other-" + uuid.NewString()
				opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'test','test')`, other)
				bin = inv08Bin(t, db, other, 2000, 200)
			case "disputed":
				opsExec(t, db, `UPDATE warehouse_bins SET capacity_status='disputed' WHERE bin_code=$1`, bin)
			case "counter_mismatch":
				opsExec(t, db, `UPDATE warehouse_bins SET quantity=1 WHERE bin_code=$1`, bin)
			case "capacity":
				opsExec(t, db, `UPDATE warehouse_bins SET capacity_volume_cm3=1 WHERE bin_code=$1`, bin)
			case "defective":
				opsExec(t, db, `UPDATE return_items SET qc_status='defective' WHERE id=$1`, item)
			case "reserved":
				if _, err := NewSupplierRepository(db).CreateReturnRequestForVariantWithIdempotency(ctx, f.supplier, f.product, sku, 2, "supplier_collect", "return to supplier", uuid.NewString(), f.user); err != nil {
					t.Fatal(err)
				}
			}
			var before int
			if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1 AND status='sellable'`, f.product).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if _, err := NewWarehouseRepository(db).PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 1, "must reject", f.user, uuid.NewString()); !errors.Is(err, ErrReturnBinConflict) {
				t.Fatalf("%s accepted: %v", kind, err)
			}
			var after, entries int
			if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1 AND status='sellable'`, f.product).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_return_bin_entries WHERE return_id=$1`, ret).Scan(&entries); err != nil || before != after || entries != 0 {
				t.Fatalf("partial write before=%d after=%d entries=%d err=%v", before, after, entries, err)
			}
			_ = warehouse
		})
	}
}

func TestINV06ConcurrentLastReturnUnitAndSQLPlacementForgery(t *testing.T) {
	db, f, _, bin, _, ret, item := inv06ReceivedReturn(t, true, 1)
	var wg sync.WaitGroup
	errorsOut := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := NewWarehouseRepository(db).PlaceCustomerReturnItemInBin(context.Background(), ret, item, bin, 1, "last unit", f.user, uuid.NewString())
			errorsOut <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errorsOut)
	success, conflict := 0, 0
	for err := range errorsOut {
		if err == nil {
			success++
		} else if errors.Is(err, ErrReturnBinConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("last unit success=%d conflicts=%d", success, conflict)
	}
	var entry string
	if err := db.QueryRow(`SELECT id FROM warehouse_return_bin_entries WHERE return_item_id=$1`, item).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO warehouse_placements(id,task_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,quantity,volume_cm3,weight_grams,measurement_revision_id,source_kind,return_entry_id) SELECT 'forged',NULL,product_id,variant_id,supplier_id,warehouse_id,bin_code,1,1,1,measurement_revision_id,'customer_return',id FROM warehouse_return_bin_entries WHERE id=$1`, entry); err == nil {
		t.Fatal("forged return placement accepted")
	}
	if _, err := db.Exec(`UPDATE warehouse_return_bin_entries SET quantity=2 WHERE id=$1`, entry); err == nil {
		t.Fatal("immutable return entry rewritten")
	}
	inv06AssertOccupancy(t, db, bin, 1, 1000, 100)
}

func TestINV06CustomerReturnFreshBinEntryAndSecondDeparture(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "tracked"}[tracked], func(t *testing.T) {
			db, f, warehouse, oldBin, sku, ret, item := inv06ReceivedReturn(t, tracked, 2)
			ctx := context.Background()
			if err := NewSupplierRepository(db).RecordProductMeasurement(ctx, f.product, f.user, 200, 100, 100, 150, "fresh return measurement"); err != nil {
				t.Fatal(err)
			}
			bin := inv08Bin(t, db, warehouse, 4000, 300)
			wh := NewWarehouseRepository(db)
			key := uuid.NewString()
			result, err := wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 2, "QC-passed return", f.user, key)
			if err != nil {
				t.Fatal(err)
			}
			if result.Quantity != 2 || result.VolumeCM3 != 4000 || result.WeightGrams != 300 {
				t.Fatalf("placement=%+v", result)
			}
			var receiptClock bool
			if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots l ON l.id=p.id JOIN warehouse_return_bin_entries e ON l.source_kind='customer_return' AND l.source_event_id=e.return_item_id::text WHERE e.id=$1 AND p.goods_class='returned' AND p.stocked_at=e.received_at AND p.original_stocked_at=e.received_at AND p.qty=e.quantity)`, result.ID).Scan(&receiptClock); err != nil || !receiptClock {
				t.Fatalf("return receipt clock lost: %v", err)
			}
			replay, err := wh.PlaceCustomerReturnItemInBin(ctx, ret, item, bin, 2, "QC-passed return", f.user, key)
			if err != nil || replay.ID != result.ID {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
			inv06AssertOccupancy(t, db, oldBin, 0, 0, 0)
			inv06AssertOccupancy(t, db, bin, 2, 4000, 300)
			order := inv06PaidOrder(t, db, f, warehouse, sku, 2)
			inv06AssertOccupancy(t, db, bin, 2, 4000, 300)
			opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
			if err = wh.ShipOrderWithActor(ctx, order, "post", "second-"+order, f.user); err != nil {
				t.Fatal(err)
			}
			inv06AssertOccupancy(t, db, bin, 0, 0, 0)
			var count, volume int
			if err = db.QueryRow(`SELECT COUNT(*),SUM(r.volume_cm3) FROM warehouse_occupancy_releases r JOIN warehouse_placements p ON p.id=r.placement_id WHERE p.product_id=$1`, f.product).Scan(&count, &volume); err != nil || count != 2 || volume != 6000 {
				t.Fatalf("releases=%d volume=%d err=%v", count, volume, err)
			}
		})
	}
}

func TestINV06DirectReturnBinCommandRollsBackEveryEffectOnFailure(t *testing.T) {
	db, f, _, bin, _, ret, item := inv06ReceivedReturn(t, true, 1)
	id := "RBP-" + uuid.NewString()
	// Inject a deterministic collision after the command's balance debit/credit.
	// The real SQL trigger must roll back those effects and its header together.
	opsExec(t, db, `INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,reference_type,reference_id,idempotency_key) VALUES($1,$2,$3,1,'test_collision',$4,$4)`, "IM-"+id, f.product, f.supplier, id)
	if _, err := db.Exec(`INSERT INTO warehouse_return_bin_entries(id,return_id,return_item_id,bin_code,quantity,reason,actor_user_id,idempotency_key) VALUES($1,$2,$3,$4,1,'direct command',$5,$6)`, id, ret, item, bin, f.user, uuid.NewString()); err == nil {
		t.Fatal("injected failure unexpectedly succeeded")
	}
	var qty, headers, placements int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1 AND variant_id IS NOT NULL AND status='sellable' AND location_type='warehouse'`, f.product).Scan(&qty); err != nil || qty != 1 {
		t.Fatalf("rolled-back floor quantity=%d err=%v", qty, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_return_bin_entries WHERE return_item_id=$1`, item).Scan(&headers); err != nil || headers != 0 {
		t.Fatalf("orphan header=%d err=%v", headers, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_placements WHERE return_entry_id=$1`, id).Scan(&placements); err != nil || placements != 0 {
		t.Fatalf("orphan placement=%d err=%v", placements, err)
	}
	inv06AssertOccupancy(t, db, bin, 0, 0, 0)
	// Direct SQL has the same conserved action; it cannot create a header alone.
	next := "RBP-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO warehouse_return_bin_entries(id,return_id,return_item_id,bin_code,quantity,reason,actor_user_id,idempotency_key) VALUES($1,$2,$3,$4,1,'direct valid command',$5,$6)`, next, ret, item, bin, f.user, uuid.NewString())
	inv06AssertOccupancy(t, db, bin, 1, 1000, 100)
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_placements p JOIN warehouse_return_bin_entries e ON e.id=p.return_entry_id JOIN inventory_movements m ON m.id='IM-'||e.id WHERE e.id=$1 AND p.id='PLACE-'||e.id AND p.task_id IS NULL AND p.source_kind='customer_return' AND p.quantity=m.quantity`, next).Scan(&placements); err != nil || placements != 1 {
		t.Fatalf("direct SQL evidence=%d err=%v", placements, err)
	}
}
