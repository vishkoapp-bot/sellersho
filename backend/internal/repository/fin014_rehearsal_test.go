package repository

import (
	"context"
	"os"
	"testing"
)

// FIN-014 W9 — migration-failure + health rehearsal (synthetic
// fixture variant; the staging/production-shaped variant is GATED
// on Q1 + Q7 and is NOT faked here).
//
// R-1 fail before commit: the W4 up file plus a deliberate error in
// one transaction — the transaction aborts and nothing is left
// behind (the three tables are absent), then the real up applies
// cleanly.
// R-2 fail after commit: a duplicate CREATE TABLE fails — the W4
// tables stay intact and the schema_migrations count is unchanged.
// R-3 fail backend health after migration: dropping a table the
// health probe reads makes the probe fail; the rehearsal HALTS (no
// repair operation is created, the fixture report is unchanged) and
// a forward repair (re-creating exactly the missing table +
// trigger, copied from the up file — never editing the applied
// migration) restores health. The repair is forward-only: no
// history rewrite, no DROP of the other tables.
//
// The backup/restore half of W9 (R-4) is a shell rehearsal with its
// evidence under ~/workspace/fin014-audit/ — a Go test cannot create
// a second database (the test role is NOCREATEDB).

// fin014ForwardRepairReportRuns re-creates fin014_report_runs, its
// index and its immutability trigger, plus the foreign key from
// fin014_repair_batches that DROP ... CASCADE removed — all copied
// verbatim from 202611210001_fin014_repair_batches.up.sql (table
// lines 23-38, index line 39, trigger lines 80-82, FK from the
// repair_batches table lines 40-53). It is the forward repair for
// R-3: it restores exactly the missing objects and touches nothing
// else.
const fin014ForwardRepairReportRuns = `
CREATE TABLE fin014_report_runs (
  id TEXT PRIMARY KEY,
  prefix TEXT NOT NULL DEFAULT '',
  cycle_period TEXT NOT NULL DEFAULT '',
  schema_version INT NOT NULL,
  total_rows INT NOT NULL CHECK (total_rows >= 0),
  certain_count INT NOT NULL CHECK (certain_count >= 0),
  ambiguous_count INT NOT NULL CHECK (ambiguous_count >= 0),
  unresolved_count INT NOT NULL CHECK (unresolved_count >= 0),
  oracle_checks JSONB NOT NULL DEFAULT '{}'::jsonb,
  fin011_pinned BOOLEAN NOT NULL DEFAULT TRUE,
  report_json JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX fin014_report_runs_created_at ON fin014_report_runs(created_at);
CREATE TRIGGER fin014_report_run_immutable BEFORE UPDATE OR DELETE ON fin014_report_runs
  FOR EACH ROW EXECUTE FUNCTION fin014_immutable_report_run();
ALTER TABLE fin014_repair_batches
  ADD CONSTRAINT fin014_repair_batches_report_run_id_fkey
  FOREIGN KEY (report_run_id) REFERENCES fin014_report_runs(id) ON DELETE RESTRICT;
`

func TestFIN014MigrationFailureRehearsal(t *testing.T) {
	db := fin014FixtureDB(t)
	ctx := context.Background()
	loadFIN014Fixture(t, db, "f14r9-", "2026-10")
	report := fin014ReportForFixture(t, db, "f14r9-")
	assertFIN014ExpectedCounts(t, report)
	up, err := os.ReadFile("../../migrations/202611210001_fin014_repair_batches.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611210001_fin014_repair_batches.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tables := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('fin014_report_runs','fin014_repair_batches','fin014_repair_operations')`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	migrations := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	healthProbe := func() error {
		if err := db.PingContext(ctx); err != nil {
			return err
		}
		var one int
		if err := db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
			return err
		}
		var runs int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fin014_report_runs`).Scan(&runs); err != nil {
			return err
		}
		return nil
	}
	if tables() != 3 {
		t.Fatalf("fin014 tables present=%d, want 3", tables())
	}
	if healthProbe() != nil {
		t.Fatalf("health probe fails before the rehearsal: %v", healthProbe())
	}
	migrationsBefore := migrations()

	// R-1: fail before commit. Down first, then the up file plus a
	// deliberate error inside one transaction: the whole transaction
	// aborts and nothing is left behind.
	if _, err := db.Exec(string(down)); err != nil {
		t.Fatalf("R-1 down: %v", err)
	}
	if tables() != 0 {
		t.Fatalf("R-1 tables after down=%d, want 0", tables())
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(up)); err != nil {
		tx.Rollback()
		t.Fatalf("R-1 up inside the broken transaction: %v", err)
	}
	if _, err := tx.Exec(`SELECT fin014_deliberate_error_column`); err == nil {
		tx.Rollback()
		t.Fatalf("R-1 deliberate error did not fail")
	}
	if err := tx.Commit(); err == nil {
		t.Fatalf("R-1 broken transaction committed, want abort")
	}
	// The failed commit already rolled the transaction back; an
	// explicit Rollback is a no-op, but it guarantees the pooled
	// connection is clean for the next statement.
	tx.Rollback()
	if tables() != 0 {
		t.Errorf("R-1 tables after failed up=%d, want 0 (failed migration left partial state behind)", tables())
	}
	if _, err := db.Exec(string(up)); err != nil {
		t.Fatalf("R-1 real up after the failure: %v", err)
	}
	if tables() != 3 {
		t.Fatalf("R-1 tables after real up=%d, want 3", tables())
	}
	if migrations() != migrationsBefore {
		t.Errorf("R-1 schema_migrations changed across the rehearsal: before=%d after=%d", migrationsBefore, migrations())
	}

	// R-2: fail after commit. A duplicate CREATE TABLE fails; the
	// W4 tables stay intact and the migration count is unchanged.
	if _, err := db.Exec(`CREATE TABLE fin014_report_runs (id TEXT PRIMARY KEY)`); err == nil {
		t.Fatalf("R-2 duplicate CREATE TABLE applied, want failure")
	}
	if tables() != 3 {
		t.Errorf("R-2 tables after failed migration=%d, want 3 (intact)", tables())
	}
	if migrations() != migrationsBefore {
		t.Errorf("R-2 schema_migrations changed: before=%d after=%d", migrationsBefore, migrations())
	}
	if err := healthProbe(); err != nil {
		t.Fatalf("R-2 health probe fails after the failed migration: %v", err)
	}

	// R-3: fail backend health after migration. Dropping a table the
	// probe reads makes the probe fail; the rehearsal HALTS — no
	// repair operation is created and the fixture report is
	// unchanged — and a forward repair restores health.
	if _, err := db.Exec(`DROP TABLE fin014_report_runs CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := healthProbe(); err == nil {
		t.Fatalf("R-3 health probe passed with fin014_report_runs dropped, want failure")
	} else {
		t.Logf("R-3 health probe fails as expected: %v (rehearsal halts here)", err)
	}
	var ops int
	if err := db.QueryRow(`SELECT count(*) FROM fin014_repair_operations`).Scan(&ops); err != nil || ops != 0 {
		t.Errorf("R-3 repair operations=%d err=%v, want 0 (halt means nothing was applied)", ops, err)
	}
	halted := fin014ReportForFixture(t, db, "f14r9-")
	assertFIN014ExpectedCounts(t, halted)
	if _, err := db.Exec(fin014ForwardRepairReportRuns); err != nil {
		t.Fatalf("R-3 forward repair: %v", err)
	}
	if tables() != 3 {
		t.Fatalf("R-3 tables after forward repair=%d, want 3", tables())
	}
	if err := healthProbe(); err != nil {
		t.Fatalf("R-3 health probe still fails after the forward repair: %v", err)
	}
	if migrations() != migrationsBefore {
		t.Errorf("R-3 schema_migrations changed: before=%d after=%d", migrationsBefore, migrations())
	}
	// The forward repair did not touch the fixture: the report is
	// still the expected 18 rows.
	repaired := fin014ReportForFixture(t, db, "f14r9-")
	assertFIN014ExpectedCounts(t, repaired)
}
