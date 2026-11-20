package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

// FIN-013 §6.5 check 2 + the CAP-01 headroom anomaly.
//
// The anomaly: the CAP-01 headroom oracle counted
// paid_orders_without_ledger_entry = 6 and reported it as an
// unexplained discrepancy. It is not one. A merely-paid order has NO
// ledger entry by design — the first order-linked entry is written at
// staged profit release (return window close, >= 7 days after
// delivery), never at payment: MarkPaymentPaid writes no ledger entry
// and AddLedgerEntry has no production callers. The six were
// unreleased paid orders.
//
// This test pins that design in the repository package, and validates
// the mismatch definition the observability poller gauges
// (dependencies.go pollFinanceGauges): only a RELEASED order without
// its release entry, or a paid-cancelled order without a cancellation
// obligation, is a mismatch.
//
// Synchronization: sequential disposable-DB assertions on one order;
// no concurrency is involved, so no barrier is needed (audit §1 d).

// fin013MismatchForOrder is the per-order form of the gauge query:
// 1 when this order is a live mismatch, 0 otherwise. The gauge
// itself sums the same predicates over all orders; scoping to one
// order keeps this test independent of other tests' fixtures in the
// shared disposable DB.
func fin013MismatchForOrder(t *testing.T, db *sql.DB, order string) int {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM orders o WHERE o.id=$1 AND o.profit_released
			AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id))
		+ (SELECT COUNT(*) FROM orders o WHERE o.id=$1 AND o.payment_status='paid' AND o.status='cancelled'
			AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation'))`, order).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestFIN013PaidOrderHasNoLedgerEntry(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	ctx := context.Background()

	// 1. Merely-paid: no order-linked ledger entry exists, and the
	// mismatch gauge definition agrees (0).
	var entries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE related_order_or_dispute=$1`, order).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("paid order has %d ledger entries, want 0 (first entry is written at release, not at payment)", entries)
	}
	if got := fin013MismatchForOrder(t, db, order); got != 0 {
		t.Fatalf("merely-paid order is a mismatch (%d), want 0 — the CAP-01 headroom anomaly was this, not a discrepancy", got)
	}

	// 2. The same order RELEASED with no release entry IS a mismatch.
	// Direct UPDATEs of profit_released are fenced (FIN-005 writer
	// fence): the test sets the writer flag the app's release writer
	// sets (set_config transaction-local), exactly as the production
	// release path does — the flag names the writer, it does not
	// bypass any money rule.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('sellersho.fin005_writer','v1',true)`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET profit_released=TRUE, profit_released_at=NOW() WHERE id=$1`, order); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := fin013MismatchForOrder(t, db, order); got != 1 {
		t.Fatalf("released order without a release entry is a mismatch (%d), want 1", got)
	}

	// 3. Writing the release entry clears it — the gauge recovers.
	opsExec(t, db, `INSERT INTO ledger_entries(id,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,$2,'seller','credit',8500000,'آزادسازی سود فروشنده','fin013-test',$3)`, "REL-LED-"+uuid.NewString(), "store_payable:"+f.store, order)
	if got := fin013MismatchForOrder(t, db, order); got != 0 {
		t.Fatalf("released order with its release entry is a mismatch (%d), want 0", got)
	}

	// 4. Paid cancellation without an obligation would be a mismatch;
	// CancelOrder creates exactly one, so the count stays 0. (The
	// order is released, not cancelled — use a second paid order for
	// the cancel leg so the two mismatch arms are exercised
	// independently.)
	db2, f2, _, order2 := fin003PaidOrder(t)
	repo := NewOrderRepository(db2)
	if err := repo.CancelOrder(ctx, order2, f2.user, false); err != nil {
		t.Fatal(err)
	}
	if got := fin013MismatchForOrder(t, db2, order2); got != 0 {
		t.Fatalf("paid-cancelled order with its cancellation obligation is a mismatch (%d), want 0", got)
	}
	var obligations int
	var amount int64
	if err := db2.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE order_id=$1 AND purpose='cancellation'`, order2).Scan(&obligations, &amount); err != nil || obligations != 1 || amount != 10000000 {
		t.Fatalf("paid-cancelled obligations=%d amount=%d, want 1/10000000 (Toman)", obligations, amount)
	}

	// 5. The gauge SQL itself runs cleanly (shape check, not a global
	// zero assertion — other tests' fixtures share this DB).
	var global int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM orders o WHERE o.profit_released AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id)) + (SELECT COUNT(*) FROM orders o WHERE o.payment_status='paid' AND o.status='cancelled' AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation'))`).Scan(&global); err != nil {
		t.Fatalf("gauge mismatch query: %v", err)
	}
	t.Logf("global live-mismatch count at end of test: %d (informational; other fixtures share this DB)", global)
}
