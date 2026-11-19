package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/pricing"
)

func monthlyLegacyFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := openFinanceBalanceDB(t)
	db.SetMaxOpenConns(6)
	if _, err := db.Exec(`ALTER TABLE stores ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT 'synthetic store'`); err != nil {
		t.Fatal(err)
	}
	legacy, err := os.ReadFile("../../migrations/202611020002_monthly_settlement.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(legacy)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE platform_settings SET settlement_day=10`); err != nil {
		t.Fatal(err)
	}
	return db
}

func applyMonthlyProgress(db *sql.DB) error {
	data, err := os.ReadFile("../../migrations/202611150006_monthly_settlement_progress.up.sql")
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

func monthlySettlementFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := monthlyLegacyFixture(t)
	if err := applyMonthlyProgress(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestFIN002MigrationPreservesHistoryAndRejectsUnsafeEffects(t *testing.T) {
	for _, kind := range []string{"valid_unknown_transfer", "missing_unique", "duplicate", "mismatched_link"} {
		t.Run(kind, func(t *testing.T) {
			db := monthlyLegacyFixture(t)
			seedFinanceBalance(t, db, "legacy", 2000000)
			for _, q := range []string{
				`INSERT INTO monthly_settlement_cycles(period,settlement_day,status,total_stores,total_gross,total_net) VALUES('2026-10',10,'completed',1,2000000,2000000)`,
				`INSERT INTO payout_requests(id,store_id,amount,status,shaba,bank_name) VALUES('legacy-payout','legacy',2000000,'processing','synthetic-bank','synthetic-bank')`,
				`INSERT INTO monthly_settlement_items(id,cycle_period,store_id,gross_amount,fee_amount,net_amount,payout_request_id,status) VALUES('legacy-item','2026-10','legacy',2000000,0,2000000,'legacy-payout','pending_payout')`,
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "duplicate" || kind == "missing_unique" {
				if _, err := db.Exec(`ALTER TABLE monthly_settlement_items DROP CONSTRAINT monthly_settlement_items_cycle_period_store_id_key`); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "duplicate" {
				if _, err := db.Exec(`INSERT INTO monthly_settlement_items(id,cycle_period,store_id,gross_amount,fee_amount,net_amount,status) VALUES('duplicate-item','2026-10','legacy',2000000,0,2000000,'skipped_no_bank_details')`); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "mismatched_link" {
				if _, err := db.Exec(`UPDATE payout_requests SET amount=3000000 WHERE id='legacy-payout'`); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func() string {
				var out string
				if err := db.QueryRow(`SELECT json_build_object('cycle',(SELECT row_to_json(c) FROM (SELECT period,status,total_stores,total_gross,total_net FROM monthly_settlement_cycles WHERE period='2026-10') c),
 'items',(SELECT json_agg(i ORDER BY id) FROM monthly_settlement_items i),'payouts',(SELECT json_agg(p ORDER BY id) FROM payout_requests p))::text`).Scan(&out); err != nil {
					t.Fatal(err)
				}
				return out
			}
			before := snapshot()
			err := applyMonthlyProgress(db)
			if kind == "duplicate" || kind == "mismatched_link" {
				if err == nil || !strings.Contains(err.Error(), "reconciliation required") {
					t.Fatalf("unsafe historical effects adopted: %v", err)
				}
				var added int
				if err := db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='monthly_settlement_cycles' AND column_name='progress_initialized'`).Scan(&added); err != nil || added != 0 {
					t.Fatalf("failed migration changed schema: %d %v", added, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				c, err := NewFinanceRepository(db).RunMonthlySettlementForPeriod(context.Background(), "2026-10", "retry")
				if err != nil || !c.AlreadyCompleted || c.ProgressKnown {
					t.Fatalf("sealed unknown transfer rearmed: %+v %v", c, err)
				}
				var work int
				if err := db.QueryRow(`SELECT count(*) FROM monthly_settlement_progress`).Scan(&work); err != nil || work != 0 {
					t.Fatalf("invented historical work: %d %v", work, err)
				}
				_, duplicateErr := db.Exec(`INSERT INTO monthly_settlement_items(id,cycle_period,store_id,gross_amount,fee_amount,net_amount,status) VALUES('forbidden-replay','2026-10','legacy',2000000,0,2000000,'skipped_no_bank_details')`)
				var constraint interface{ SQLState() string }
				if !errors.As(duplicateErr, &constraint) || constraint.SQLState() != "23505" {
					t.Fatalf("period/store effect not unique: %v", duplicateErr)
				}
			}
			if after := snapshot(); after != before {
				t.Fatal("migration/retry rewrote accepted historical money/effects")
			}
		})
	}
}

func monthlyBankHistory(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO payout_requests(id,store_id,amount,status,shaba,bank_name) VALUES($1,$2,1,'rejected','synthetic-bank','synthetic-bank')`, "bank-"+id, id); err != nil {
		t.Fatal(err)
	}
}

func TestFIN002SharedMonthlyListPreservesAllLegacyHistory(t *testing.T) {
	db := monthlySettlementFixture(t)
	if _, err := db.Exec(`INSERT INTO monthly_settlement_cycles(period,settlement_day,status)
 SELECT to_char('2024-01-01'::date+n*INTERVAL '1 month','YYYY-MM'),10,'completed' FROM generate_series(0,27) n`); err != nil {
		t.Fatal(err)
	}
	cycles, err := NewFinanceRepository(db).ListMonthlyCycles(context.Background())
	if err != nil || len(cycles) != 28 {
		t.Fatalf("shared handler projection truncates existing monthly history: %d %v", len(cycles), err)
	}
}

func TestFIN002CycleAggregateRemainsTransportSafe(t *testing.T) {
	db := monthlySettlementFixture(t)
	if _, err := db.Exec(`INSERT INTO monthly_settlement_cycles(period,settlement_day,status,total_stores,total_gross,total_net) VALUES('2026-10',10,'completed',1,$1,$1)`, pricing.MaxTransportMoney+1); err != nil {
		t.Fatal(err)
	}
	r := NewFinanceRepository(db)
	if _, err := r.GetMonthlySettlementCycle(context.Background(), "2026-10"); !errors.Is(err, pricing.ErrInvalidMoney) {
		t.Fatalf("unsafe financial total exposed to JSON: %v", err)
	}
	if _, err := r.ListMonthlyCycles(context.Background()); !errors.Is(err, pricing.ErrInvalidMoney) {
		t.Fatalf("list rounded/accepted unsafe total: %v", err)
	}
	var exact int64
	if err := db.QueryRow(`SELECT total_gross FROM monthly_settlement_cycles WHERE period='2026-10'`).Scan(&exact); err != nil || exact != pricing.MaxTransportMoney+1 {
		t.Fatalf("read validation rewrote historical amount %d %v", exact, err)
	}
}

func TestFIN002InvalidAggregateYieldsItsFairTurn(t *testing.T) {
	db := monthlySettlementFixture(t)
	seedFinanceBalance(t, db, "representable-new", 2000000)
	monthlyBankHistory(t, db, "representable-new")
	if _, err := db.Exec(`INSERT INTO monthly_settlement_cycles(period,settlement_day,status,total_stores,total_gross,total_net) VALUES('2026-10',10,'blocked',1,$1,$1)`, pricing.MaxTransportMoney+1); err != nil {
		t.Fatal(err)
	}
	r := NewFinanceRepository(db)
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	if _, err := r.RunMonthlySettlement(context.Background(), now, "first"); !errors.Is(err, pricing.ErrInvalidMoney) {
		t.Fatalf("invalid historical amount guessed: %v", err)
	}
	c, err := r.RunMonthlySettlement(context.Background(), now, "next-tick")
	if err != nil || c.Period != "2026-11" || c.Status != "completed" {
		t.Fatalf("invalid old aggregate starved unrelated period %+v %v", c, err)
	}
}

func TestFIN002DetailKeepsTotalsAndItemsInOneSnapshot(t *testing.T) {
	db := monthlySettlementFixture(t)
	for i := 0; i < 31; i++ {
		seedFinanceBalance(t, db, fmt.Sprintf("snapshot-%02d", i), 2000000)
	}
	r := NewFinanceRepository(db)
	ctx := context.Background()
	if _, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "first"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "concurrent"); done <- err }()
	for i := 0; i < 30; i++ {
		cycle, items, err := r.GetMonthlySettlementDetail(ctx, "2026-11")
		if err != nil {
			t.Fatal(err)
		}
		var gross, net int64
		for _, item := range items {
			gross += item.GrossAmount
			net += item.NetAmount
		}
		if cycle.TotalStores != len(items) || cycle.TotalGross != gross || cycle.TotalNet != net {
			t.Fatalf("mixed financial snapshots: cycle=%+v items=%d gross=%d net=%d", cycle, len(items), gross, net)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFIN002UnknownOlderProgressCannotStarveNewCycle(t *testing.T) {
	db := monthlySettlementFixture(t)
	seedFinanceBalance(t, db, "next-month", 2000000)
	monthlyBankHistory(t, db, "next-month")
	if _, err := db.Exec(`INSERT INTO monthly_settlement_cycles(period,settlement_day,status) VALUES('2026-10',10,'blocked')`); err != nil {
		t.Fatal(err)
	}
	r := NewFinanceRepository(db)
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	if _, err := r.RunMonthlySettlement(context.Background(), now, "first"); !errors.Is(err, ErrSettlementReconciliation) {
		t.Fatalf("unknown legacy work guessed: %v", err)
	}
	c, err := r.RunMonthlySettlement(context.Background(), now, "next-tick")
	if err != nil || c.Period != "2026-11" || c.Status != "completed" || c.TotalGross != 2000000 {
		t.Fatalf("older unknown cycle starved new period: %+v %v", c, err)
	}
	old, err := r.GetMonthlySettlementCycle(context.Background(), "2026-10")
	if err != nil || old.Status != "blocked" || old.ProgressKnown {
		t.Fatalf("unknown historical work was fabricated %+v %v", old, err)
	}
}

func TestFIN002BatchBoundAndDurableRestartOutsideWindow(t *testing.T) {
	db := monthlySettlementFixture(t)
	for i := 0; i < 31; i++ {
		seedFinanceBalance(t, db, fmt.Sprintf("bounded-%02d", i), 2000000)
	}
	ctx := context.Background()
	first, err := NewFinanceRepository(db).RunMonthlySettlementForPeriod(ctx, "2026-11", "manual")
	if err != nil || first.Status != "running" || first.CompletedStores != 25 || first.PendingStores != 6 {
		t.Fatalf("unbounded/premature completed batch: %+v %v", first, err)
	}
	// A fresh service has no cursor/state from the previous call. Outside the
	// configured catch-up window, it must still finish the durable manifest.
	second, err := NewFinanceRepository(db).RunMonthlySettlement(ctx, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC), "restarted-worker")
	if err != nil || second.Status != "completed" || second.CompletedStores != 31 || second.TotalStores != 31 {
		t.Fatalf("unfinished work did not resume: %+v %v", second, err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM monthly_settlement_items`).Scan(&n); err != nil || n != 31 {
		t.Fatalf("replayed item effects %d %v", n, err)
	}
}

func TestFIN002CommittedAccountSurvivesCanceledBatchAndRestart(t *testing.T) {
	db := monthlySettlementFixture(t)
	for _, id := range []string{"a-first", "z-remaining"} {
		seedFinanceBalance(t, db, id, 2000000)
		monthlyBankHistory(t, db, id)
	}
	r := NewFinanceRepository(db)
	ctx := context.Background()
	if _, err := r.initializeMonthlyCycle(ctx, "2026-11", 10, "worker"); err != nil {
		t.Fatal(err)
	}
	if err := r.settleMonthlyAccount(ctx, "2026-11", "a-first"); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.RunMonthlySettlementForPeriod(canceled, "2026-11", "lost-worker"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled batch accepted: %v", err)
	}
	var done int
	if err := db.QueryRow(`SELECT count(*) FROM monthly_settlement_progress WHERE state='done'`).Scan(&done); err != nil || done != 1 {
		t.Fatalf("committed progress lost %d %v", done, err)
	}
	result, err := NewFinanceRepository(db).RunMonthlySettlement(ctx, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC), "restarted")
	if err != nil || result.Status != "completed" || result.TotalGross != 4000000 {
		t.Fatalf("restart %+v %v", result, err)
	}
	var total, count int64
	if err := db.QueryRow(`SELECT COALESCE(sum(amount),0),count(*) FROM payout_requests WHERE status='pending'`).Scan(&total, &count); err != nil || total != 4000000 || count != 2 {
		t.Fatalf("duplicate/recovered reservations count=%d total=%d err=%v", count, total, err)
	}
}

func TestFIN002ActualBatchCancellationPreservesFirstCommittedAccount(t *testing.T) {
	db := monthlySettlementFixture(t)
	for _, id := range []string{"a-committed", "z-interrupted"} {
		seedFinanceBalance(t, db, id, 2000000)
		monthlyBankHistory(t, db, id)
	}
	if _, err := db.Exec(`CREATE FUNCTION pause_second_monthly_item() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.store_id='z-interrupted' THEN PERFORM pg_advisory_xact_lock(91827003); END IF; RETURN NEW; END $$;
 CREATE TRIGGER pause_second_monthly_item BEFORE INSERT ON monthly_settlement_items FOR EACH ROW EXECUTE FUNCTION pause_second_monthly_item()`); err != nil {
		t.Fatal(err)
	}
	inspection, stopInspection := context.WithTimeout(context.Background(), 8*time.Second)
	defer stopInspection()
	barrier, err := db.Conn(inspection)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Close()
	if _, err = barrier.ExecContext(inspection, `SELECT pg_advisory_lock(91827003)`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		barrier.ExecContext(ctx, `SELECT pg_advisory_unlock(91827003)`)
	}()
	request, cancel := context.WithCancel(inspection)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := NewFinanceRepository(db).RunMonthlySettlementForPeriod(request, "2026-11", "actual-batch")
		done <- err
	}()
	ready := false
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if err := db.QueryRowContext(inspection, `SELECT EXISTS(SELECT 1 FROM monthly_settlement_progress WHERE store_id='a-committed' AND state='done')
 AND EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND wait_event='advisory' AND query LIKE '%INSERT INTO monthly_settlement_items%')`).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("actual batch did not reach second-account barrier after first commit")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted batch returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("actual batch did not stop after cancellation")
	}
	var committed, remaining, payouts int
	if err := db.QueryRowContext(inspection, `SELECT count(*) FILTER(WHERE state='done'),count(*) FILTER(WHERE state='pending') FROM monthly_settlement_progress`).Scan(&committed, &remaining); err != nil || committed != 1 || remaining != 1 {
		t.Fatalf("lost/miscompleted durable progress %d/%d %v", committed, remaining, err)
	}
	if err := db.QueryRowContext(inspection, `SELECT count(*) FROM payout_requests WHERE status='pending'`).Scan(&payouts); err != nil || payouts != 1 {
		t.Fatalf("interrupted account leaked payout %d %v", payouts, err)
	}
	if _, err = barrier.ExecContext(inspection, `SELECT pg_advisory_unlock(91827003)`); err != nil {
		t.Fatal(err)
	}
	cycle, err := NewFinanceRepository(db).RunMonthlySettlement(inspection, time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC), "fresh-worker")
	if err != nil || cycle.Status != "completed" || cycle.CompletedStores != 2 || cycle.TotalGross != 4000000 {
		t.Fatalf("actual interrupted batch did not resume %+v %v", cycle, err)
	}
	if err := db.QueryRowContext(inspection, `SELECT count(*) FROM payout_requests WHERE status='pending'`).Scan(&payouts); err != nil || payouts != 2 {
		t.Fatalf("restart duplicated payout %d %v", payouts, err)
	}
}

func TestFIN002TwoWorkersAndForegroundDebitsConserveTwoMillion(t *testing.T) {
	for _, kind := range []string{"payout", "early", "service"} {
		t.Run(kind, func(t *testing.T) {
			db := monthlySettlementFixture(t)
			seedFinanceBalance(t, db, "competing", 2000000)
			monthlyBankHistory(t, db, "competing")
			r := NewFinanceRepository(db)
			ctx := context.Background()
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < 3; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					if i < 2 {
						if _, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "worker"); err != nil {
							t.Error(err)
						}
						return
					}
					var err error
					switch kind {
					case "payout":
						_, err = r.RequestStorePayout(ctx, "competing", "bank", "bank", 2000000)
					case "early":
						_, err = r.RequestEarlyWithdrawal(ctx, "00000000-0000-4000-8000-000000000001", "competing", 2000000, "bank", "bank")
					case "service":
						err = r.PurchaseSellerService(ctx, "00000000-0000-4000-8000-000000000001", "competing", "standard")
					}
					if err != nil && !errors.Is(err, ErrStoreInsufficientBalance) {
						t.Error(err)
					}
				}(i)
			}
			close(start)
			wg.Wait()
			if _, err := db.Exec(`UPDATE monthly_settlement_progress SET next_attempt_at=NOW() WHERE state<>'done'`); err != nil {
				t.Fatal(err)
			}
			c, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "resume")
			if err != nil || c.Status != "completed" {
				t.Fatalf("competing cycle %+v %v", c, err)
			}
			var used int64
			if err := db.QueryRow(`SELECT COALESCE((SELECT sum(amount) FROM payout_requests WHERE store_id='competing' AND status='pending'),0)+COALESCE((SELECT sum(gross_amount) FROM seller_early_withdrawals WHERE store_id='competing' AND status='pending'),0)+COALESCE((SELECT sum(amount) FROM seller_service_purchases WHERE store_id='competing'),0)`).Scan(&used); err != nil || used != 2000000 {
				t.Fatalf("2m became %d reserved/debited: %v", used, err)
			}
			balance, err := r.GetStoreBalance(ctx, "competing")
			if err != nil || balance.AvailableAmount != 0 {
				t.Fatalf("conservation balance=%+v %v", balance, err)
			}
			var duplicates int
			if err := db.QueryRow(`SELECT count(*) FROM monthly_settlement_items WHERE cycle_period='2026-11' AND store_id='competing'`).Scan(&duplicates); err != nil || duplicates > 1 {
				t.Fatalf("duplicate cycle effect %d %v", duplicates, err)
			}
		})
	}
}

func TestFIN002RejectedPayoutDoesNotRearmCycle(t *testing.T) {
	db := monthlySettlementFixture(t)
	seedFinanceBalance(t, db, "rejected", 2000000)
	monthlyBankHistory(t, db, "rejected")
	r := NewFinanceRepository(db)
	ctx := context.Background()
	if _, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE payout_requests SET status='rejected' WHERE status='pending'`); err != nil {
		t.Fatal(err)
	}
	c, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "retry")
	if err != nil || !c.AlreadyCompleted || c.TotalGross != 2000000 {
		t.Fatalf("sealed cycle changed %+v %v", c, err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM payout_requests`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rejected payout silently replaced %d %v", n, err)
	}
}

func TestFIN002BusyFirstPageDoesNotStarveLaterAccount(t *testing.T) {
	db := monthlySettlementFixture(t)
	if _, err := db.Exec(`INSERT INTO stores(id,owner_type) SELECT 'busy-'||lpad(n::text,2,'0'),'dropshipper' FROM generate_series(1,25)n`); err != nil {
		t.Fatal(err)
	}
	seedFinanceBalance(t, db, "z-healthy", 2000000)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	barrier, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	rows, err := barrier.QueryContext(ctx, `SELECT id FROM stores WHERE id LIKE 'busy-%' ORDER BY id FOR UPDATE`)
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	r := NewFinanceRepository(db)
	_, err = r.RunMonthlySettlementForPeriod(ctx, "2026-11", "busy-first-batch")
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err = r.RunMonthlySettlementForPeriod(ctx, "2026-11", "next-batch"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := db.QueryRowContext(ctx, `SELECT state FROM monthly_settlement_progress WHERE store_id='z-healthy'`).Scan(&state); err != nil || state != "done" {
		t.Fatalf("busy first page starved later account: %s %v", state, err)
	}
	c, err := r.GetMonthlySettlementCycle(ctx, "2026-11")
	if err != nil || c.Status == "completed" || c.PendingStores != 25 {
		t.Fatalf("locked accounts falsely completed: %+v %v", c, err)
	}
}

func TestFIN002SweepSerializesWithPausedForegroundPayout(t *testing.T) {
	db := monthlySettlementFixture(t)
	seedFinanceBalance(t, db, "deterministic", 2000000)
	monthlyBankHistory(t, db, "deterministic")
	if _, err := db.Exec(`CREATE FUNCTION pause_foreground_payout() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.shaba='foreground-bank' THEN PERFORM pg_advisory_xact_lock(91827002); END IF; RETURN NEW; END $$;
 CREATE TRIGGER pause_foreground_payout BEFORE INSERT ON payout_requests FOR EACH ROW EXECUTE FUNCTION pause_foreground_payout()`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	barrier, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Close()
	if _, err = barrier.ExecContext(ctx, `SELECT pg_advisory_lock(91827002)`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		barrier.ExecContext(cleanup, `SELECT pg_advisory_unlock(91827002)`)
	}()
	r := NewFinanceRepository(db)
	done := make(chan error, 1)
	go func() {
		_, err := r.RequestStorePayout(ctx, "deterministic", "foreground-bank", "bank", 2000000)
		done <- err
	}()
	ready := false
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND wait_event='advisory' AND query LIKE '%INSERT INTO payout_requests%')`).Scan(&ready); err != nil {
			t.Fatal(err)
		}
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("foreground payout did not reach its actual post-balance INSERT barrier")
	}
	if _, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "overlapping-sweep"); err != nil {
		t.Fatal(err)
	}
	var reserved int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(sum(amount),0) FROM payout_requests WHERE status='pending'`).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatalf("sweep bypassed held foreground account and authorized stale funds: %d %v", reserved, err)
	}
	if _, err = barrier.ExecContext(ctx, `SELECT pg_advisory_unlock(91827002)`); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE monthly_settlement_progress SET next_attempt_at=NOW() WHERE state<>'done'`); err != nil {
		t.Fatal(err)
	}
	c, err := r.RunMonthlySettlementForPeriod(ctx, "2026-11", "resume")
	if err != nil || c.Status != "completed" {
		t.Fatalf("resume %+v %v", c, err)
	}
	if err := db.QueryRow(`SELECT COALESCE(sum(amount),0) FROM payout_requests WHERE status='pending'`).Scan(&reserved); err != nil || reserved != 2000000 {
		t.Fatalf("2m became %d reserved after deterministic overlap: %v", reserved, err)
	}
}

func TestFIN002MonthlySweepDoesNotRetainUnrelatedAccountGuard(t *testing.T) {
	db := monthlySettlementFixture(t)
	seedFinanceBalance(t, db, "a-independent", 2000000)
	seedFinanceBalance(t, db, "z-blocked", 2000000)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	locked, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locked.Rollback()
	if _, err := locked.ExecContext(ctx, `SELECT id FROM stores WHERE id='z-blocked' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	repo := NewFinanceRepository(db)
	done := make(chan error, 1)
	go func() {
		_, err := repo.RunMonthlySettlement(ctx, time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC), "synthetic-worker")
		done <- err
	}()
	// Either observe the actual account-lock wait or a bounded completed batch.
	// Polling does not require the short lock wait to remain visible under load.
	finished := false
	ready := false
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		select {
		case err := <-done:
			if err != nil && !strings.Contains(err.Error(), "lock timeout") {
				t.Fatalf("sweep failed before exercising contention: %v", err)
			}
			finished, ready = true, true
		default:
		}
		if ready {
			break
		}
		var waiting bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM stores WHERE id=$1 FOR UPDATE%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			ready = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !ready {
		t.Fatal("sweep never reached the held account or finished a bounded batch")
	}
	foreground, stop := context.WithTimeout(ctx, 400*time.Millisecond)
	started := time.Now()
	payout, payoutErr := repo.RequestStorePayout(foreground, "a-independent", "synthetic-bank", "synthetic-bank", 1000000)
	elapsed := time.Since(started)
	stop()
	// Release the exact test barrier before reporting an assertion failure.
	if err := locked.Rollback(); err != nil {
		t.Fatal(err)
	}
	if !finished {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("sweep did not leave after releasing its test barrier")
		}
	}
	if payoutErr != nil || payout == nil || elapsed >= 400*time.Millisecond {
		t.Fatalf("unrelated foreground payout blocked by whole-sweep guards: duration=%s error=%v", elapsed, payoutErr)
	}
	t.Logf("local foreground payout during blocked monthly sweep: %s", elapsed)
}
