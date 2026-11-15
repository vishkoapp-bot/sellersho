package repository

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func fin006Fixture(t *testing.T) (*sql.DB, opsFixture, string) {
	t.Helper()
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	warehouse := "fee-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'test','test')`, warehouse)
	if err := NewSupplierRepository(db).RecordProductMeasurement(context.Background(), f.product, f.user, 500, 400, 300, 1500, "known fee probe measurement"); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_enabled=TRUE,storage_fee_free_days=30,storage_fee_per_kg_day=2000,storage_fee_per_m3_day=8000 WHERE id=1`)
	// Model a known history-policy installation and verified receipt facts in the
	// past; do not backdate or certify unknown production positions.
	policyStart := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	opsExec(t, db, `UPDATE warehouse_storage_history_policy SET effective_at=$1 WHERE id=1`, policyStart)
	revision := "MEAS-HIST-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO inventory_measurement_revisions(id,product_id,scope,length_mm,width_mm,height_mm,weight_grams,verified,reason,actor_user_id,created_at) VALUES($1,$2,'product',500,400,300,1500,TRUE,'known historical test receipt',$3,$4)`, revision, f.product, f.user, policyStart)
	opsExec(t, db, `UPDATE products SET measurement_revision_id=$2 WHERE id=$1`, f.product, revision)
	return db, f, warehouse
}

func fin006KnownReceipt(t *testing.T, db *sql.DB, f opsFixture, warehouse string, quantity int, billing time.Time, legacy ...bool) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, TransactionRowLock{Table: "suppliers", ID: f.supplier}, TransactionRowLock{Table: "products", ID: f.product}); err != nil {
		t.Fatal(err)
	}
	inbound, line, disposition, movement := "FEE-"+uuid.NewString(), "IL-"+uuid.NewString(), "QC-"+uuid.NewString(), "IM-"+uuid.NewString()
	physical := billing.Add(-time.Hour)
	// Conserved immutable upstream fixture facts simulate receipts in the past;
	// the real shared helpers create the protected accounting source and lot.
	statements := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO inbound_shipments(id,supplier_id,supplier_name,warehouse_id,items_count,cartons_count,status,created_at,received_at) VALUES($1,$2,'known fixture',$3,$4,1,'received',$5,$5)`, []any{inbound, f.supplier, warehouse, quantity, physical}},
		{`INSERT INTO inbound_inventory_lines(id,shipment_id,supplier_id,warehouse_id,product_id,source_kind,external_reference,reason,quantity,dispatched_quantity,received_quantity) VALUES($1,$2,$3,$4,$5,'external_replenishment',$1,'known test receipt',$6,$6,$6)`, []any{line, inbound, f.supplier, warehouse, f.product, quantity}},
		{`INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,created_at) VALUES($1,$2,$3,$4,'external','quarantine','external',$5,'inbound_transfer',$6,$1,$7,$8)`, []any{movement, f.product, f.supplier, quantity, "inbound-line:" + line, line, f.user, physical}},
		{`INSERT INTO inbound_qc_dispositions(id,receipt_line_id,cumulative_disposed,received_quantity,quantity_passed,quantity_failed,quantity_shortage,passed_delta,failed_delta,reason,actor_user_id,created_at) VALUES($1,$2,$3,$3,$3,0,0,$3,0,'known test acceptance',$4,$5)`, []any{disposition, line, quantity, f.user, billing}},
		{`INSERT INTO qc_results(id,product_id,inbound_id,inspector_user_id,status,quantity_passed,quantity_failed,created_at) VALUES($1,$2,$3,$4,'completed',$5,0,$6)`, []any{disposition, f.product, inbound, f.user, quantity, billing}},
	}
	for _, stmt := range statements {
		if _, err = tx.ExecContext(ctx, stmt.q, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	persisted := inboundPersistedLine{ID: line, Shipment: inbound, Supplier: f.supplier, Warehouse: warehouse, Product: f.product, Kind: "external_replenishment", Quantity: quantity, Dispatched: quantity, Received: quantity}
	if err = createInboundPutawayTx(ctx, tx, persisted, disposition, quantity); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,created_at) VALUES($1,$2,$3,$4,'quarantine','qc_accepted',$5,$6,'inbound_qc_disposition',$7,$1,$8,$9)`, "IM-"+uuid.NewString(), f.product, f.supplier, quantity, "inbound-line:"+line, "qc-accepted:"+disposition, disposition, f.user, billing); err != nil {
		t.Fatal(err)
	}
	unknownLegacy := len(legacy) > 0 && legacy[0]
	if !unknownLegacy {
		if err = creditStorageReceiptLotTx(ctx, tx, line, f.product, f.supplier, "", warehouse, quantity, physical, billing); err != nil {
			t.Fatal(err)
		}
	}
	var task, position string
	if err = tx.QueryRowContext(ctx, `SELECT task_id FROM inbound_putaway_allocations WHERE disposition_id=$1`, disposition).Scan(&task); err != nil {
		t.Fatal(err)
	}
	if !unknownLegacy {
		if err = tx.QueryRowContext(ctx, `SELECT id FROM warehouse_storage_receipt_lots WHERE source_event_id=$1`, disposition).Scan(&position); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return task, position
}

func TestFIN006AssessmentKeepsReceiptRateAndMeasurementSnapshot(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	old := tehranDay(time.Now()).AddDate(0, 0, -40)
	fin006KnownReceipt(t, db, f, warehouse, 1, old)
	if err := NewSupplierRepository(db).RecordProductMeasurement(context.Background(), f.product, f.user, 100, 100, 100, 100, "new measurement for future receipts"); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE platform_settings SET storage_fee_per_kg_day=5000 WHERE id=1`)
	summary, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalCharged != 34800 {
		t.Fatalf("receipt was repriced from today's settings/measurement: %d want34800", summary.TotalCharged)
	}
}

func TestFIN006KnownHistoryRejectsUnsourcedQuantityDebtAndCheckpoint(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	_, position := fin006KnownReceipt(t, db, f, warehouse, 1, tehranDay(time.Now()).AddDate(0, 0, -40))
	cases := []struct {
		name, q string
		args    []any
	}{
		{"quantity without physical event", `UPDATE warehouse_storage_positions SET qty=0 WHERE id=$1`, []any{position}},
		{"wrong daily debt", `INSERT INTO warehouse_storage_fee_charges(id,position_id,supplier_id,product_id,warehouse_id,fee_date,qty,fee_unit_day_toman,amount_toman,goods_class) VALUES($1,$2,$3,$4,$5,$6,1,3480,1,'normal')`, []any{"forged-fee-" + uuid.NewString(), position, f.supplier, f.product, warehouse, tehranDay(time.Now()).AddDate(0, 0, -1)}},
		{"checkpoint without recorded daily evidence", `UPDATE warehouse_storage_positions SET last_assessed_date=$2 WHERE id=$1`, []any{position, tehranDay(time.Now()).AddDate(0, 0, -1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err = tx.Exec(c.q, c.args...); err == nil {
				t.Fatal("known storage history accepted an unsupported mutation")
			}
		})
	}
}

func TestFIN006TopUpDoesNotInheritOldReceiptAge(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	old := tehranDay(time.Now()).AddDate(0, 0, -40)
	fin006KnownReceipt(t, db, f, warehouse, 1, old)
	fin006KnownReceipt(t, db, f, warehouse, 9, time.Now().Add(-time.Second))
	summary, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// One known old unit owes10 days. Nine units received today are still free.
	if summary.TotalCharged != 34800 {
		t.Fatalf("mixed-age receipt charged=%d want34800 (old unit10days; fresh nine units free)", summary.TotalCharged)
	}
}

func TestFIN006DepartureRetainsUnpaidCompletedDays(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	old := tehranDay(time.Now()).AddDate(0, 0, -40)
	task, _ := fin006KnownReceipt(t, db, f, warehouse, 1, old)
	ctx := context.Background()
	wh := NewWarehouseRepository(db)
	bin := inv08Bin(t, db, warehouse, 60000, 1500)
	if err := wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	if err := wh.ConfirmPutaway(ctx, task, f.user); err != nil {
		t.Fatal(err)
	}
	order := inv06PaidOrder(t, db, f, warehouse, "")
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err := wh.ShipOrderWithActor(ctx, order, "post", "fee-"+order, f.user); err != nil {
		t.Fatal(err)
	}

	summary, err := NewSupplierRepository(db).RunStorageFeeAssessment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalCharged != 34800 {
		t.Fatalf("departed receipt charged=%d want34800 (completed unpaid days survive closure)", summary.TotalCharged)
	}
}

func TestFIN006AssessmentBoundedBacklogAndTwoWorkers(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	fin006KnownReceipt(t, db, f, wh, 1, tehranDay(time.Now()).AddDate(0, 0, -110))
	repo := NewSupplierRepository(db)
	first, err := repo.RunStorageFeeAssessment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ChargesCreated != 32 || !first.MoreWork || first.TotalCharged != 32*3480 {
		t.Fatalf("unbounded/misreported first pass: %+v", first)
	}
	var wg sync.WaitGroup
	results := make(chan StorageFeeRunSummary, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := repo.RunStorageFeeAssessment(context.Background())
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var created int
	for r := range results {
		created += r.ChargesCreated
	}
	if created != 48 {
		t.Fatalf("two workers duplicated/missed remainder: %d want48", created)
	}
	last, err := repo.RunStorageFeeAssessment(context.Background())
	if err != nil || last.ChargesCreated != 0 || last.MoreWork {
		t.Fatalf("replay %+v %v", last, err)
	}
	var sum, projection int64
	if err = db.QueryRow(`SELECT SUM(amount_toman) FROM warehouse_storage_fee_charges WHERE product_id=$1`, f.product).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT total_fee_charged_toman FROM warehouse_storage_positions WHERE product_id=$1`, f.product).Scan(&projection); err != nil || projection != sum || sum != 80*3480 {
		t.Fatalf("quantity-day integral/projection %d/%d %v", sum, projection, err)
	}
}

func TestFIN006AssessmentFailureRollsBackChargesAndCheckpoint(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	_, position := fin006KnownReceipt(t, db, f, wh, 1, tehranDay(time.Now()).AddDate(0, 0, -40))
	opsExec(t, db, `CREATE FUNCTION test_fail_second_storage_charge() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF EXISTS(SELECT 1 FROM warehouse_storage_fee_charges WHERE position_id=NEW.position_id) THEN RAISE EXCEPTION 'injected interruption'; END IF; RETURN NEW; END $$; CREATE TRIGGER zz_test_storage_failure BEFORE INSERT ON warehouse_storage_fee_charges FOR EACH ROW EXECUTE FUNCTION test_fail_second_storage_charge()`)
	repo := NewSupplierRepository(db)
	if _, err := repo.RunStorageFeeAssessment(context.Background()); err == nil {
		t.Fatal("fault did not abort assessment")
	}
	var intact bool
	if err := db.QueryRow(`SELECT p.qty=1 AND p.last_assessed_date IS NULL AND p.last_fee_date IS NULL AND p.total_fee_charged_toman=0 AND NOT EXISTS(SELECT 1 FROM warehouse_storage_fee_charges WHERE position_id=p.id) AND NOT EXISTS(SELECT 1 FROM warehouse_storage_certified_charges WHERE position_id=p.id) FROM warehouse_storage_positions p WHERE p.id=$1`, position).Scan(&intact); err != nil || !intact {
		t.Fatalf("partial accounting after interruption: %v %v", intact, err)
	}
	opsExec(t, db, `DROP TRIGGER zz_test_storage_failure ON warehouse_storage_fee_charges;DROP FUNCTION test_fail_second_storage_charge()`)
	retry, err := repo.RunStorageFeeAssessment(context.Background())
	if err != nil || retry.TotalCharged != 34800 || retry.ChargesCreated != 10 {
		t.Fatalf("recovery %+v %v", retry, err)
	}
}

func fin006PlaceTask(t *testing.T, db *sql.DB, f opsFixture, wh, task string, q int) string {
	t.Helper()
	bin := inv08Bin(t, db, wh, 60000*q, 1500*q)
	repo := NewWarehouseRepository(db)
	if err := repo.AssignPutawayTask(context.Background(), task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmPutaway(context.Background(), task, f.user); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestFIN006ConcurrentPartialDepartureAndAssessment(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	task, position := fin006KnownReceipt(t, db, f, warehouse, 3, tehranDay(time.Now()).AddDate(0, 0, -40))
	fin006PlaceTask(t, db, f, warehouse, task, 3)
	order := inv06PaidOrder(t, db, f, warehouse, "", 1)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		<-start
		_, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background())
		done <- err
	}()
	go func() {
		<-start
		done <- NewWarehouseRepository(db).ShipOrderWithActor(context.Background(), order, "post", "parallel-fee-"+order, f.user)
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var quantity, departures, charged int
	if err := db.QueryRow(`SELECT qty,(SELECT SUM(quantity) FROM warehouse_storage_lot_departures WHERE position_id=p.id),total_fee_charged_toman FROM warehouse_storage_positions p WHERE id=$1`, position).Scan(&quantity, &departures, &charged); err != nil || quantity != 2 || departures != 1 || charged != 3*34800 {
		t.Fatalf("partial departure/accrual %d/%d/%d %v", quantity, departures, charged, err)
	}
	replay, err := NewSupplierRepository(db).RunStorageFeeAssessment(context.Background())
	if err != nil || replay.TotalCharged != 0 {
		t.Fatalf("concurrent replay %+v %v", replay, err)
	}
}

func TestFIN006AssessmentDiscoveryRequiresIndexedFrontier(t *testing.T) {
	db, _, _ := fin006Fixture(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SET LOCAL enable_seqscan=off;SET LOCAL enable_bitmapscan=off`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err = tx.QueryRow(`EXPLAIN (FORMAT JSON) `+storageAssessmentDiscoverySQL, tehranDay(time.Now()).AddDate(0, 0, -1), 101).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan, `"Node Type": "Sort"`) || strings.Contains(plan, `"Node Type": "Seq Scan"`) {
		t.Fatalf("bounded discovery requires an indexed frontier, no full-history sort/scan: %s", plan)
	}
}

func TestFIN006FrontierAdvancesPastUnresolvedHistory(t *testing.T) {
	db, f, wh := fin006Fixture(t)
	fin006KnownReceipt(t, db, f, wh, 1, tehranDay(time.Now()).AddDate(0, 0, -40))
	opsExec(t, db, `UPDATE products SET measurement_revision_id=NULL,length_mm=NULL,width_mm=NULL,height_mm=NULL,weight_grams=NULL WHERE id=$1`, f.product)
	for i := 0; i < 101; i++ {
		fin006KnownReceipt(t, db, f, wh, 1, time.Now().Add(-time.Second))
	}
	var total int64
	var examined int
	repo := NewSupplierRepository(db)
	for i := 0; i < 3; i++ {
		r, err := repo.RunStorageFeeAssessment(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if r.ReceiptsExamined > 100 {
			t.Fatalf("unbounded frontier %+v", r)
		}
		total += r.TotalCharged
		examined += r.ReceiptsExamined
		if !r.UnresolvedSampleLimited || r.PositionsSkippedNoMeasurement != 100 {
			t.Fatalf("unresolved sample limit not reported %+v", r)
		}
		if !r.MoreWork {
			break
		}
		if i == 2 {
			t.Fatal("frontier never finished one bounded cycle")
		}
	}
	if total != 34800 || examined != 102 {
		t.Fatalf("blocked history starved or duplicated valid accrual: amount=%d examined=%d", total, examined)
	}
	var frontier string
	if err := db.QueryRow(`SELECT last_receipt_id FROM warehouse_storage_assessment_cursor WHERE id=1`).Scan(&frontier); err != nil || frontier != "" {
		t.Fatalf("completed cycle failed to reset %q %v", frontier, err)
	}
}

func TestFIN006LaterUnknownBinHistoryCannotEraseClosedAccrual(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	task, position := fin006KnownReceipt(t, db, f, warehouse, 1, tehranDay(time.Now()).AddDate(0, 0, -40))
	bin := fin006PlaceTask(t, db, f, warehouse, task, 1)
	ctx := context.Background()
	wh := NewWarehouseRepository(db)
	ship := func() {
		order := inv06PaidOrder(t, db, f, warehouse, "", 1)
		opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
		if err := wh.ShipOrderWithActor(ctx, order, "post", "closed-accrual-"+order, f.user); err != nil {
			t.Fatal(err)
		}
	}
	ship()
	// A pre-cutover accepted staging task may be shelved after this known lot
	// has departed. Its unknown age must not invalidate an unrelated past debt.
	oldTask, _ := fin006KnownReceipt(t, db, f, warehouse, 1, time.Date(2019, 12, 1, 0, 0, 0, 0, time.UTC), true)
	if err := wh.AssignPutawayTask(ctx, oldTask, bin, f.user); err != nil {
		t.Fatal(err)
	}
	if err := wh.ConfirmPutaway(ctx, oldTask, f.user); err != nil {
		t.Fatal(err)
	}
	ship()
	var exceptions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_storage_history_exceptions WHERE product_id=$1`, f.product).Scan(&exceptions); err != nil || exceptions != 1 {
		t.Fatalf("unresolved source evidence=%d %v", exceptions, err)
	}
	result, err := NewSupplierRepository(db).RunStorageFeeAssessment(ctx)
	if err != nil || result.TotalCharged != 34800 {
		t.Fatalf("later unknown custody erased completed closed accrual: %+v %v", result, err)
	}
	var status string
	if err = db.QueryRow(`SELECT history_status FROM warehouse_storage_history_audit WHERE position_id=$1`, position).Scan(&status); err != nil || status != "verified" {
		t.Fatalf("unrelated old receipt blocked: %s %v", status, err)
	}
}

func TestFIN006SQLCannotBackdateHistoryException(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	task, _ := fin006KnownReceipt(t, db, f, warehouse, 1, time.Date(2019, 12, 1, 0, 0, 0, 0, time.UTC), true)
	fin006PlaceTask(t, db, f, warehouse, task, 1)
	order := inv06PaidOrder(t, db, f, warehouse, "", 1)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err := NewWarehouseRepository(db).ShipOrderWithActor(context.Background(), order, "post", "legacy-history-"+order, f.user); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var inserted time.Time
	before := time.Now().Add(-time.Second)
	err = tx.QueryRow(`INSERT INTO warehouse_storage_history_exceptions(id,event_type,event_id,source_balance_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,location_type,location_key,quantity,reason,actor_user_id,created_at) SELECT $1,'warehouse_shipment',$2,balance_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,location_type,location_key,quantity,'legacy counted source',$3,'2000-01-01' FROM expected_storage_departure_sources('warehouse_shipment',$2) RETURNING created_at`, "SHE-test-"+uuid.NewString(), order, f.user).Scan(&inserted)
	if err != nil {
		t.Fatal(err)
	}
	if inserted.Before(before) {
		t.Fatalf("SQL invented a backdated exception %s", inserted)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestFIN006IncompleteActiveSourceKeepsAccrualPending(t *testing.T) {
	db, f, warehouse := fin006Fixture(t)
	task, position := fin006KnownReceipt(t, db, f, warehouse, 2, tehranDay(time.Now()).AddDate(0, 0, -40))
	bin := fin006PlaceTask(t, db, f, warehouse, task, 2)
	opsExec(t, db, `UPDATE warehouse_bins SET capacity_volume_cm3=180000,max_weight_grams=4500 WHERE bin_code=$1`, bin)
	ctx := context.Background()
	wh := NewWarehouseRepository(db)
	ship := func(q int) {
		order := inv06PaidOrder(t, db, f, warehouse, "", q)
		opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
		if err := wh.ShipOrderWithActor(ctx, order, "post", "pending-source-"+order, f.user); err != nil {
			t.Fatal(err)
		}
	}
	ship(1)
	oldTask, _ := fin006KnownReceipt(t, db, f, warehouse, 1, time.Date(2019, 12, 1, 0, 0, 0, 0, time.UTC), true)
	if err := wh.AssignPutawayTask(ctx, oldTask, bin, f.user); err != nil {
		t.Fatal(err)
	}
	if err := wh.ConfirmPutaway(ctx, oldTask, f.user); err != nil {
		t.Fatal(err)
	}
	ship(2)
	result, err := NewSupplierRepository(db).RunStorageFeeAssessment(ctx)
	if err != nil || result.ChargesCreated != 0 || result.TotalCharged != 0 {
		t.Fatalf("uncertain active history invented debt: %+v %v", result, err)
	}
	var preserved bool
	if err = db.QueryRow(`SELECT p.qty=1 AND p.last_assessed_date IS NULL AND p.last_fee_date IS NULL AND p.closed_at IS NULL AND (SELECT SUM(quantity) FROM warehouse_storage_lot_departures WHERE position_id=p.id)=1 AND storage_receipt_is_blocked(p.id) FROM warehouse_storage_positions p WHERE p.id=$1`, position).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("incomplete source guessed units or discarded pending days: %v %v", preserved, err)
	}
}
