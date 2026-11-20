package repository

// INV-12 Phase A — regression promotion: the named race tests and the
// mixed-workload conservation oracle the audit (§6/§7) found missing.
//
// §1 FILE MAPPING (audit Finding 1): the spec's audit_supply_test.go and
// audit_order_test.go DO NOT EXIST anywhere in the repo. The six audit
// inventory/order reproductions already live in maintained integration test
// files; this file does not re-import them, it records their real locations:
//   - TestAuditSupplyReceiptDoesNotRecreateSoldTransferredUnits — inbound_inventory_integration_test.go (INV-02)
//   - TestAuditSupplyFailedLegacyQCRemovesDeclaredSellableStock — inv04_disposition_integration_test.go (INV-04)
//   - TestAuditSupplyWarehouseOriginCannotCaptureSupplierOnlyLot — inventory_origin_integration_test.go (INV-05)
//   - TestAuditSupplyLegacyAggregateEditDoesNotAddWarehouseStockTwice — legacy_stock_adjustment_integration_test.go (INV-03)
//   - TestAuditOrderPartiallyShippedCannotRestoreShippedStock — order_cancellation_dispatch_test.go (INV-01)
//   - TestAuditSupplyShippingReleasesActualPutawayBinCapacity — warehouse_occupancy_integration_test.go (INV-06)
//
// GUARD: every test here opens its database through testdb.Open (via
// skuInventoryFixture / operationsDB), which refuses any target whose
// public.sellersho_test_guard marker does not match the disposable database
// name. TestINV12DisposableDBGuardAndAuditFileMapping asserts the marker
// directly. No test in this file touches production data or money paths.

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestINV12ReceiptVsCaptureSameLotExactlyOnce — audit §6, receipt/capture race.
//
// One tracked red unit is sellable on the same lot. A physical receipt of one
// more unit races a capture of one unit. Both must succeed (the receipt only
// adds stock) and the effect must be exactly-once: balance 1 - 1 + 1 = 1, one
// capture row, one capture movement, one receipt movement, no negatives.
func TestINV12ReceiptVsCaptureSameLotExactlyOnce(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	itemID := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'inv12-receipt-race','p',$2,1)`, itemID, red); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	ctx := context.Background()
	type outcome struct {
		kind string
		err  error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tx, err := scoped.Begin()
		if err != nil {
			results <- outcome{"receipt", err}
			return
		}
		defer tx.Rollback()
		err = creditSKUInventoryTx(ctx, tx, "p", "s", red, 1, "sellable", "supplier", "supplier-warehouse:sw1", "inbound_receipt", "inv12-receipt", "inv12-receipt-"+uuid.NewString(), "")
		if err == nil {
			err = tx.Commit()
		}
		results <- outcome{"receipt", err}
	}()
	go func() {
		defer wg.Done()
		tx, err := scoped.Begin()
		if err != nil {
			results <- outcome{"capture", err}
			return
		}
		defer tx.Rollback()
		_, err = captureOrderInventoryTx(ctx, tx, "inv12-receipt-race")
		if err == nil {
			err = tx.Commit()
		}
		results <- outcome{"capture", err}
	}()
	wg.Wait()
	close(results)
	for r := range results {
		if r.err != nil {
			t.Fatalf("%s failed: %v", r.kind, r.err)
		}
	}
	var balance, variantStock, productStock, captures, captureMoves, receiptMoves, negatives int
	if err := scoped.QueryRow(`SELECT quantity FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, red).Scan(&balance); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&variantStock); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT stock FROM products WHERE id='p'`).Scan(&productStock); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT COUNT(*) FROM order_inventory_captures WHERE order_id='inv12-receipt-race'`).Scan(&captures); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE variant_id=$1 AND reference_type='order_capture'`, red).Scan(&captureMoves); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE variant_id=$1 AND reference_type='inbound_receipt'`, red).Scan(&receiptMoves); err != nil {
		t.Fatal(err)
	}
	if err := scoped.QueryRow(`SELECT COUNT(*) FROM inventory_balances WHERE product_id='p' AND quantity<0`).Scan(&negatives); err != nil {
		t.Fatal(err)
	}
	// Initial red 1 - captured 1 + receipt 1 = 1; blue untouched at 3, so the
	// product projection is 1 + 3 = 4.
	if balance != 1 || variantStock != 1 || productStock != 4 || captures != 1 || captureMoves != 1 || receiptMoves != 1 || negatives != 0 {
		t.Fatalf("receipt/capture race: balance=%d variantStock=%d productStock=%d captures=%d captureMoves=%d receiptMoves=%d negatives=%d", balance, variantStock, productStock, captures, captureMoves, receiptMoves, negatives)
	}
}

// inv12SupplierOrigin inserts a single supplier-origin leg for an order's one
// line, mirroring seedCanonicalWarehouseOrderOrigins for the supplier side.
func inv12SupplierOrigin(t *testing.T, db *sql.DB, order, supplier, source string) {
	t.Helper()
	var itemID string
	if err := db.QueryRow(`SELECT id::text FROM order_items WHERE order_id=$1`, order).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,supplier_warehouse_id,quantity,origin_label) VALUES($1,$2,$3::uuid,0,'supplier',$4,$5,1,'INV-12 supplier origin')`, "inv12-origin-"+uuid.NewString()[:8], order, itemID, supplier, source)
}

// inv12PaidOrderWithSupplierOrigin builds a paid order for qty 1 of a tracked
// variant against a supplier origin and pays it (capturing the unit).
func inv12PaidOrderWithSupplierOrigin(t *testing.T, db *sql.DB, f opsFixture, variant, source string) string {
	t.Helper()
	order, token := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, variant)
	inv12SupplierOrigin(t, db, order, f.supplier, source)
	if _, err := NewOrderRepository(db).MarkPaymentPaid(context.Background(), token, "rrn-"+uuid.NewString()[:20], "mask"); err != nil {
		t.Fatal(err)
	}
	return order
}

// TestINV12SupplierReturnVsCaptureSameBalanceExactlyOnce — audit §6,
// supplier-return/capture race.
//
// One tracked unit is sellable on the same balance. A supplier return of that
// unit races a capture of that unit. Exactly one must win; the loser must
// fail (not silently double-spend). Conservation: sellable + return_requested
// + captured == 1, and the active reservation quantity equals the
// return_requested balance.
func TestINV12SupplierReturnVsCaptureSameBalanceExactlyOnce(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	source := "sw-" + uuid.NewString()
	red := uuid.NewString()
	opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'p','c')`, source, f.supplier)
	opsExec(t, db, `UPDATE products SET schema_version=2,product_type='variable',attribute_schema='{}',origin_supplier_warehouse_id=$1 WHERE id=$2`, source, f.product)
	opsExec(t, db, `INSERT INTO product_variants(id,product_id,spnc,inventory_mode,stock) VALUES($1,$2,$3,'tracked',1)`, red, f.product, "R"+red[:10])
	opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,$4,'supplier','supplier',$5,'sellable',1,$6)`, uuid.NewString(), f.product, f.supplier, red, "supplier-warehouse:"+source, source)

	order, token := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, red)
	inv12SupplierOrigin(t, db, order, f.supplier, source)

	sup := NewSupplierRepository(db)
	orders := NewOrderRepository(db)
	type outcome struct {
		kind string
		err  error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := sup.CreateReturnRequestForVariantWithIdempotency(ctx, f.supplier, f.product, red, 1, "supplier_collect", "inv12 return race", "inv12-return-"+uuid.NewString(), f.user)
		results <- outcome{"return", err}
	}()
	go func() {
		defer wg.Done()
		_, err := orders.MarkPaymentPaid(ctx, token, "rrn-"+uuid.NewString()[:20], "mask")
		results <- outcome{"capture", err}
	}()
	wg.Wait()
	close(results)
	successes := 0
	for r := range results {
		if r.err == nil {
			successes++
		} else {
			t.Logf("inv12 return/capture race loser: %s: %v", r.kind, r.err)
		}
	}
	if successes != 1 {
		t.Fatalf("return/capture race successes=%d, want exactly 1", successes)
	}
	var sellable, requested, captured, reserved, negatives int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, red).Scan(&sellable); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='return_requested'`, red).Scan(&requested); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM order_inventory_captures WHERE variant_id=$1`, red).Scan(&captured); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations WHERE variant_id=$1 AND reference_type='supplier_return' AND status='active'`, red).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_balances WHERE variant_id=$1 AND quantity<0`, red).Scan(&negatives); err != nil {
		t.Fatal(err)
	}
	if sellable+requested+captured != 1 || reserved != requested || negatives != 0 {
		t.Fatalf("return/capture race: sellable=%d return_requested=%d captured=%d reserved=%d negatives=%d", sellable, requested, captured, reserved, negatives)
	}
}

// TestINV12CapacityAssignmentVsDepartureSamePosition — audit §6,
// capacity-assignment/departure race.
//
// Two units sit in a bin sized for exactly two. A paid order for one unit
// ships (departure releases one unit of bin capacity) while a second putaway
// task for one more unit of the same product is assigned into the same bin.
// The assignment succeeds only if the departure releases capacity first;
// either way the departure happens exactly once and the bin ends consistent:
// quantity 1, volume 1000 cm3, weight 100 g, no history exception.
func TestINV12CapacityAssignmentVsDepartureSamePosition(t *testing.T) {
	db, f, warehouse, bin, sku := inv06Placed(t, true, 2)
	ctx := context.Background()
	order := inv06PaidOrder(t, db, f, warehouse, sku, 1)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	task2, _ := fin006KnownReceipt(t, db, f, warehouse, 1, time.Now())
	wh := NewWarehouseRepository(db)
	type outcome struct {
		kind string
		err  error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- outcome{"assign", wh.AssignPutawayTask(ctx, task2, bin, f.user)}
	}()
	go func() {
		defer wg.Done()
		results <- outcome{"ship", wh.ShipOrderWithActor(ctx, order, "post", "TRK-inv12-"+order, f.user)}
	}()
	wg.Wait()
	close(results)
	var shipErr, assignErr error
	for r := range results {
		if r.kind == "ship" {
			shipErr = r.err
		} else {
			assignErr = r.err
		}
	}
	if shipErr != nil {
		t.Fatalf("departure failed: %v", shipErr)
	}
	// The only legitimate assignment failure is the bin being full at the
	// moment it ran (departure had not released capacity yet).
	if assignErr != nil && !strings.Contains(assignErr.Error(), "insufficient bin volume capacity") {
		t.Fatalf("assignment failed for an unexpected reason: %v", assignErr)
	}
	var departed, exceptions, binQty, binVol, binWeight, task2Reservations int
	var task2Status string
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM warehouse_storage_lot_departures WHERE event_type='warehouse_shipment' AND event_id=$1`, order).Scan(&departed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_history_exceptions WHERE event_type='warehouse_shipment' AND event_id=$1`, order).Scan(&exceptions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT quantity,occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code=$1`, bin).Scan(&binQty, &binVol, &binWeight); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM putaway_capacity_reservations WHERE task_id=$1 AND status='active'`, task2).Scan(&task2Reservations); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM putaway_tasks WHERE id=$1`, task2).Scan(&task2Status); err != nil {
		t.Fatal(err)
	}
	if departed != 1 || exceptions != 0 || binQty != 1 || binVol != 1000 || binWeight != 100 {
		t.Fatalf("assignment/departure race: departed=%d exceptions=%d binQty=%d binVol=%d binWeight=%d assignErr=%v", departed, exceptions, binQty, binVol, binWeight, assignErr)
	}
	// Assignment outcome and reservation must agree: a successful assignment
	// holds exactly one 1-unit reservation for the second task, a failed one
	// holds none and leaves the task pending.
	if assignErr == nil && (task2Reservations != 1 || task2Status != "assigned") {
		t.Fatalf("successful assignment left reservations=%d status=%q", task2Reservations, task2Status)
	}
	if assignErr != nil && (task2Reservations != 0 || task2Status != "pending") {
		t.Fatalf("failed assignment left reservations=%d status=%q", task2Reservations, task2Status)
	}
}

// TestINV12DoubleDepartureSameEventExactlyOnce — audit §6, double-subtracted
// physical departure.
//
// The occupancy release for a paid, packed order is pre-created exactly as
// ShipOrderWithActor does, then two concurrent consumeStorageBusinessEventTx
// calls run on the same warehouse_shipment event. Exactly one must succeed;
// the other is a no-op/error — never a second subtraction. Final departures
// sum to the order quantity and no history exception is written.
func TestINV12DoubleDepartureSameEventExactlyOnce(t *testing.T) {
	db, f, warehouse, _, sku := inv06Placed(t, true, 1)
	ctx := context.Background()
	order := inv06PaidOrder(t, db, f, warehouse, sku, 1)
	// ShipOrderWithActor marks the order shipped before it releases
	// occupancy; the warehouse_occupancy_releases trigger requires exactly
	// that evidence (status='shipped', paid, shipped_at set). Reproduce
	// that mid-transaction state here, minus the departure itself.
	opsExec(t, db, `UPDATE orders SET status='shipped',fulfillment_method='warehouse',shipped_at=NOW() WHERE id=$1`, order)
	setup, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := loadOrderDepartureAllocationsTx(ctx, setup, order)
	if err != nil {
		setup.Rollback()
		t.Fatal(err)
	}
	if err := releasePlacementOccupancyTx(ctx, setup, order, "warehouse_shipment", f.user, allocations); err != nil {
		setup.Rollback()
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	consume := func() error {
		// LockTransactionRowsTx requires a bounded context.
		callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tx, err := db.BeginTx(callCtx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := consumeStorageBusinessEventTx(callCtx, tx, "warehouse_shipment", order, f.user); err != nil {
			return err
		}
		return tx.Commit()
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- consume() }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			t.Logf("inv12 double-departure loser: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("double departure successes=%d, want exactly 1", successes)
	}
	var departed, departureRows, exceptions int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0),COUNT(*) FROM warehouse_storage_lot_departures WHERE event_type='warehouse_shipment' AND event_id=$1`, order).Scan(&departed, &departureRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_history_exceptions WHERE event_type='warehouse_shipment' AND event_id=$1`, order).Scan(&exceptions); err != nil {
		t.Fatal(err)
	}
	if departed != 1 || departureRows != 1 || exceptions != 0 {
		t.Fatalf("double departure: departed=%d rows=%d exceptions=%d", departed, departureRows, exceptions)
	}
}

// TestINV12MixedWorkloadConservationOracle — audit §6, disjoint per-lot/event
// conservation.
//
// One mixed workload over two tracked lots (red: warehouse/bin, blue:
// supplier): receipts + a supplier-return reservation + captures + a restore
// + a physical departure. The oracle asserts per-lot and per-event disjoint
// conservation: movement sums equal physical deltas, no negatives, no orphan
// reservation rows, no origin mismatch, and the product/variant projections
// equal the sellable balance sums.
func TestINV12MixedWorkloadConservationOracle(t *testing.T) {
	db, f, warehouse, shipment, red, blue := inv09ReceiveTracked(t, 5)
	ctx := context.Background()
	wh := NewWarehouseRepository(db)
	if err := wh.RecordQCPass(ctx, f.product, shipment, f.user, 5); err != nil {
		t.Fatal(err)
	}
	inv09PlacePending(t, db, f, shipment, warehouse)
	// red: 5 sellable in a bin. blue: 3 sellable at the supplier.
	var source string
	if err := db.QueryRow(`SELECT origin_supplier_warehouse_id FROM products WHERE id=$1`, f.product).Scan(&source); err != nil {
		t.Fatal(err)
	}

	// Reservation: supplier return of blue x2.
	sup := NewSupplierRepository(db)
	if _, err := sup.CreateReturnRequestForVariantWithIdempotency(ctx, f.supplier, f.product, blue, 2, "supplier_collect", "inv12 oracle return", "inv12-oracle-return-"+uuid.NewString(), f.user); err != nil {
		t.Fatal(err)
	}

	// Captures: red x2 (warehouse origin) and blue x1 (supplier origin).
	orders := NewOrderRepository(db)
	redOrder, redToken := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET quantity=2, variant_id=$2 WHERE order_id=$1`, redOrder, red)
	opsExec(t, db, `UPDATE orders SET total_amount=4000,payable_amount=4000 WHERE id=$1`, redOrder)
	opsExec(t, db, `UPDATE payment_transactions SET amount=4000 WHERE order_id=$1`, redOrder)
	seedCanonicalWarehouseOrderOrigins(t, db, redOrder, warehouse)
	if _, err := orders.MarkPaymentPaid(ctx, redToken, "rrn-"+uuid.NewString()[:20], "mask"); err != nil {
		t.Fatal(err)
	}
	blueOrder := inv12PaidOrderWithSupplierOrigin(t, db, f, blue, source)
	_ = blueOrder // captured; its capture row is part of the blue assertions below.

	// Restore: cancel the red order, returning its 2 units.
	if err := orders.CancelOrder(ctx, redOrder, f.user, true); err != nil {
		t.Fatal(err)
	}

	// Departure: a second red order for 1 unit ships from the bin.
	shipOrder := inv06PaidOrder(t, db, f, warehouse, red, 1)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, shipOrder)
	if err := wh.ShipOrderWithActor(ctx, shipOrder, "post", "TRK-inv12-oracle-"+shipOrder, f.user); err != nil {
		t.Fatal(err)
	}

	// --- Oracle: per-lot conservation ---
	// red: 5 received - 3 captured (2 + 1) + 2 restored = 4 sellable.
	// blue: 3 received - 1 captured - 2 reserved = 0 sellable, 2 return_requested.
	var redSellable, redCaptured, redRestored, redCapMoves, redCancelMoves int
	var blueSellable, blueRequested, blueCaptured int
	var negatives, orphanReservations, originMismatches int
	var productStock, redStock, blueStock int
	queries := []struct {
		q    string
		args []any
		dst  *int
	}{
		{`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, []any{red}, &redSellable},
		{`SELECT COALESCE(SUM(quantity),0) FROM order_inventory_captures WHERE variant_id=$1`, []any{red}, &redCaptured},
		{`SELECT COALESCE(SUM(restored_quantity),0) FROM order_inventory_captures WHERE variant_id=$1`, []any{red}, &redRestored},
		{`SELECT COALESCE(SUM(quantity),0) FROM inventory_movements WHERE variant_id=$1 AND reference_type='order_capture'`, []any{red}, &redCapMoves},
		{`SELECT COALESCE(SUM(quantity),0) FROM inventory_movements WHERE variant_id=$1 AND reference_type='order_cancel'`, []any{red}, &redCancelMoves},
		{`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, []any{blue}, &blueSellable},
		{`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=$1 AND status='return_requested'`, []any{blue}, &blueRequested},
		{`SELECT COALESCE(SUM(quantity),0) FROM order_inventory_captures WHERE variant_id=$1`, []any{blue}, &blueCaptured},
		{`SELECT COUNT(*) FROM inventory_balances WHERE product_id=$1 AND quantity<0`, []any{f.product}, &negatives},
		// Orphan reservation: an active supplier-return reservation with no
		// live return request behind it.
		{`SELECT COUNT(*) FROM inventory_reservations r WHERE r.product_id=$1 AND r.status='active' AND NOT EXISTS(SELECT 1 FROM supplier_return_requests q WHERE q.id=r.reference_id AND q.status NOT IN ('cancelled','rejected'))`, []any{f.product}, &orphanReservations},
		// Origin mismatch: a capture allocation whose balance location does
		// not match the origin leg it was consumed for.
		{`SELECT COUNT(*) FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a JOIN inventory_balances b ON b.id=a->>'balanceId' JOIN order_item_origins o ON o.order_item_id::text=c.order_item_id AND o.id=a->>'originId' WHERE c.product_id=$1 AND ((o.origin_type='warehouse' AND b.location_type NOT IN ('warehouse','bin')) OR (o.origin_type='supplier' AND b.location_type<>'supplier'))`, []any{f.product}, &originMismatches},
		{`SELECT stock FROM products WHERE id=$1`, []any{f.product}, &productStock},
		{`SELECT stock FROM product_variants WHERE id=$1`, []any{red}, &redStock},
		{`SELECT stock FROM product_variants WHERE id=$1`, []any{blue}, &blueStock},
	}
	for _, qq := range queries {
		if err := db.QueryRow(qq.q, qq.args...).Scan(qq.dst); err != nil {
			t.Fatalf("oracle query %q: %v", qq.q, err)
		}
	}
	var departed, blueReserved int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM warehouse_storage_lot_departures WHERE event_type='warehouse_shipment' AND event_id=$1`, shipOrder).Scan(&departed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations WHERE variant_id=$1 AND reference_type='supplier_return' AND status='active'`, blue).Scan(&blueReserved); err != nil {
		t.Fatal(err)
	}
	// Cancelled red order must never depart; the shipped order departs once.
	var redOrderDeparted int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM warehouse_storage_lot_departures WHERE event_type='warehouse_shipment' AND event_id=$1`, redOrder).Scan(&redOrderDeparted); err != nil {
		t.Fatal(err)
	}
	if redSellable != 4 || redCaptured != 3 || redRestored != 2 || redCapMoves != 3 || redCancelMoves != 2 {
		t.Fatalf("oracle red: sellable=%d captured=%d restored=%d captureMoves=%d cancelMoves=%d (want 4/3/2/3/2)", redSellable, redCaptured, redRestored, redCapMoves, redCancelMoves)
	}
	if blueSellable != 0 || blueRequested != 2 || blueCaptured != 1 || blueReserved != 2 {
		t.Fatalf("oracle blue: sellable=%d return_requested=%d captured=%d reserved=%d (want 0/2/1/2)", blueSellable, blueRequested, blueCaptured, blueReserved)
	}
	if blueSellable+blueRequested+blueCaptured != 3 {
		t.Fatalf("oracle blue conservation broken: %d + %d + %d != 3", blueSellable, blueRequested, blueCaptured)
	}
	if departed != 1 || redOrderDeparted != 0 {
		t.Fatalf("oracle departures: shipped=%d cancelled=%d (want 1/0)", departed, redOrderDeparted)
	}
	if productStock != 4 || redStock != 4 || blueStock != 0 {
		t.Fatalf("oracle projections: product=%d red=%d blue=%d (want 4/4/0)", productStock, redStock, blueStock)
	}
	if negatives != 0 || orphanReservations != 0 || originMismatches != 0 {
		t.Fatalf("oracle defects: negatives=%d orphanReservations=%d originMismatches=%d", negatives, orphanReservations, originMismatches)
	}
}

// TestINV12DisposableDBGuardAndAuditFileMapping asserts the disposable-DB
// marker testdb.Open enforces (see the file header): the guard table names
// the database the test is actually connected to.
func TestINV12DisposableDBGuardAndAuditFileMapping(t *testing.T) {
	db := operationsDB(t)
	var actual, marker string
	if err := db.QueryRow(`SELECT current_database(), database_name FROM public.sellersho_test_guard WHERE singleton = TRUE`).Scan(&actual, &marker); err != nil {
		t.Fatalf("disposable-DB guard marker missing: %v", err)
	}
	if actual != marker || marker == "" {
		t.Fatalf("guard marker mismatch: connected=%q marker=%q", actual, marker)
	}
}
