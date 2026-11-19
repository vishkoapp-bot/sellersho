package repository

import (
	"context"
	"github.com/drup/platform/internal/migrations"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFIN005MigrationDrainAndQueuedOldWriter(t *testing.T) {
	db := notifyWritersTestDB(t)
	customer, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "drain-order", "DRAIN", customer, "shipped", "paid")
	// Install prerequisite reversal relation only, before the tested fence migration.
	for _, q := range []string{`ALTER TABLE order_item_origins ADD COLUMN IF NOT EXISTS order_id TEXT,ADD COLUMN IF NOT EXISTS quantity INT`, `CREATE TABLE return_items(id UUID  DEFAULT gen_random_uuid() PRIMARY KEY,return_id UUID REFERENCES customer_returns(id),order_item_id TEXT,qty INT,qc_status TEXT DEFAULT 'pending'::character varying ,received_at TIMESTAMPTZ,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			variant_id uuid)`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile("../../migrations/202611050006_customer_return_financial_events.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(b)); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/202611160002_dispute_eligibility.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Rollback()
	if _, err = old.ExecContext(ctx, `UPDATE orders SET customer_full_name='old writer' WHERE id='drain-order'`); err != nil {
		t.Fatal(err)
	}
	attempt, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Rollback()
	if _, err = attempt.ExecContext(ctx, string(migration)); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("contended migration did not abort atomically %v", err)
	}
	attempt.Rollback()
	var exists bool
	if err = db.QueryRowContext(ctx, `SELECT to_regclass('dispute_order_evidence') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("partial schema installed %v %v", exists, err)
	}
	if err = old.Commit(); err != nil {
		t.Fatal(err)
	}
	// A discovery reader may acquire a child relation before financial locks.
	// NOWAIT must abort the whole drain rather than retain earlier parent locks.
	reader, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	var returns int
	if err = reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM customer_returns`).Scan(&returns); err != nil {
		t.Fatal(err)
	}
	readerAttempt, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer readerAttempt.Rollback()
	if _, err = readerAttempt.ExecContext(ctx, string(migration)); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("discovery reader did not abort drain: %v", err)
	}
	readerAttempt.Rollback()
	if _, err = db.ExecContext(ctx, `UPDATE orders SET customer_full_name='old writer' WHERE id='drain-order'`); err != nil {
		t.Fatalf("aborted drain retained parent lock: %v", err)
	}
	if err = reader.Commit(); err != nil {
		t.Fatal(err)
	}
	preBegun, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer preBegun.Rollback()
	queued, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer queued.Rollback()
	var queuedPID int
	if err = queued.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&queuedPID); err != nil {
		t.Fatal(err)
	}
	var ordersRelation int64
	if err = db.QueryRowContext(ctx, `SELECT 'orders'::regclass::oid::bigint`).Scan(&ordersRelation); err != nil {
		t.Fatal(err)
	}

	install, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = install.ExecContext(ctx, string(migration)); err != nil {
		install.Rollback()
		t.Fatal(err)
	}
	defer install.Rollback()
	queuedResult := make(chan error, 1)
	go func() {
		_, e := queued.ExecContext(ctx, `UPDATE orders SET profit_released=TRUE WHERE id='drain-order'`)
		queuedResult <- e
	}()
	for {
		var waiting bool
		if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND relation=$2::oid AND locktype='relation' AND NOT granted)`, queuedPID, ordersRelation).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case e := <-queuedResult:
			t.Fatalf("old statement completed without the required migration relation-lock wait: %v", e)
		default:
		}
		if ctx.Err() != nil {
			t.Fatal("old statement never reached migration relation-lock barrier:", ctx.Err())
		}
	}
	if err = install.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-queuedResult; err == nil || !strings.Contains(err.Error(), "FIN005-writer-required") {
		t.Fatalf("queued old statement bypassed installed fence %v", err)
	}
	queued.Rollback()
	// Preserve the separate BEGIN-before-install/first-write-after-commit case.
	if _, err = preBegun.ExecContext(ctx, `UPDATE orders SET referral_commission_released=TRUE WHERE id='drain-order'`); err == nil || !strings.Contains(err.Error(), "FIN005-writer-required") {
		t.Fatalf("pre-begun idle transaction bypassed fence: %v", err)
	}
	preBegun.Rollback()
	var unchanged bool
	if err = db.QueryRowContext(ctx, `SELECT status='shipped' AND NOT profit_released AND NOT referral_commission_released AND NOT EXISTS(SELECT 1 FROM disputes WHERE order_id='drain-order') AND NOT EXISTS(SELECT 1 FROM ledger_entries WHERE related_order_or_dispute='drain-order') AND NOT EXISTS(SELECT 1 FROM dispute_order_evidence WHERE order_id='drain-order') AND NOT EXISTS(SELECT 1 FROM dispute_financial_decisions WHERE order_id='drain-order') AND NOT EXISTS(SELECT 1 FROM dispute_order_transitions WHERE order_id='drain-order') FROM orders WHERE id='drain-order'`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("queued old statement partially changed status/money/evidence: %v %v", unchanged, err)
	}
	// Local opt-in cannot substitute for durable admission evidence.
	marked, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer marked.Rollback()
	if err = markDisputeWriterTx(ctx, marked); err != nil {
		t.Fatal(err)
	}
	if _, err = marked.ExecContext(ctx, `INSERT INTO disputes(id,order_id,status,claim_amount) VALUES('missing-evidence','drain-order','pending',100)`); err != nil {
		t.Fatal(err)
	}
	if err = marked.Commit(); err == nil || !strings.Contains(err.Error(), "admission evidence") {
		t.Fatalf("missing evidence committed %v", err)
	}
	// Simulated old-binary rollback after installation still fails closed.
	if _, err = db.ExecContext(ctx, `UPDATE orders SET referral_commission_released=TRUE WHERE id='drain-order'`); err == nil {
		t.Fatal("old release accepted after application rollback")
	}
	if _, err = db.ExecContext(ctx, `UPDATE orders SET customer_full_name='unrelated compatible writer' WHERE id='drain-order'`); err != nil {
		t.Fatal(err)
	}
}
func TestFIN005PopulatedDownRefusesEvidenceLoss(t *testing.T) {
	repo, _, _ := fin005Admitted(t)
	b, err := os.ReadFile("../../migrations/202611160002_dispute_eligibility.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repo.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(b)); err == nil || !strings.Contains(err.Error(), "populated evidence") {
		t.Fatalf("down discarded evidence %v", err)
	}
}

func TestFIN005MigrationRunnerSecondApplication(t *testing.T) {
	db := operationsDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		report, err := migrations.Run(ctx, db, migrations.Options{Dir: "../../migrations", LockTimeout: time.Second, MigrationTimeout: 5 * time.Second})
		if err != nil || len(report.Applied) != 0 {
			t.Fatalf("repeat migration run %d applied=%v %v", i, report.Applied, err)
		}
	}
}
func TestFIN005MigrationPreservesProductionShapedLegacyRows(t *testing.T) {
	db := notifyWritersTestDB(t)
	customer, _, _, _, _, _, _, _ := seedNotifyOrderWorld(t, db)
	seedNotifyOrder(t, db, "legacy-order", "LEGACY", customer, "delivered_confirmed", "paid")
	opsExec(t, db, `UPDATE orders SET confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 day',dropshipper_profit=7000 WHERE id='legacy-order'`)
	opsExec(t, db, `INSERT INTO disputes(id,order_id,order_number,type,status,resolution_action,responsible_party,claim_amount) VALUES('legacy-resolved','legacy-order','LEGACY','delivery_undelivered','resolved','release_to_seller','seller',1000)`)
	for _, status := range []string{"requested", "refunded", "cancelled", "cancelled"} {
		opsExec(t, db, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason) VALUES($1,'legacy-order',$2,$3,'historical')`, uuid.NewString(), customer, status)
	}
	snapshot := func() string {
		var raw string
		if err := db.QueryRow(`SELECT jsonb_build_object('orders',(SELECT jsonb_agg(jsonb_build_object('id',id,'status',status,'payable',payable_amount,'profit',dropshipper_profit,'released',profit_released,'confirmed',confirmed_at,'window',return_window_ends_at)) FROM orders),'disputes',(SELECT jsonb_agg(jsonb_build_object('id',id,'status',status,'claim',claim_amount,'action',resolution_action,'party',responsible_party)) FROM disputes),'returns',(SELECT jsonb_agg(jsonb_build_object('id',id,'status',status) ORDER BY id) FROM customer_returns),'ledger',(SELECT COUNT(*) FROM ledger_entries))::text`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	installFIN005PrivateFixture(t, db)
	after := snapshot()
	if before != after {
		t.Fatalf("migration rewrote history before=%s after=%s", before, after)
	}
	var n int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM dispute_order_evidence)+(SELECT COUNT(*) FROM dispute_financial_decisions)+(SELECT COUNT(*) FROM dispute_order_transitions)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("migration inferred financial facts %d %v", n, err)
	}
}
