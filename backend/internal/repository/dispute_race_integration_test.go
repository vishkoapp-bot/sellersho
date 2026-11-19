package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"strings"
	"testing"
	"time"
)

func fin005WaitOrderWriters(t *testing.T, ctx context.Context, dbQuery func(context.Context, string, ...any) (int, error), want int) {
	t.Helper()
	for {
		n, err := dbQuery(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%SELECT id FROM orders WHERE id%FOR UPDATE%'`)
		if err != nil {
			t.Fatal(err)
		}
		if n >= want {
			return
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
}
func TestFIN005ReturnFilingRechecksAfterExpiryAndRelease(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	repo := NewOrderRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()+INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
	var item string
	if err := db.QueryRow(`SELECT id::text FROM order_items WHERE order_id=$1 LIMIT 1`, order).Scan(&item); err != nil {
		t.Fatal(err)
	}
	hold, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback()
	if _, err = hold.ExecContext(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, order); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, e := repo.CreateCustomerReturn(ctx, f.user, order, "race", []CustomerReturnItemRequest{{OrderItemID: item, Qty: 1}}, CustomerReturnOptions{})
		result <- e
	}()
	fin005WaitOrderWriters(t, ctx, func(ctx context.Context, q string, args ...any) (int, error) {
		var n int
		e := db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n, e
	}, 1)
	// Admission read happened before expiry, but release changed under its guard.
	if err = markDisputeWriterTx(ctx, hold); err != nil {
		t.Fatal(err)
	}
	if _, err = hold.ExecContext(ctx, `UPDATE orders SET return_window_ends_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, order); err != nil {
		t.Fatal(err)
	}
	if ok, e := releaseClosedReturnWindowTx(ctx, hold, order); e != nil || !ok {
		t.Fatalf("release winner %v %v", ok, e)
	}
	if err = hold.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrReturnWindowClosed) {
		t.Fatalf("stale filing admitted %v", err)
	}
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM customer_returns WHERE order_id=$1`, order).Scan(&n); err != nil || n != 0 {
		t.Fatalf("post-release return %d %v", n, err)
	}
}
func TestFIN005GuardedReturnWinsBeforeRelease(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	repo := NewOrderRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
	filing, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer filing.Rollback()
	if err = lockOrderFinanceTx(ctx, filing, order); err != nil {
		t.Fatal(err)
	}
	if err = markDisputeWriterTx(ctx, filing); err != nil {
		t.Fatal(err)
	}
	if _, err = filing.ExecContext(ctx, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,warehouse_id,prepaid) VALUES($1,$2,$3,'approved','already admitted before expiry',$4,TRUE)`, uuid.NewString(), order, f.user, warehouse); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		ok, e := repo.releaseClosedReturnWindow(ctx, order)
		if ok && e == nil {
			e = errors.New("released through committed return")
		}
		done <- e
	}()
	fin005WaitOrderWriters(t, ctx, func(ctx context.Context, q string, args ...any) (int, error) {
		var n int
		e := db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n, e
	}, 1)
	if err = filing.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestFIN005CarrierOnlyHoldAndStaleSellerEvidence(t *testing.T) {
	repo, id, order := fin005Admitted(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := repo.ResolveDisputeWithOutcome(ctx, id, DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "actor"}); err != nil {
		t.Fatal(err)
	}
	orders := NewOrderRepository(repo.db)
	if err := orders.UpdateOrderStatusFromCarrier(ctx, order, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := orders.releaseClosedReturnWindow(ctx, order); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := repo.db.QueryRow(`SELECT status='delivery_disputed' AND disputed_at<>(SELECT hold_at FROM dispute_order_evidence WHERE dispute_id=$2) AND NOT profit_released FROM orders WHERE id=$1`, order, id).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("later carrier failure overwritten %v %v", preserved, err)
	}
	// Carrier-only failure needs its own actual physical timestamp, no fabricated dispute.
	db, _, _, only := fin003VerifiedPaidOrder(t)
	opsExec(t, db, `UPDATE orders SET status='shipped' WHERE id=$1`, only)
	if err := NewOrderRepository(db).UpdateOrderStatusFromCarrier(ctx, only, "failed"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM disputes WHERE order_id=$1`, only).Scan(&count); err != nil || count != 0 {
		t.Fatalf("carrier hold manufactured dispute %d %v", count, err)
	}
}

func TestFIN005OldChildFirstClosureRejectsBeforeParentAndIndependentOrderProgresses(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	repo := NewOrderRepository(db)
	// TEST-02 round 5: the context timeout was 8s, but it is created
	// at the top of the test while the two fin003VerifiedPaidOrder
	// setups consume most of it before the independent-order update
	// runs — in a shared-DB full run the update then exceeds the
	// remaining budget (r5b: "independent order blocked context
	// deadline exceeded"). 30s keeps the same intent (the
	// independent order must progress while the parent is held)
	// with margin for full-run latency.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
	ret := uuid.NewString()
	fin005ReturnFixtureExec(t, db, order, `INSERT INTO customer_returns(id,order_id,customer_id,status,reason,warehouse_id) VALUES($1,$2,$3,'approved','fixture',$4)`, ret, order, f.user, warehouse)
	independent, _, _, other := fin003VerifiedPaidOrder(t)
	opsExec(t, independent, `UPDATE orders SET status='shipped' WHERE id=$1`, other)
	// TEST-02 round 5: the independent order is created BEFORE the
	// hold transaction opens. fin003VerifiedPaidOrder runs
	// fin003RestorePrincipalGuards (DROP/CREATE TRIGGER on orders
	// and payment_transactions), which needs an ACCESS EXCLUSIVE
	// lock on those tables — it cannot run while the hold
	// transaction below holds a row lock on orders (r5b/solo:
	// the carrier update for the independent order then blocked
	// until the context deadline). Creating the order first keeps
	// the intent (the independent order progresses while the parent
	// is held) without DDL under a held lock.
	hold, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback()
	if err = lockOrderFinanceTx(ctx, hold, order); err != nil {
		t.Fatal(err)
	}
	// An old child-first writer must reject without trying the held parent.
	if _, err = db.ExecContext(ctx, `UPDATE customer_returns SET status='cancelled' WHERE id=$1`, ret); err == nil || !strings.Contains(err.Error(), "FIN005-writer-required") {
		t.Fatalf("old child writer waited/bypassed %v", err)
	}
	if err = NewOrderRepository(independent).UpdateOrderStatusFromCarrier(ctx, other, "failed"); err != nil {
		t.Fatalf("independent order blocked %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- repo.CancelCustomerReturn(ctx, ret) }()
	fin005WaitOrderWriters(t, ctx, func(ctx context.Context, q string, args ...any) (int, error) {
		var n int
		e := db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n, e
	}, 1)
	if ok, e := releaseClosedReturnWindowTx(ctx, hold, order); e != nil || ok {
		t.Fatalf("active return released %v %v", ok, e)
	}
	if err = hold.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	if ok, e := repo.releaseClosedReturnWindow(ctx, order); e != nil || !ok {
		t.Fatalf("closure recovery %v %v", ok, e)
	}
}
func TestFIN005WalletRefundReleaseAndPayoutConserveDebt(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	repo := NewOrderRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	key := time.Now().UnixNano()
	name := "fin005_pause_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	opsExec(t, db, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id::text=%s AND NEW.status='refunded' THEN PERFORM pg_advisory_xact_lock(%d); END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE UPDATE ON customer_returns FOR EACH ROW EXECUTE FUNCTION %s()`, name, pq.QuoteLiteral(ret), key, name, name))
	t.Cleanup(func() {
		opsExec(t, db, fmt.Sprintf(`DROP TRIGGER %s ON customer_returns; DROP FUNCTION %s()`, name, name))
	})
	pause, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pause.Rollback()
	if _, err = pause.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		t.Fatal(err)
	}
	refunded := make(chan error, 1)
	go func() {
		amount, _, e := repo.RefundCustomerReturn(ctx, ret, f.user)
		if e == nil && amount != 10000000 {
			e = fmt.Errorf("wrong principal %d", amount)
		}
		refunded <- e
	}()
	for {
		var waiting bool
		if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%UPDATE customer_returns%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	released := make(chan error, 1)
	go func() {
		ok, e := repo.releaseClosedReturnWindow(ctx, order)
		if e == nil && !ok {
			e = errors.New("verified refunded return failed release")
		}
		released <- e
	}()
	payout := make(chan error, 1)
	go func() {
		_, e := NewFinanceRepository(db).RequestStorePayout(ctx, f.store, "bank", "bank", 2000000)
		payout <- e
	}()
	fin005WaitOrderWriters(t, ctx, func(ctx context.Context, q string, args ...any) (int, error) {
		var n int
		e := db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n, e
	}, 1)
	if err = pause.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-refunded; err != nil {
		t.Fatal(err)
	}
	if err = <-released; err != nil {
		t.Fatal(err)
	}
	if err = <-payout; err == nil {
		t.Fatal("payout spent reversed seller profit")
	}
	balance, err := NewFinanceRepository(db).GetStoreBalance(ctx, f.store)
	if err != nil || balance.AvailableAmount != -8993000 {
		t.Fatalf("signed debt lost %+v %v", balance, err)
	}
	var wallet int64
	if err = db.QueryRow(`SELECT balance FROM customer_wallets WHERE customer_id=$1`, f.user).Scan(&wallet); err != nil || wallet != 10000000 {
		t.Fatalf("wallet principal %d %v", wallet, err)
	}
}

func TestFIN005VerdictAndReleaseSerializeSinglePair(t *testing.T) {
	repo, id, order := fin005Admitted(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	opsExec(t, repo.db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW()-INTERVAL '8 days',return_window_ends_at=NOW()-INTERVAL '1 hour',dropshipper_profit=7000 WHERE id=$1`, order)
	hold, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback()
	if _, err = hold.ExecContext(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, order); err != nil {
		t.Fatal(err)
	}
	verdict := make(chan error, 1)
	release := make(chan error, 1)
	go func() {
		_, e := repo.ResolveDisputeWithOutcome(ctx, id, DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "actor"})
		verdict <- e
	}()
	go func() { _, e := NewOrderRepository(repo.db).releaseClosedReturnWindow(ctx, order); release <- e }()
	fin005WaitOrderWriters(t, ctx, func(ctx context.Context, q string, args ...any) (int, error) {
		var n int
		e := repo.db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n, e
	}, 2)
	if err = hold.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-verdict; err != nil {
		t.Fatal(err)
	}
	if err = <-release; err != nil {
		t.Fatal(err)
	}
	var n int
	if err = repo.db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1 AND operator='system:delivery-lock-releaser'`, order).Scan(&n); err != nil || n != 2 {
		t.Fatalf("release pair %d %v", n, err)
	}
}
func TestFIN005CarrierWaitsForVerdictThenOwnsNewHold(t *testing.T) {
	repo, id, order := fin005Admitted(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	key := time.Now().UnixNano()
	name := "fin005_carrier_pause_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	opsExec(t, repo.db, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.dispute_id=%s THEN PERFORM pg_advisory_xact_lock(%d); END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE INSERT ON dispute_financial_decisions FOR EACH ROW EXECUTE FUNCTION %s()`, name, pq.QuoteLiteral(id), key, name, name))
	t.Cleanup(func() {
		opsExec(t, repo.db, fmt.Sprintf(`DROP TRIGGER %s ON dispute_financial_decisions; DROP FUNCTION %s()`, name, name))
	})
	hold, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback()
	if _, err = hold.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		t.Fatal(err)
	}
	verdict := make(chan error, 1)
	go func() {
		_, e := repo.ResolveDisputeWithOutcome(ctx, id, DisputeResolutionInput{Action: "release_to_seller", ResponsibleParty: "seller", ActorUserID: "actor"})
		verdict <- e
	}()
	for {
		var waiting bool
		if err = repo.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%INSERT INTO dispute_financial_decisions%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	carrier := make(chan error, 1)
	go func() { carrier <- NewOrderRepository(repo.db).UpdateOrderStatusFromCarrier(ctx, order, "failed") }()
	fin005WaitOrderWriters(t, ctx, func(ctx context.Context, q string, args ...any) (int, error) {
		var n int
		e := repo.db.QueryRowContext(ctx, q, args...).Scan(&n)
		return n, e
	}, 1)
	if err = hold.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-verdict; err != nil {
		t.Fatal(err)
	}
	if err = <-carrier; err != nil {
		t.Fatal(err)
	}
	if _, err = NewOrderRepository(repo.db).releaseClosedReturnWindow(ctx, order); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err = repo.db.QueryRow(`SELECT status='delivery_disputed' AND disputed_at<>(SELECT hold_at FROM dispute_order_evidence WHERE dispute_id=$2) FROM orders WHERE id=$1`, order, id).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("new carrier evidence lost %v %v", preserved, err)
	}
}
