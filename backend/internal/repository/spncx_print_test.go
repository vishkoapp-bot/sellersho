package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func printTestCount(n int) *int { return &n }

func printTestRequest(codes ...string) MarkUnitsPrintedRequest {
	return MarkUnitsPrintedRequest{SupplierID: "s", ProductID: "p", ActorUserID: "printer",
		IdempotencyKey: uuid.NewString(), UnitCodes: codes}
}

func mintPrintTestUnits(t *testing.T, tx *sql.Tx, variant string, n int, refType, refID string) []string {
	t.Helper()
	var lot string
	if err := tx.QueryRow(`SELECT id FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, variant).Scan(&lot); err != nil {
		t.Fatal(err)
	}
	units, err := mintUnitsTx(context.Background(), tx, variant, "p", lot, n, "in_stock", refType, refID, "")
	if err != nil {
		t.Fatal(err)
	}
	codes := make([]string, len(units))
	for i, unit := range units {
		codes[i] = unit.Code
	}
	return codes
}

func printTestUnits(t *testing.T, payload json.RawMessage) []PrintLabelPayload {
	t.Helper()
	var out printedUnitLabels
	if err := json.Unmarshal(payload, &out); err != nil || out.Units == nil {
		t.Fatalf("invalid print envelope: %s (%v)", payload, err)
	}
	return out.Units
}

func requirePrintCodes(t *testing.T, units []PrintLabelPayload, want ...string) {
	t.Helper()
	var codes []string
	for _, unit := range units {
		if unit.Code != unit.BarcodeValue {
			t.Fatalf("barcode=%q, code=%q", unit.BarcodeValue, unit.Code)
		}
		codes = append(codes, unit.Code)
	}
	if !slices.Equal(codes, want) {
		t.Fatalf("codes=%v, want %v", codes, want)
	}
}

func requirePrintRollback(t *testing.T, db *sql.DB, key string, code string, count int) {
	t.Helper()
	var jobs, marks int
	err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM spncx_print_jobs WHERE idempotency_key=$1),
		(SELECT print_count FROM product_units WHERE code=$2)`, key, code).Scan(&jobs, &marks)
	if err != nil || jobs != 0 || marks != count {
		t.Fatalf("after failed print: jobs=%d marks=%d (want %d), err=%v", jobs, marks, count, err)
	}
}

func TestSPNCXPrintMarkReprintAndReplay(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	code := mintPrintTestUnits(t, setup, red, 1, "fixture", "")[0]
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()
	req := printTestRequest(code)
	key := req.IdempotencyKey
	req.IdempotencyKey = "print:" + key
	initial, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	units := printTestUnits(t, initial)
	requirePrintCodes(t, units, code)
	first := units[0]
	if first.PrintCount != 1 || first.PrintedAt == nil || first.LastPrintedAt == nil ||
		first.ProductID != "p" || first.VariantID != red || first.SPNC != "A0C00001" || first.Title != "product" || first.Status != "in_stock" {
		t.Fatalf("first print: %+v", first)
	}
	var job, event, actor string
	var audit, jobUnit, data bool
	err = db.QueryRow(`SELECT j.id,e.event_type,e.actor_user_id,
		e.metadata=jsonb_build_object('idempotency_key',$1::text,'physical_print_unconfirmed',true)
		AND e.from_status='in_stock' AND e.to_status='in_stock' AND e.reference_type='label_print',
		EXISTS(SELECT 1 FROM spncx_print_job_units ju WHERE ju.job_id=j.id AND ju.unit_id=u.id AND ju.code=u.code),
		j.unit_codes=jsonb_build_array(u.code) AND j.payload=$3::jsonb AND j.actor_user_id=$4 AND u.printed_by=$4
		FROM spncx_print_jobs j JOIN product_unit_events e ON e.reference_id=j.id::text
		JOIN product_units u ON u.id=e.unit_id WHERE j.idempotency_key=$1 AND u.code=$2`,
		key, code, string(initial), req.ActorUserID).Scan(&job, &event, &actor, &audit, &jobUnit, &data)
	if err != nil || event != "printed" || actor != req.ActorUserID || !audit || !jobUnit || !data {
		t.Fatalf("print audit: event=%q actor=%q audit=%v association=%v job data=%v err=%v", event, actor, audit, jobUnit, data, err)
	}

	req.IdempotencyKey = key
	again, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil || !bytes.Equal(initial, again) {
		t.Fatalf("prefix-normalized replay: %s (%v)", again, err)
	}

	req.IdempotencyKey = uuid.NewString()
	_, err = MarkUnitsPrintedTx(ctx, db, req)
	var conflict *PrintConflictError
	if !errors.Is(err, ErrPrintAlreadyPrinted) || !errors.As(err, &conflict) ||
		conflict.JobID != job || !slices.Equal(conflict.UnitCodes, []string{code}) {
		t.Fatalf("expected prior job %q and code %q, got %#v (%v)", job, code, conflict, err)
	}
	requirePrintRollback(t, db, req.IdempotencyKey, code, 1)
	req.Reprint = true
	secondPayload, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	units = printTestUnits(t, secondPayload)
	requirePrintCodes(t, units, code)
	second := units[0]
	if second.PrintCount != 2 || second.PrintedAt == nil || !second.PrintedAt.Equal(*first.PrintedAt) ||
		second.LastPrintedAt == nil || !second.LastPrintedAt.After(*first.LastPrintedAt) {
		t.Fatalf("reprint: first %+v second %+v", first, second)
	}
	err = db.QueryRow(`SELECT e.event_type FROM product_unit_events e JOIN spncx_print_jobs j
		ON e.reference_id=j.id::text WHERE j.idempotency_key=$1 AND j.reprint`, req.IdempotencyKey).Scan(&event)
	if err != nil || event != "reprinted" {
		t.Fatalf("reprint audit: %q (%v)", event, err)
	}
}

func TestSPNCXPrintIdempotencyMismatchAndSetReplay(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	codes := mintPrintTestUnits(t, setup, red, 3, "fixture", "")
	if _, err := setup.Exec(`INSERT INTO products(id,supplier_id) VALUES('other','s')`); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()
	req := printTestRequest(codes[1], codes[0])
	initial, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	requirePrintCodes(t, printTestUnits(t, initial), codes[1], codes[0])
	var initialJob string
	if err := db.QueryRow(`SELECT id FROM spncx_print_jobs WHERE idempotency_key=$1`, req.IdempotencyKey).Scan(&initialJob); err != nil {
		t.Fatal(err)
	}
	reordered := req
	reordered.UnitCodes = []string{codes[0], codes[1]}
	again, err := MarkUnitsPrintedTx(ctx, db, reordered)
	if err != nil || !bytes.Equal(initial, again) {
		t.Fatalf("same code set in opposite order: %s (%v)", again, err)
	}
	otherProduct := req
	otherProduct.ProductID = "other"
	otherCodes := req
	otherCodes.UnitCodes = []string{codes[2], codes[1]}
	otherVariant := req
	otherVariant.UnitCodes, otherVariant.VariantID, otherVariant.Count = nil, blue, printTestCount(1)
	for _, test := range []struct {
		name string
		req  MarkUnitsPrintedRequest
	}{{"product", otherProduct}, {"variant", otherVariant}, {"code set", otherCodes}} {
		t.Run(test.name, func(t *testing.T) {
			_, err := MarkUnitsPrintedTx(ctx, db, test.req)
			var conflict *PrintConflictError
			if !errors.Is(err, ErrPrintIdempotencyMismatch) || !errors.As(err, &conflict) || conflict.JobID != initialJob {
				t.Fatalf("expected mismatch for job %q, got %#v (%v)", initialJob, conflict, err)
			}
		})
	}
	var untouched, jobs int
	err = db.QueryRow(`SELECT print_count,(SELECT COUNT(*) FROM spncx_print_jobs)
		FROM product_units WHERE code=$1`, codes[2]).Scan(&untouched, &jobs)
	if err != nil || untouched != 0 || jobs != 1 {
		t.Fatalf("mismatch mutated state: marks=%d jobs=%d err=%v", untouched, jobs, err)
	}
}

func TestSPNCXPrintInvalidUnitsAndRequest(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	codes := mintPrintTestUnits(t, setup, red, 2, "fixture", "")
	if _, err := setup.Exec(`UPDATE product_units SET status='sold' WHERE code=$1`, codes[1]); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()

	for _, reprint := range []bool{false, true} {
		req := printTestRequest(codes[0], codes[1], "unknown")
		req.Reprint = reprint
		_, err := MarkUnitsPrintedTx(ctx, db, req)
		want := []PrintUnitError{
			{Code: codes[1], Error: "unit_not_printable", Status: "sold"},
			{Code: "unknown", Error: "unit_not_found"},
		}
		var invalid *PrintNotPrintableError
		if !errors.As(err, &invalid) || !reflect.DeepEqual(invalid.Units, want) {
			t.Fatalf("reprint=%t expected %+v, got %#v (%v)", reprint, want, invalid, err)
		}
		requirePrintRollback(t, db, req.IdempotencyKey, codes[0], 0)
	}
	for _, test := range []struct {
		name string
		edit func(*MarkUnitsPrintedRequest)
		err  error
	}{
		{"missing key", func(r *MarkUnitsPrintedRequest) { r.IdempotencyKey = "" }, ErrPrintKeyRequired},
		{"bad key", func(r *MarkUnitsPrintedRequest) { r.IdempotencyKey = "wrong" }, ErrPrintInvalidKey},
		{"count without variant", func(r *MarkUnitsPrintedRequest) { r.Count = printTestCount(1) }, ErrPrintVariantRequired},
		{"nonpositive count", func(r *MarkUnitsPrintedRequest) { r.VariantID, r.Count = red, printTestCount(0) }, ErrPrintInvalidCount},
		{"no selection", func(r *MarkUnitsPrintedRequest) { r.UnitCodes = []string{} }, ErrPrintSelectionRequired},
		{"duplicate", func(r *MarkUnitsPrintedRequest) { r.UnitCodes = append(r.UnitCodes, codes[0]) }, ErrPrintDuplicateCodes},
		{"wrong variant", func(r *MarkUnitsPrintedRequest) { r.VariantID, r.Count = blue, printTestCount(1) }, ErrPrintVariantMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := printTestRequest(codes[0])
			test.edit(&req)
			_, err := MarkUnitsPrintedTx(ctx, db, req)
			if !errors.Is(err, test.err) {
				t.Fatalf("want %v, got %v", test.err, err)
			}
			requirePrintRollback(t, db, req.IdempotencyKey, codes[0], 0)
		})
	}
}

func TestSPNCXPrintCountAndEmptyReplay(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	db = skuScopedDB(t, schema)
	ctx := context.Background()
	req := printTestRequest()
	req.Count, req.VariantID, req.UnprintedOnly = printTestCount(1), red, true
	code := formatUnitCode("A0C00001", 1)
	initial, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	requirePrintCodes(t, printTestUnits(t, initial), code)
	req.IdempotencyKey = uuid.NewString()
	empty, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	requirePrintCodes(t, printTestUnits(t, empty))
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	mintPrintTestUnits(t, setup, red, 1, "fixture", "")
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	req.Count = printTestCount(2)
	again, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil || !bytes.Equal(empty, again) {
		t.Fatalf("empty count replay after mint: %s (%v)", again, err)
	}
	req.IdempotencyKey, req.UnprintedOnly, req.Count = uuid.NewString(), false, printTestCount(1)
	if _, err := MarkUnitsPrintedTx(ctx, db, req); !errors.Is(err, ErrPrintAlreadyPrinted) {
		t.Fatalf("count without unprinted filter: %v", err)
	}
	req.Reprint = true
	data, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	requirePrintCodes(t, printTestUnits(t, data), code)
}

func TestSPNCXUnitLabelCursorAndScope(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	codes := mintPrintTestUnits(t, setup, red, 3, "fixture", "")
	if _, err := setup.Exec(`UPDATE product_variants SET created_at=created_at-INTERVAL '1 day' WHERE id=$1`, red); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec(`UPDATE product_units SET status='sold' WHERE code=$1`, codes[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec(`UPDATE products SET spn='PC-0000001' WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	req := printTestRequest(codes[0])
	if _, err := MarkUnitsPrintedTx(ctx, db, req); err != nil {
		t.Fatal(err)
	}
	want := []string{formatUnitCode("A0C00001", 1), codes[0], codes[1],
		formatUnitCode("A0C00002", 1), formatUnitCode("A0C00002", 2), formatUnitCode("A0C00002", 3)}
	opts := UnitLabelListOptions{Limit: 2}
	var seen []string
	for range 3 {
		page, err := repo.ListProductUnitLabels(ctx, "s", "p", opts)
		if err != nil || page.TotalForFilter != len(want) || len(page.Units) != opts.Limit ||
			page.SPN != "PC-0000001" || page.ProductTitle != "product" {
			t.Fatalf("page mismatch: %+v (%v)", page, err)
		}
		for _, row := range page.Units {
			if row.Status != "in_stock" || row.Title != page.ProductTitle || row.SPN != page.SPN {
				t.Fatalf("incorrect default-filter row: %+v", row)
			}
			seen = append(seen, row.Code)
		}
		if page.NextCursor == nil {
			break
		}
		opts.Cursor = *page.NextCursor
		if len(seen) == len(want) {
			t.Fatal("last page has a cursor")
		}
	}
	if !slices.Equal(seen, want) {
		t.Fatalf("cursor traversal: got %v, want %v", seen, want)
	}
	page, err := repo.ListProductUnitLabels(ctx, "s", "p", UnitLabelListOptions{VariantID: red, UnprintedOnly: true})
	if err != nil || page.TotalForFilter != 2 || len(page.Units) != 2 || page.Units[1].Code != codes[1] ||
		page.Units[0].PrintedAt != nil || page.Units[1].PrintedAt != nil || page.NextCursor != nil {
		t.Fatalf("unprinted variant filter: %+v (%v)", page, err)
	}
	page, err = repo.ListProductUnitLabels(ctx, "s", "p", UnitLabelListOptions{Status: "sold"})
	if err != nil || page.TotalForFilter != 1 || len(page.Units) != 1 || page.Units[0].Code != codes[2] {
		t.Fatalf("sold filter: %+v (%v)", page, err)
	}
	if _, err := repo.ListProductUnitLabels(ctx, "other-supplier", "p", UnitLabelListOptions{VariantID: blue}); !errors.Is(err, ErrPrintProductNotFound) {
		t.Fatalf("expected missing scoped product, got %v", err)
	}
	req = printTestRequest(codes[1])
	req.SupplierID = "other-supplier"
	if _, err := MarkUnitsPrintedTx(ctx, db, req); !errors.Is(err, ErrPrintProductNotFound) {
		t.Fatalf("expected missing scoped print product, got %v", err)
	}
	requirePrintRollback(t, db, req.IdempotencyKey, codes[1], 0)
	if _, err := repo.ListProductUnitLabels(ctx, "s", "p", UnitLabelListOptions{VariantID: uuid.NewString()}); !errors.Is(err, ErrPrintVariantNotFound) {
		t.Fatalf("expected missing variant, got %v", err)
	}
}

func TestSPNCXPrintWarehouseReceiptLine(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	line := "print-receipt-line"
	if _, err := setup.Exec(`INSERT INTO inbound_inventory_lines
		(id,shipment_id,supplier_id,warehouse_id,product_id,variant_id,source_kind,external_reference,reason,quantity,dispatched_quantity,received_quantity)
		VALUES($1,'print-test-shipment','s','w1','p',$2,'external_replenishment','print-ref','test fixture',3,3,3),
		('other-line','other-shipment','s','w1','p',$2,'external_replenishment','other-ref','test fixture',1,1,1)`,
		line, red); err != nil {
		t.Fatal(err)
	}
	// Stage the same mint event as receive writes. These units already sit in
	// a sellable lot, as after QC, so the event must supply receipt provenance.
	codes := mintPrintTestUnits(t, setup, red, 3, "inbound_receive", line)
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()
	if _, err := MarkUnitsPrintedTx(ctx, db, printTestRequest(codes[1])); err != nil {
		t.Fatal(err)
	}
	req := MarkUnitsPrintedRequest{ReceiptLineID: &line, IdempotencyKey: uuid.NewString(), ActorUserID: "warehouse-printer"}
	payload, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	requirePrintCodes(t, printTestUnits(t, payload), codes[0], codes[2])
	other := "other-line"
	req.ReceiptLineID = &other
	if _, err := MarkUnitsPrintedTx(ctx, db, req); !errors.Is(err, ErrPrintIdempotencyMismatch) {
		t.Fatalf("receipt-line replay mismatch: %v", err)
	}
	req.ReceiptLineID, req.IdempotencyKey = &line, uuid.NewString()
	empty, err := MarkUnitsPrintedTx(ctx, db, req)
	if err != nil {
		t.Fatal(err)
	}
	requirePrintCodes(t, printTestUnits(t, empty))
	missing := "missing"
	req.ReceiptLineID, req.IdempotencyKey = &missing, uuid.NewString()
	if _, err := MarkUnitsPrintedTx(ctx, db, req); !errors.Is(err, ErrPrintReceiptLineNotFound) {
		t.Fatalf("unknown receipt line: %v", err)
	}
}

type printTestResult struct {
	payload json.RawMessage
	err     error
}

func startPrintTestRequest(ctx context.Context, db *sql.DB, req MarkUnitsPrintedRequest) <-chan printTestResult {
	result := make(chan printTestResult, 1)
	go func() {
		payload, err := MarkUnitsPrintedTx(ctx, db, req)
		result <- printTestResult{payload, err}
	}()
	return result
}

func printTestLockUnit(t *testing.T, tx *sql.Tx, code string) int {
	t.Helper()
	var pid int
	if err := tx.QueryRow(`SELECT pg_backend_pid() FROM product_units WHERE code=$1 FOR UPDATE`, code).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return pid
}

// Wait for real PostgreSQL lock contention before releasing the test barrier.
// With the same key the second print waits on the first, which waits on the unit.
func waitForPrintTestLocks(t *testing.T, ctx context.Context, db *sql.DB, pid, want int) {
	t.Helper()
	for {
		var count int
		err := db.QueryRowContext(ctx, `WITH RECURSIVE blocked(pid) AS (
			SELECT $1::int UNION
			SELECT a.pid FROM pg_stat_activity a JOIN blocked b ON b.pid=ANY(pg_blocking_pids(a.pid))
		) SELECT COUNT(*)-1 FROM blocked`, pid).Scan(&count)
		if err != nil {
			t.Fatalf("waiting for %d blocked print transactions: %v", want, err)
		}
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSPNCXPrintConcurrentCountAndReplay(t *testing.T) {
	for _, test := range []struct {
		name    string
		sameKey bool
	}{{"different keys", false}, {"same key", true}} {
		t.Run(test.name, func(t *testing.T) {
			db, schema, red, _ := skuInventoryFixture(t)
			setup := skuTestTx(t, db, schema)
			defer setup.Rollback()
			codes := append([]string{formatUnitCode("A0C00001", 1)}, mintPrintTestUnits(t, setup, red, 3, "fixture", "")...)
			if err := setup.Commit(); err != nil {
				t.Fatal(err)
			}
			scoped := skuScopedDB(t, schema)
			blocker := skuTestTx(t, db, schema)
			defer blocker.Rollback()
			pid := printTestLockUnit(t, blocker, codes[0])
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			first := printTestRequest()
			first.VariantID, first.Count, first.UnprintedOnly = red, printTestCount(2), true
			second := first
			if !test.sameKey {
				second.IdempotencyKey = uuid.NewString()
			}
			a := startPrintTestRequest(ctx, scoped, first)
			waitForPrintTestLocks(t, ctx, db, pid, 1)
			b := startPrintTestRequest(ctx, scoped, second)
			waitForPrintTestLocks(t, ctx, db, pid, 2)
			if err := blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			aResult, bResult := <-a, <-b
			if aResult.err != nil || bResult.err != nil {
				t.Fatalf("concurrent print errors: first %v, second %v", aResult.err, bResult.err)
			}
			requirePrintCodes(t, printTestUnits(t, aResult.payload), codes[:2]...)
			wantMarked := 4
			if test.sameKey {
				wantMarked = 2
				if !bytes.Equal(aResult.payload, bResult.payload) {
					t.Fatalf("concurrent replay changed payload: %s vs %s", aResult.payload, bResult.payload)
				}
			} else {
				requirePrintCodes(t, printTestUnits(t, bResult.payload), codes[2:]...)
			}
			var marked, marks int
			err := scoped.QueryRow(`SELECT COUNT(*),COALESCE(SUM(print_count),0)
				FROM product_units WHERE variant_id=$1 AND printed_at IS NOT NULL`, red).Scan(&marked, &marks)
			if err != nil || marked != wantMarked || marks != wantMarked {
				t.Fatalf("want %d distinct single marks, got %d units, %d marks (%v)", wantMarked, marked, marks, err)
			}
		})
	}
}

func TestSPNCXPrintConcurrentExplicitConflict(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	scoped := skuScopedDB(t, schema)
	blocker := skuTestTx(t, db, schema)
	defer blocker.Rollback()
	code := formatUnitCode("A0C00001", 1)
	pid := printTestLockUnit(t, blocker, code)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, second := printTestRequest(code), printTestRequest(code)
	a := startPrintTestRequest(ctx, scoped, first)
	waitForPrintTestLocks(t, ctx, db, pid, 1)
	b := startPrintTestRequest(ctx, scoped, second)
	waitForPrintTestLocks(t, ctx, db, pid, 2)
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	aResult, bResult := <-a, <-b
	if aResult.err != nil {
		t.Fatal(aResult.err)
	}
	var conflict *PrintConflictError
	var jobID string
	if err := scoped.QueryRow(`SELECT id FROM spncx_print_jobs WHERE idempotency_key=$1`, first.IdempotencyKey).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(bResult.err, ErrPrintAlreadyPrinted) || !errors.As(bResult.err, &conflict) ||
		conflict.JobID != jobID || !slices.Equal(conflict.UnitCodes, []string{code}) {
		t.Fatalf("concurrent explicit loser should name %s, got %#v (%v)", jobID, conflict, bResult.err)
	}
	requirePrintRollback(t, scoped, second.IdempotencyKey, code, 1)
}

func TestSPNCXPrintRejectsUnitSoldWhileWaitingOnLock(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	scoped := skuScopedDB(t, schema)
	sale := skuTestTx(t, db, schema)
	defer sale.Rollback()
	code := formatUnitCode("A0C00001", 1)
	pid := printTestLockUnit(t, sale, code)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := printTestRequest(code)
	result := startPrintTestRequest(ctx, scoped, req)
	waitForPrintTestLocks(t, ctx, db, pid, 1)
	if _, err := sale.ExecContext(ctx, `UPDATE product_units SET status='sold' WHERE code=$1`, code); err != nil {
		t.Fatal(err)
	}
	if err := sale.Commit(); err != nil {
		t.Fatal(err)
	}
	var invalid *PrintNotPrintableError
	finished := <-result
	want := []PrintUnitError{{Code: code, Error: "unit_not_printable", Status: "sold"}}
	if !errors.As(finished.err, &invalid) || !reflect.DeepEqual(invalid.Units, want) {
		t.Fatalf("post-lock validation should reject sold unit, got %#v (%v)", invalid, finished.err)
	}
	requirePrintRollback(t, scoped, req.IdempotencyKey, code, 0)
}
