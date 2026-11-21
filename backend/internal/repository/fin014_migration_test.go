package repository

import (
	"os"
	"testing"
)

// FIN-014 W4 — repair-batch schema (audit §3.3).
//
// The disposable runner applies all migrations (including
// 202611210001) before this test runs, so the test rehearses the
// down/up cycle on the live tables: down, up, insert one run + batch
// + operation, prove the immutability triggers, down, up again, and
// prove the post-condition (exactly the three tables, empty).
//
// The MIG-02 manifest half of the gate is verified outside this test
// (check_classification.py + compute_last_compatible): the entry is
// EXPAND/compatible with moves_floor=false, and the floor recomputes
// to ff57367 — see the W4 evidence in ~/workspace/fin014-audit/.

func TestFIN014Migration(t *testing.T) {
	up, err := os.ReadFile("../../migrations/202611210001_fin014_repair_batches.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611210001_fin014_repair_batches.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	db := fin014FixtureDB(t)
	tables := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('fin014_report_runs','fin014_repair_batches','fin014_repair_operations')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if tables() != 3 {
		t.Fatalf("fin014 tables present=%d, want 3 (migration not applied by the runner?)", tables())
	}
	// Down: tables gone. Up: tables back, empty.
	if _, err := db.Exec(string(down)); err != nil {
		t.Fatalf("fin014 down: %v", err)
	}
	if tables() != 0 {
		t.Fatalf("fin014 tables after down=%d, want 0", tables())
	}
	if _, err := db.Exec(string(up)); err != nil {
		t.Fatalf("fin014 up: %v", err)
	}
	if tables() != 3 {
		t.Fatalf("fin014 tables after up=%d, want 3", tables())
	}
	// One run + batch + operation, then the immutability triggers.
	if _, err := db.Exec(`INSERT INTO fin014_report_runs(id, prefix, cycle_period, schema_version, total_rows, certain_count, ambiguous_count, unresolved_count, oracle_checks, fin011_pinned, report_json) VALUES('mig-run-1','f14m-','2026-10',213,0,0,0,0,'{}',true,'{}')`); err != nil {
		t.Fatalf("fin014 seed run: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO fin014_repair_batches(id, report_run_id, status, created_by) VALUES('mig-batch-1','mig-run-1','proposed','fin014-fixture')`); err != nil {
		t.Fatalf("fin014 seed batch: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO fin014_repair_operations(id, batch_id, class, record_id, record_kind, effect, expected_before, explanation, cash_basis, confidence) VALUES('mig-op-1','mig-batch-1','coupon_oversubscription','mig-coupon-1','coupon','{}','{}','mig fixture','unknown','certain')`); err != nil {
		t.Fatalf("fin014 seed operation: %v", err)
	}
	// Report runs are append-only evidence: any UPDATE/DELETE fails.
	if _, err := db.Exec(`UPDATE fin014_report_runs SET total_rows=1 WHERE id='mig-run-1'`); err == nil {
		t.Errorf("report run UPDATE accepted, want immutable")
	}
	if _, err := db.Exec(`DELETE FROM fin014_report_runs WHERE id='mig-run-1'`); err == nil {
		t.Errorf("report run DELETE accepted, want immutable")
	}
	// Operation proposal content is frozen: changing the effect fails,
	// but a lifecycle change (proposed -> held) is allowed.
	if _, err := db.Exec(`UPDATE fin014_repair_operations SET effect='{"x":1}' WHERE id='mig-op-1'`); err == nil {
		t.Errorf("operation effect UPDATE accepted, want frozen proposal content")
	}
	if _, err := db.Exec(`UPDATE fin014_repair_operations SET status='held', hold_reason='mig fixture hold' WHERE id='mig-op-1'`); err != nil {
		t.Fatalf("operation hold: %v", err)
	}
	// A batch becomes immutable once applied; an operation in an
	// applied batch is immutable too.
	if _, err := db.Exec(`UPDATE fin014_repair_batches SET status='approved', approved_by='mig-approver', approved_at=NOW() WHERE id='mig-batch-1'`); err != nil {
		t.Fatalf("batch approve: %v", err)
	}
	if _, err := db.Exec(`UPDATE fin014_repair_batches SET status='applied', applied_at=NOW() WHERE id='mig-batch-1'`); err != nil {
		t.Fatalf("batch apply: %v", err)
	}
	if _, err := db.Exec(`UPDATE fin014_repair_batches SET status='failed' WHERE id='mig-batch-1'`); err == nil {
		t.Errorf("applied batch UPDATE accepted, want immutable once applied")
	}
	if _, err := db.Exec(`UPDATE fin014_repair_operations SET status='applied', applied_at=NOW() WHERE id='mig-op-1'`); err == nil {
		t.Errorf("operation in applied batch UPDATE accepted, want immutable")
	}
	// Down/up again: the tables come back empty (no history carried).
	if _, err := db.Exec(string(down)); err != nil {
		t.Fatalf("fin014 down (2nd): %v", err)
	}
	if _, err := db.Exec(string(up)); err != nil {
		t.Fatalf("fin014 up (2nd): %v", err)
	}
	if tables() != 3 {
		t.Fatalf("fin014 tables after 2nd up=%d, want 3", tables())
	}
	var runs, batches, ops int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM fin014_report_runs), (SELECT count(*) FROM fin014_repair_batches), (SELECT count(*) FROM fin014_repair_operations)`).Scan(&runs, &batches, &ops); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || batches != 0 || ops != 0 {
		t.Errorf("fin014 tables after down/up not empty: runs=%d batches=%d ops=%d, want 0/0/0", runs, batches, ops)
	}
}
