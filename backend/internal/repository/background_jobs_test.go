package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/jobrunner"
	"github.com/drup/platform/internal/testdb"
	"os"
	"sync"
	"testing"
	"time"
)

func TestJobStoreTwoOwnersAndStaleCompletion(t *testing.T) {
	dsn := os.Getenv("OPERATIONS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable database required")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := JobStore{db}
	ctx := context.Background()
	ok, err := s.Acquire(ctx, "ownership-test", "old", 30*time.Millisecond)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err = s.Acquire(ctx, "ownership-test", "other", time.Second); err != nil || ok {
		t.Fatal("simultaneous lease", ok, err)
	}
	time.Sleep(45 * time.Millisecond)
	if ok, err = s.Acquire(ctx, "ownership-test", "new", time.Second); err != nil || !ok {
		t.Fatal("crash recovery", ok, err)
	}
	if err = s.Complete(ctx, jobrunner.Outcome{Job: "ownership-test", Owner: "old", Started: time.Now()}); err == nil {
		t.Fatal("stale owner acknowledged newer lease")
	}
	if err = s.Release(ctx, "ownership-test", "old"); err != nil {
		t.Fatal(err)
	}
	o := jobrunner.Outcome{Job: "ownership-test", Owner: "new", Started: time.Now(), Result: jobrunner.Result{Affected: 7}}
	if err = s.Complete(ctx, o); err != nil {
		t.Fatal(err)
	}
	if err = s.Complete(ctx, o); err != nil {
		t.Fatal(err)
	}
	var owner string
	var runs, n int
	if err = db.QueryRow(`SELECT owner,runs,affected FROM background_job_state WHERE job='ownership-test'`).Scan(&owner, &runs, &n); err != nil || owner != "new" || runs != 1 || n != 7 {
		t.Fatal(owner, runs, n, err)
	}
	if err = s.Release(ctx, "ownership-test", "new"); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryReleaseTwoWorkersPostsExactlyOneBalancedPair(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	id, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',dropshipper_profit=7000,return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, id)
	var wg sync.WaitGroup
	results := make(chan int64, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := NewOrderRepository(db).ReleaseClosedReturnWindows(context.Background())
			results <- n
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	var total int64
	for n := range results {
		total += n
	}
	for err := range failures {
		if err != nil && !isCandidateBudgetCancellation(err) {
			t.Fatal(err)
		}
		if err != nil {
			t.Logf("contending worker yielded within candidate budget: %v", err)
		}
	}
	total += finishDueReleaseRetry(t, db, id)
	var n, credit, debit int64
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount) FILTER(WHERE type='credit'),0),COALESCE(SUM(amount) FILTER(WHERE type='debit'),0) FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser'`, id).Scan(&n, &credit, &debit); err != nil {
		t.Fatal(err)
	}
	if total > 1 || n != 2 || credit != 7000 || debit != 7000 {
		t.Fatal(total, n, credit, debit)
	}
}

// Candidate contention is deliberately bounded. A timeout is an observable
// retry outcome, not evidence that the financial transaction partly committed.
func TestDeliveryReleaseContendedDeadlineRollsBackAndPersistsRetry(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	id, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',dropshipper_profit=7000,return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, id)
	blocker, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = blocker.Exec(`SELECT id FROM orders WHERE id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	n, err := NewOrderRepository(db).ReleaseClosedReturnWindows(context.Background())
	if err == nil || !isCandidateBudgetCancellation(err) || n != 0 {
		t.Fatalf("bounded candidate n=%d err=%v", n, err)
	}
	var released bool
	var ledger int
	var retry bool
	if err = db.QueryRow(`SELECT profit_released FROM orders WHERE id=$1`, id).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, id).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM background_job_candidate_retry WHERE job='delivery-release' AND candidate=$1 AND next_retry_at>clock_timestamp())`, id).Scan(&retry); err != nil {
		t.Fatal(err)
	}
	t.Logf("owned guard contention elapsed=%s released=%v ledger=%d persisted_retry=%v", time.Since(began), released, ledger, retry)
	if released || ledger != 0 || !retry {
		t.Fatal("timeout left partial finance or lost retry", released, ledger, retry)
	}
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if n, err = NewOrderRepository(db).ReleaseClosedReturnWindows(context.Background()); err != nil || n != 0 {
		t.Fatalf("persisted delay ignored n=%d err=%v", n, err)
	}
	// Advance only this disposable operational retry marker, simulating its due
	// time without weakening the production timeout or waiting five wall minutes.
	finishDueReleaseRetry(t, db, id)
	var credit, debit int64
	if err = db.QueryRow(`SELECT count(*),COALESCE(SUM(amount) FILTER(WHERE type='credit'),0),COALESCE(SUM(amount) FILTER(WHERE type='debit'),0) FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser'`, id).Scan(&ledger, &credit, &debit); err != nil {
		t.Fatal(err)
	}
	if ledger != 2 || credit != 7000 || debit != 7000 {
		t.Fatal(ledger, credit, debit)
	}
}

// Cancellation can race database/sql rollback and lib/pq connection invalidation.
// A bad connection needs an actual deadline in this same error chain; unrelated
// business/metadata leaves must still fail every cancellation assertion.
func isCandidateBudgetCancellation(err error) bool {
	if err == nil || errors.Is(err, jobrunner.ErrMetadata) {
		return false
	}
	allowBadConnection := errors.Is(err, context.DeadlineExceeded)
	var cancellation func(error) bool
	cancellation = func(part error) bool {
		if part == nil {
			return false
		}
		if joined, ok := part.(interface{ Unwrap() []error }); ok {
			for _, leaf := range joined.Unwrap() {
				if !cancellation(leaf) {
					return false
				}
			}
			return true
		}
		if wrapped, ok := part.(interface{ Unwrap() error }); ok {
			return cancellation(wrapped.Unwrap())
		}
		if errors.Is(part, context.DeadlineExceeded) || errors.Is(part, sql.ErrTxDone) {
			return true
		}
		if errors.Is(part, driver.ErrBadConn) {
			return allowBadConnection
		}
		var state interface{ SQLState() string }
		return errors.As(part, &state) && state.SQLState() == "57014"
	}
	return cancellation(err)
}

// The financial assertion is eventual exactly-once effects, not first-pass
// success under arbitrary suite load. Advance only the owned disposable retry
// marker after verifying that unfinished work really has a persisted retry.
func finishDueReleaseRetry(t *testing.T, db *sql.DB, id string) int64 {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	var total int64
	for time.Now().Before(until) {
		var released bool
		if err := db.QueryRow(`SELECT profit_released FROM orders WHERE id=$1`, id).Scan(&released); err != nil {
			t.Fatal(err)
		}
		if released {
			return total
		}
		var next time.Time
		if err := db.QueryRow(`SELECT next_retry_at FROM background_job_candidate_retry WHERE job='delivery-release' AND candidate=$1`, id).Scan(&next); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				t.Fatal("unfinished release lost its persisted retry")
			}
			t.Fatal(err)
		}
		opsExec(t, db, `UPDATE background_job_candidate_retry SET next_retry_at=clock_timestamp()-INTERVAL '1 second' WHERE job='delivery-release' AND candidate=$1`, id)
		n, err := NewOrderRepository(db).ReleaseClosedReturnWindows(context.Background())
		total += n
		if err != nil && !isCandidateBudgetCancellation(err) {
			t.Fatal(err)
		}
	}
	t.Fatal("release did not recover within bounded disposable retry simulation")
	return total
}

func TestDeliveryReleaseRevalidatesEligibilityAndAmountAfterGuard(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	id, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',dropshipper_profit=1000,return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, id)
	repo := NewOrderRepository(db)
	for _, eligible := range []bool{false, true} {
		opsExec(t, db, `UPDATE orders SET return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, id)
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`SELECT id FROM orders WHERE id=$1 FOR UPDATE`, id); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := repo.ReleaseClosedReturnWindows(context.Background()); done <- err }()
		until := time.Now().Add(time.Second)
		waiting := 0
		for time.Now().Before(until) {
			_ = db.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&waiting)
			if waiting > 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if waiting == 0 {
			tx.Rollback()
			t.Fatal("release did not reach financial guard")
		}
		window := "NOW()+INTERVAL '1 day'"
		if eligible {
			window = "NOW()-INTERVAL '1 hour'"
		}
		if _, err = tx.Exec(`UPDATE orders SET dropshipper_profit=3333,return_window_ends_at=`+window+` WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err = <-done; err != nil {
			if !isCandidateBudgetCancellation(err) {
				t.Fatal(err)
			}
			var released bool
			var rows int
			var retry bool
			if e := db.QueryRow(`SELECT profit_released FROM orders WHERE id=$1`, id).Scan(&released); e != nil {
				t.Fatal(e)
			}
			if e := db.QueryRow(`SELECT count(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, id).Scan(&rows); e != nil {
				t.Fatal(e)
			}
			if e := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM background_job_candidate_retry WHERE job='delivery-release' AND candidate=$1 AND next_retry_at>clock_timestamp())`, id).Scan(&retry); e != nil {
				t.Fatal(e)
			}
			if released || rows != 0 || !retry {
				t.Fatal("interrupted guard left partial effects or lost retry", released, rows, retry)
			}
			if eligible {
				finishDueReleaseRetry(t, db, id)
			} else {
				opsExec(t, db, `UPDATE background_job_candidate_retry SET next_retry_at=clock_timestamp()-INTERVAL '1 second' WHERE job='delivery-release' AND candidate=$1`, id)
				if n, e := repo.ReleaseClosedReturnWindows(context.Background()); e != nil || n != 0 {
					t.Fatal("ineligible retry released money", n, e)
				}
			}
		}
		var released bool
		var n, credit, debit int64
		if err = db.QueryRow(`SELECT profit_released FROM orders WHERE id=$1`, id).Scan(&released); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount) FILTER(WHERE type='credit'),0),COALESCE(SUM(amount) FILTER(WHERE type='debit'),0) FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser'`, id).Scan(&n, &credit, &debit); err != nil {
			t.Fatal(err)
		}
		if eligible {
			if !released || n != 2 || credit != 3333 || debit != 3333 {
				t.Fatal(released, n, credit, debit)
			}
		} else if released || n != 0 {
			t.Fatal("stale snapshot authorized release", released, n)
		}
	}
	// A second worker's retry cannot duplicate the accepted money pair.
	if n, err := repo.ReleaseClosedReturnWindows(context.Background()); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

// The guarded transaction's authority is tested separately from admission's
// 300ms retry policy, so a cancelled first attempt cannot hide a stale snapshot
// mutation. Production callers still supply their unchanged300ms child context.
func TestDeliveryReleaseTransactionUsesGuardedSnapshot(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	id, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',dropshipper_profit=1000,return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, id)
	for _, eligible := range []bool{false, true} {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`SELECT id FROM orders WHERE id=$1 FOR UPDATE`, id); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		done := make(chan error, 1)
		go func() { _, e := NewOrderRepository(db).releaseClosedReturnWindow(ctx, id); done <- e }()
		until := time.Now().Add(time.Second)
		waiting := false
		for time.Now().Before(until) {
			if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock')`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !waiting {
			_ = tx.Rollback()
			cancel()
			t.Fatal("guarded transaction never reached owned barrier")
		}
		window := "NOW()+INTERVAL '1 day'"
		if eligible {
			window = "NOW()-INTERVAL '1 hour'"
		}
		if _, err = tx.Exec(`UPDATE orders SET dropshipper_profit=3333,return_window_ends_at=`+window+` WHERE id=$1`, id); err != nil {
			_ = tx.Rollback()
			cancel()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			cancel()
			t.Fatal(err)
		}
		err = <-done
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		var released bool
		var n, credit, debit int64
		if err = db.QueryRow(`SELECT profit_released FROM orders WHERE id=$1`, id).Scan(&released); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(`SELECT count(*),COALESCE(SUM(amount)FILTER(WHERE type='credit'),0),COALESCE(SUM(amount)FILTER(WHERE type='debit'),0) FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser'`, id).Scan(&n, &credit, &debit); err != nil {
			t.Fatal(err)
		}
		if eligible {
			if !released || n != 2 || credit != 3333 || debit != 3333 {
				t.Fatal(released, n, credit, debit)
			}
		} else if released || n != 0 {
			t.Fatal("pre-guard eligibility authorized a release", released, n)
		}
	}
}

func TestJobsBoundCleanupQCAndFairSupplierAssessments(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	opsExec(t, db, `INSERT INTO ad_request_limits(identity_key,bucket,count) SELECT 'job-'||g, NOW()-INTERVAL '1 hour',1 FROM generate_series(1,2101)g`)
	opsExec(t, db, `INSERT INTO ad_fraud_keys(campaign_id,event_type,identity_key,expires_at) SELECT 'job','view','job-'||g,NOW()-INTERVAL '2 hours' FROM generate_series(1,2101)g`)
	n, err := CleanupExpiredAds(context.Background(), db)
	if err != nil || n != 4000 {
		t.Fatal(n, err)
	}
	var remain int
	if err = db.QueryRow(`SELECT count(*) FROM ad_request_limits WHERE identity_key LIKE 'job-%'`).Scan(&remain); err != nil || remain != 101 {
		t.Fatal(remain, err)
	}
	for i := 0; i < 105; i++ {
		opsExec(t, db, `INSERT INTO qc_discrepancies(id,supplier_id,product_id,expected_quantity,received_quantity,defect_count,reason,status,response_due_at) VALUES($1,$2,$3,2,1,0,'job','open',NOW()-INTERVAL '1 minute')`, fmt.Sprintf("job-qc-%d", i), f.supplier, f.product)
	}
	if n, err = EscalateQCDeadlines(context.Background(), db); err != nil || n != 100 {
		t.Fatal(n, err)
	}
	if n, err = EscalateQCDeadlines(context.Background(), db); err != nil || n != 5 {
		t.Fatal(n, err)
	}
	opsExec(t, db, `INSERT INTO suppliers(id,name,approval_status) SELECT 'job-sla-'||g,'job supplier','approved' FROM generate_series(1,115)g`)
	// Put a known early candidate behind an actual SQL barrier. Other
	// candidates commit; the blocked one must roll back and persist its retry.
	poison := "job-sla-1"
	opsExec(t, db, `CREATE FUNCTION job01_block_assessment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.supplier_id=TG_ARGV[0] THEN PERFORM pg_advisory_xact_lock(90261177); END IF; RETURN NEW; END $$`)
	opsExec(t, db, `CREATE TRIGGER job01_block_assessment BEFORE INSERT OR UPDATE ON supplier_sla_metrics FOR EACH ROW EXECUTE FUNCTION job01_block_assessment('job-sla-1')`)
	barrier, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if _, err = barrier.Exec(`SELECT pg_advisory_xact_lock(90261177)`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	passCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	go func() { _, e := RefreshSupplierSLA(passCtx, db); done <- e }()
	until := time.Now().Add(3 * time.Second)
	blocked := false
	retry := false
	for time.Now().Before(until) {
		var waiting bool
		if e := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=90261177 AND NOT granted)`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		blocked = blocked || waiting
		if e := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM background_job_candidate_retry WHERE job='supplier-sla' AND candidate=$1 AND next_retry_at>clock_timestamp())`, poison).Scan(&retry); e != nil {
			t.Fatal(e)
		}
		if retry {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = barrier.Rollback()
	err = <-done
	cancel()
	if !blocked || !retry || !isCandidateBudgetCancellation(err) {
		t.Fatal("controlled candidate defer not proved", blocked, retry, err)
	}
	var rejectedMetrics int
	if e := db.QueryRow(`SELECT count(*) FROM supplier_sla_metrics WHERE supplier_id=$1`, poison).Scan(&rejectedMetrics); e != nil || rejectedMetrics != 0 {
		t.Fatal("rejected candidate partially committed", rejectedMetrics, e)
	}
	var first, attempted int
	if e := db.QueryRow(`SELECT count(*) FROM supplier_sla_metrics`).Scan(&first); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`SELECT count(*) FROM (SELECT supplier_id AS id FROM supplier_sla_metrics UNION SELECT candidate FROM background_job_candidate_retry WHERE job='supplier-sla') q`).Scan(&attempted); e != nil {
		t.Fatal(e)
	}
	if first < 1 || first > 100 || attempted > 100 {
		t.Fatal("lost committed partial pass or exceeded100 candidates", first, attempted)
	}
	t.Logf("controlled SLA pass committed=%d distinct_attempted=%d rejected_rolled_back=%d durable_retry=%v", first, attempted, rejectedMetrics, retry)
	// Simulate only owned disposable retry times becoming due; each real pass
	// keeps the production3s parent and300ms candidate budgets.
	until = time.Now().Add(30 * time.Second)
	for time.Now().Before(until) {
		var untouched int
		if e := db.QueryRow(`SELECT count(*) FROM suppliers s LEFT JOIN supplier_sla_metrics m ON s.id=m.supplier_id WHERE m.supplier_id IS NULL`).Scan(&untouched); e != nil {
			t.Fatal(e)
		}
		if untouched == 0 {
			return
		}
		opsExec(t, db, `UPDATE background_job_candidate_retry SET next_retry_at=clock_timestamp()-INTERVAL '1 second' WHERE job='supplier-sla'`)
		before := first
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		_, e := RefreshSupplierSLA(ctx, db)
		stop()
		if e != nil && !isCandidateBudgetCancellation(e) {
			t.Fatal(e)
		}
		if e = db.QueryRow(`SELECT count(*) FROM supplier_sla_metrics`).Scan(&first); e != nil {
			t.Fatal(e)
		}
		if first-before > 100 {
			t.Fatal("retry pass exceeded100 supplier bound", before, first)
		}
	}
	t.Fatal("bounded due retries starved suppliers")
}

func TestCandidateContextRollbackCanReturnTxDoneWithoutEffects(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO supplier_sla_metrics(supplier_id,samples,on_time,score,eligible) VALUES($1,0,0,0,FALSE)`, f.supplier); err != nil {
		t.Fatal(err)
	}
	cancel()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		err = tx.Commit()
		if errors.Is(err, sql.ErrTxDone) {
			break
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !errors.Is(err, sql.ErrTxDone) {
		t.Fatal("context rollback did not produce transaction-done outcome", err)
	}
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM supplier_sla_metrics WHERE supplier_id=$1`, f.supplier).Scan(&n); err != nil || n != 0 {
		t.Fatal("canceled transaction left effects", n, err)
	}
	t.Log("actual canceled PostgreSQL transaction rolled back; Commit reports sql.ErrTxDone")
}

func TestExpiryTransientDeadlineDoesNotBecomeFiveMinuteHumanReview(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	id, _ := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE orders SET payment_expires_at=clock_timestamp()-INTERVAL '2 minutes' WHERE id=$1`, id)
	opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,payment_expires_at) SELECT $1||g,$1||g,$2,'transient backlog',0,0,'job','09123456789','x','x','x','1234512345',$1||g,clock_timestamp()-INTERVAL '2 minutes' FROM generate_series(1,69)g`, "trn-"+f.store+"-", f.store)
	barrier, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback()
	if _, err = barrier.Exec(`SELECT id FROM stores WHERE id=$1 FOR UPDATE`, f.store); err != nil {
		t.Fatal(err)
	}
	repo := NewOrderRepository(db)
	batchCtx, batchCancel := context.WithTimeout(context.Background(), 3*time.Second)
	orders, runErr := repo.ExpireUnpaidOrders(batchCtx)
	batchCancel()
	if runErr == nil || !isCandidateBudgetCancellation(runErr) || len(orders) != 0 {
		t.Fatal("controlled timeout not observed", len(orders), runErr)
	}
	var pending string
	var reason string
	var seconds int64
	if err = db.QueryRow(`SELECT status FROM orders WHERE id=$1`, id).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	var humanReason sql.NullString
	var humanDelay sql.NullInt64
	err = db.QueryRow(`SELECT reason,EXTRACT(EPOCH FROM next_retry_at-clock_timestamp())::bigint FROM order_expiry_review_state WHERE order_id=$1`, id).Scan(&humanReason, &humanDelay)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	automaticErr := db.QueryRow(`SELECT reason,EXTRACT(EPOCH FROM next_retry_at-clock_timestamp())::bigint FROM background_job_candidate_retry WHERE job='unpaid-order-expiry' AND candidate=$1`, id).Scan(&reason, &seconds)
	t.Logf("controlled pending order=%s status=%s old_review_reason=%s old_delay_seconds=%d automatic_reason=%s automatic_delay_seconds=%d", id, pending, humanReason.String, humanDelay.Int64, reason, seconds)
	if automaticErr != nil || reason != "transient" || seconds > 5 || humanReason.Valid {
		t.Errorf("ordinary deadline misclassified as long human review: automatic_err=%v reason=%s delay=%d human=%s/%d", automaticErr, reason, seconds, humanReason.String, humanDelay.Int64)
	}
	health, err := BackgroundJobHealth(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if health[0].Ready != 70 || health[0].Review != 0 {
		t.Errorf("transient retry hidden in financial review: %+v", health[0])
	}
	t.Logf("actual70-order backlog ready=%d human_review=%d automatic_retry=%d oldest_ready_seconds=%d", health[0].Ready, health[0].Review, health[0].AutomaticRetry, health[0].OldestReadySeconds)
	_ = barrier.Rollback()
	if t.Failed() {
		return
	}
	until := time.Now().Add(12 * time.Second)
	for time.Now().Before(until) {
		if err = db.QueryRow(`SELECT status FROM orders WHERE id=$1`, id).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		var remaining int
		if err = db.QueryRow(`SELECT count(*) FROM orders WHERE store_id=$1 AND status IN ('pending','processing')`, f.store).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining == 0 {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = repo.ExpireUnpaidOrders(ctx)
		cancel()
		if err != nil && !isCandidateBudgetCancellation(err) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var remaining, cancelled int
	if err = db.QueryRow(`SELECT count(*) FILTER(WHERE status IN ('pending','processing')),count(*) FILTER(WHERE status='cancelled') FROM orders WHERE store_id=$1`, f.store).Scan(&remaining, &cancelled); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 || cancelled != 70 {
		t.Fatal("automatic retry failed to drain70 within expiry SLO", remaining, cancelled)
	}
	var stock, ledger int
	if err = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM ledger_entries l JOIN orders o ON o.id=l.related_order_or_dispute WHERE o.store_id=$1`, f.store).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	if stock != 100 || ledger != 0 {
		t.Fatal("transient expiry changed uncaptured stock or money", stock, ledger)
	}
	poison, _ := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE orders SET payment_expires_at=clock_timestamp()-INTERVAL '2 minutes' WHERE id=$1`, poison)
	opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status,shipped_at) VALUES($1,$2,$3,'{}','09123456789','job','shipped',NOW())`, "poison-"+f.supplier, poison, f.supplier)
	until = time.Now().Add(12 * time.Second)
	permanent := false
	for time.Now().Before(until) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, _ = repo.ExpireUnpaidOrders(ctx)
		cancel()
		var delay int64
		e := db.QueryRow(`SELECT EXTRACT(EPOCH FROM next_retry_at-clock_timestamp())::bigint FROM order_expiry_review_state WHERE order_id=$1 AND reason='candidate_rejected'`, poison).Scan(&delay)
		if e == nil {
			if delay < 240 {
				t.Fatal("financial review delay shortened", delay)
			}
			permanent = true
			break
		}
		if !errors.Is(e, sql.ErrNoRows) {
			t.Fatal(e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !permanent {
		t.Fatal("dispatched poison was downgraded from human review")
	}
	var poisonStatus string
	if err = db.QueryRow(`SELECT status FROM orders WHERE id=$1`, poison).Scan(&poisonStatus); err != nil || poisonStatus == "cancelled" {
		t.Fatal("dispatch guard weakened", poisonStatus, err)
	}
	health, err = BackgroundJobHealth(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if health[0].Ready != 0 || health[0].Review != 1 || health[0].AutomaticRetry != 0 {
		t.Fatal("permanent review mixed with automatic retry", health[0])
	}
	t.Log("genuine dispatch/reconciliation guard retains five-minute human review")
}

func TestSupplierPoisonCandidateYieldsAndRemainsVisible(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	opsExec(t, db, `CREATE FUNCTION job01_reject_sla() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.supplier_id LIKE 'os-%' THEN RAISE EXCEPTION 'fixture rejection'; END IF; RETURN NEW; END $$`)
	opsExec(t, db, `CREATE TRIGGER job01_reject_sla BEFORE INSERT OR UPDATE ON supplier_sla_metrics FOR EACH ROW EXECUTE FUNCTION job01_reject_sla()`)
	if _, err := RefreshSupplierSLA(context.Background(), db); err == nil {
		t.Fatal("poison failure hidden")
	}
	var deferred bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM background_job_candidate_retry WHERE job='supplier-sla' AND candidate=$1 AND next_retry_at>NOW())`, f.supplier).Scan(&deferred); err != nil || !deferred {
		t.Fatal(deferred, err)
	}
	if _, err := RefreshSupplierSLA(context.Background(), db); err != nil {
		t.Fatal("poison blocked next page", err)
	}
	health, err := BackgroundJobHealth(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range health {
		if h.Job == "supplier-sla" && h.Review != 1 {
			t.Fatalf("review row hidden: %+v", h)
		}
	}
}

func TestSupplierSLACommitFailureDoesNotCountRolledBackEffects(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	id, _ := opsOrder(t, db, f, true)
	opsExec(t, db, `INSERT INTO supplier_shipments(id,order_id,supplier_id,shipping_address,customer_phone,customer_name,status,created_at,shipped_at) VALUES($1,$2,$3,'{}','09123456789','job','shipped',NOW()-INTERVAL '3 days',NOW()-INTERVAL '2 days')`, "commit-"+f.supplier, id, f.supplier)
	// This fires only at COMMIT, after the candidate obtained a positive enabled
	// row count. The real PostgreSQL transaction must then roll every write back.
	opsExec(t, db, `CREATE FUNCTION job01_fail_sla_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'job01 forced deferred SLA commit failure'; END $$`)
	opsExec(t, db, `CREATE CONSTRAINT TRIGGER job01_fail_sla_commit AFTER INSERT OR UPDATE ON supplier_shipping_methods DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION job01_fail_sla_commit()`)
	r := jobrunner.New(JobStore{DB: db}, []jobrunner.Job{{ID: "supplier-sla", Period: time.Hour, Timeout: 3 * time.Second, Run: func(ctx context.Context) (jobrunner.Result, error) {
		n, err := RefreshSupplierSLA(ctx, db)
		return jobrunner.Result{Affected: n}, err
	}}})
	result, err := r.Run(context.Background(), "supplier-sla")
	if err == nil {
		t.Fatal("deferred business commit failure hidden")
	}
	if result.Affected != 0 {
		t.Errorf("rolled-back SLA effect returned as committed: affected=%d", result.Affected)
	}
	var metrics, grants int
	if err = db.QueryRow(`SELECT count(*) FROM supplier_sla_metrics WHERE supplier_id=$1`, f.supplier).Scan(&metrics); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM supplier_shipping_methods WHERE owner_user_id=$1`, f.user).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if metrics != 0 || grants != 0 {
		t.Errorf("deferred failure left business rows: metrics=%d grants=%d", metrics, grants)
	}
	var stateCount, outcomeCount, legacyCount int64
	var code string
	if err = db.QueryRow(`SELECT affected,last_error FROM background_job_state WHERE job='supplier-sla'`).Scan(&stateCount, &code); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT affected FROM background_job_outcomes WHERE job='supplier-sla'`).Scan(&outcomeCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT affected FROM operations_job_runs WHERE job='supplier-sla'`).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if stateCount != 0 || outcomeCount != 0 || legacyCount != 0 || code != "job_failed" {
		t.Errorf("outcome counted rollback or hid failure: state=%d outcome=%d legacy=%d code=%s", stateCount, outcomeCount, legacyCount, code)
	}
	var retry bool
	if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM background_job_candidate_retry WHERE job='supplier-sla' AND candidate=$1 AND next_retry_at>clock_timestamp())`, f.supplier).Scan(&retry); err != nil || !retry {
		t.Fatal("commit failure lost review retry", retry, err)
	}
}

func TestOutcomeStorageFailureIsVisibleAndRetryRecovers(t *testing.T) {
	db := operationsDB(t)
	opsExec(t, db, `CREATE FUNCTION job01_reject_outcome() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture metadata failure'; END $$`)
	opsExec(t, db, `CREATE TRIGGER job01_reject_outcome BEFORE INSERT ON background_job_outcomes FOR EACH ROW EXECUTE FUNCTION job01_reject_outcome()`)
	r := jobrunner.New(JobStore{DB: db}, []jobrunner.Job{{ID: "unpaid-order-expiry", Period: time.Hour, Timeout: time.Second, Run: func(context.Context) (jobrunner.Result, error) { return jobrunner.Result{Affected: 7}, nil }}})
	if result, err := r.Run(context.Background(), "unpaid-order-expiry"); !errors.Is(err, jobrunner.ErrMetadata) || result.Affected != 7 {
		t.Fatalf("lost committed result %+v %v", result, err)
	}
	health, err := BackgroundJobHealth(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if health[0].Error != "outcome_missing" || !health[0].Stale || health[0].Running {
		t.Fatalf("failed metadata looks healthy: %+v", health[0])
	}
	opsExec(t, db, `DROP TRIGGER job01_reject_outcome ON background_job_outcomes`)
	if _, err = r.Run(context.Background(), "unpaid-order-expiry"); err != nil {
		t.Fatal(err)
	}
	health, err = BackgroundJobHealth(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if health[0].Error != "" || health[0].Stale || health[0].Runs != 1 {
		t.Fatal(health[0])
	}
}

func TestExpiryRetryClassificationRequiresCancellationEvidence(t *testing.T) {
	cases := []struct {
		name            string
		err, contextErr error
		automatic       bool
	}{
		{"bare transaction done", sql.ErrTxDone, nil, false},
		{"cancelled transaction done", sql.ErrTxDone, context.DeadlineExceeded, true},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded, true},
		{"reconciliation", ErrFinancePrincipalUnresolved, nil, false},
		{"reconciliation with elapsed context", ErrFinancePrincipalUnresolved, context.DeadlineExceeded, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := expiryCandidateTransient(c.err, c.contextErr); got != c.automatic {
				t.Fatalf("automatic=%v want%v", got, c.automatic)
			}
		})
	}
}
