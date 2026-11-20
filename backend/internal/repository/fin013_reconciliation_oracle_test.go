package repository

import (
	"context"
	"database/sql"
	"testing"
)

// FIN-013 §6.5 — the 10-point reconciliation oracle as a runnable Go
// check. The SQL form lives in scripts/fin013-oracle.sql (run it
// against the disposable DB before teardown after any load run); this
// test executes the same predicates against app-writer-seeded data
// and asserts zero difference.
//
// What is seeded here (all amounts Toman, via the app's own writers):
// a paid order that is then cancelled (checks 1-4), and the FIN-011
// dispute-then-wallet return path (check 7). Checks 5, 6, 8, 9 run
// globally over the disposable DB — in a single-test run that DB
// holds only this test's fixtures; in a full-package run they also
// cover other tests' fixtures, which must be consistent by their own
// assertions. Check 10 re-adds the statement builder's own totals
// (opening + credits - debits = closing; closing - reservations =
// signed available) via GetStoreStatement.
//
// Synchronization: sequential disposable-DB assertions; no
// concurrency is involved, so no barrier is needed (audit §1 d).

// fin013OracleCount runs one zero-difference query and returns its count.
func fin013OracleCount(t *testing.T, db *sql.DB, name, query string, args ...interface{}) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("oracle %s: %v", name, err)
	}
	return n
}

func TestFIN013ReconciliationOracle(t *testing.T) {
	// Verified-paid seed (not MarkPaymentPaid): the receipt leg of
	// check 1 only holds for payments that went through verification
	// (RecordPaymentVerification writes the receipt); a direct
	// MarkPaymentPaid has no receipt by construction — see the SQL
	// script's check-1 note.
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	repo := NewOrderRepository(db)
	if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}

	// 1. orders_payments: the paid order has exactly one active
	// payment row and one receipt; payable equals the callback amount.
	if n := fin013OracleCount(t, db, "orders_payments", `SELECT COUNT(*) FROM orders o WHERE o.id=$1 AND o.payment_status='paid' AND ((SELECT COUNT(*) FROM payment_transactions p WHERE p.order_id=o.id AND p.status='paid')<>1 OR (SELECT COUNT(*) FROM order_payment_receipts r WHERE r.order_id=o.id)<>1 OR o.payable_amount<>(SELECT p.amount FROM payment_transactions p WHERE p.order_id=o.id AND p.status='paid' LIMIT 1))`, order); n != 0 {
		t.Errorf("oracle orders_payments difference=%d, want 0", n)
	}
	// 2. payments_ledger: the order is NOT released, so no ledger
	// entry is expected (by design — see TestFIN013PaidOrderHasNoLedgerEntry).
	if n := fin013OracleCount(t, db, "payments_ledger", `SELECT COUNT(*) FROM orders o WHERE o.id=$1 AND o.profit_released AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id)`, order); n != 0 {
		t.Errorf("oracle payments_ledger difference=%d, want 0", n)
	}
	// 3. cancellations_obligations: exactly one cancellation obligation.
	if n := fin013OracleCount(t, db, "cancellations_obligations", `SELECT COUNT(*) FROM orders o WHERE o.id=$1 AND o.payment_status='paid' AND o.status='cancelled' AND (SELECT COUNT(*) FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')<>1`, order); n != 0 {
		t.Errorf("oracle cancellations_obligations difference=%d, want 0", n)
	}
	// 4. obligations_outcomes: cumulative refunds <= captured; every
	// obligation terminal or in the unknown queue.
	if n := fin013OracleCount(t, db, "obligations_outcomes", `SELECT COUNT(*) FROM orders o WHERE o.id=$1 AND ((SELECT COALESCE(SUM(r.amount_toman),0) FROM order_refund_obligations r WHERE r.order_id=o.id) > COALESCE(o.payable_amount,0) OR EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=r.id ORDER BY e.event_no DESC LIMIT 1), r.status) NOT IN ('succeeded','failed','unknown','pending')))`, order); n != 0 {
		t.Errorf("oracle obligations_outcomes difference=%d, want 0", n)
	}
	// 5. settlement: no duplicate reservation per period/store; cycle
	// stored totals equal the sum of their items.
	if n := fin013OracleCount(t, db, "settlement", `SELECT COUNT(*) FROM (SELECT cycle_period, store_id FROM monthly_settlement_items GROUP BY cycle_period, store_id HAVING COUNT(*)>1 UNION ALL SELECT c.period, '' FROM monthly_settlement_cycles c WHERE c.total_gross<>(SELECT COALESCE(SUM(i.gross_amount),0) FROM monthly_settlement_items i WHERE i.cycle_period=c.period) OR c.total_net<>(SELECT COALESCE(SUM(i.net_amount),0) FROM monthly_settlement_items i WHERE i.cycle_period=c.period) OR c.total_stores<>(SELECT COUNT(*) FROM monthly_settlement_items i WHERE i.cycle_period=c.period)) s`); n != 0 {
		t.Errorf("oracle settlement difference=%d, want 0", n)
	}
	// 6. coupons: used_count equals the occupying reservations.
	if n := fin013OracleCount(t, db, "coupons", `SELECT COUNT(*) FROM seller_discount_codes c WHERE c.used_count<>(SELECT COUNT(*) FROM order_coupon_reservations r WHERE r.coupon_id=c.id AND r.state IN ('reserved','consumed','review_hold'))`); n != 0 {
		t.Errorf("oracle coupons difference=%d, want 0", n)
	}
	// 8. storage: no duplicate charge per (position, day); no
	// non-positive charge. (The per-lot integral is asserted by the
	// FIN-006 tests, not re-derived here — see the SQL script.)
	if n := fin013OracleCount(t, db, "storage", `SELECT COUNT(*) FROM (SELECT position_id, fee_date FROM warehouse_storage_fee_charges GROUP BY position_id, fee_date HAVING COUNT(*)>1 UNION ALL SELECT id, NULL FROM warehouse_storage_fee_charges WHERE amount_toman<=0 OR qty<=0) s`); n != 0 {
		t.Errorf("oracle storage difference=%d, want 0", n)
	}
	// 9. treasury: no negative payable; unknown obligations never sum negative.
	if n := fin013OracleCount(t, db, "treasury", `SELECT COUNT(*) FROM (SELECT 1 WHERE EXISTS (SELECT 1 FROM orders o WHERE o.payment_status='paid' AND o.status<>'cancelled' AND o.payable_amount<0) UNION ALL SELECT 1 WHERE (SELECT COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE status='unknown')<0) s`); n != 0 {
		t.Errorf("oracle treasury difference=%d, want 0", n)
	}

	// 7. referral — the KNOWN asymmetry case. Build the
	// dispute-then-wallet return path (same steps as
	// TestFIN009AuditMixedDisputeThenWalletCountsOnce): release,
	// 25% dispute refund (2,500,000 of 10,000,000), then a 50%
	// wallet return of one unit (5,000,000).
	db7, f7, warehouse7, order7, _ := fin011ReferralOrder(t, true)
	fin011Confirm(t, db7, order7, f7.user)
	fin011CloseWindow(t, db7, order7)
	if released := fin011Release(t, db7, order7); !released {
		t.Fatalf("referral setup: release did not run")
	}
	fin009ResolveDispute(t, db7, f7, order7, 2500000)
	ret := fin003ReceivedReturn(t, db7, f7, warehouse7, order7)
	if amount, _, err := NewOrderRepository(db7).RefundCustomerReturn(ctx, ret, f7.user); err != nil || amount != 5000000 {
		t.Fatalf("referral setup: wallet refund amount=%d err=%v, want 5000000", amount, err)
	}
	// The FIN-011 follow-up fix has NOT landed: the return path
	// counts return_items only, so the reversal totals 5,000, not
	// the conserved 7,500. Pin the observed number (do NOT flip this
	// to 7500 silently — a fix flips it deliberately).
	if got := fin011ReversalSum(t, db7, order7); got != 5000 {
		t.Errorf("oracle referral: reversal sum=%d, want 5000 (observed pre-existing FIN-011 asymmetry; 7500 is the conserved value the follow-up fix must produce)", got)
	}
	// The release gate MUST flag it: referralReversalCompleteTx is
	// false, i.e. the order would land in needs_review with
	// referral_reversal_incomplete before any release.
	tx, err := db7.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := referralReversalCompleteTx(ctx, tx, order7)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Rollback()
	if complete {
		t.Errorf("oracle referral: release gate says complete, want incomplete (the 2,500 Toman/case undercount must be flagged, not released)")
	}
	t.Logf("oracle referral: asymmetry case present (reversal 5000 vs conserved 7500, undercount 2500 Toman); release gate flags it (complete=false); FIN-011 follow-up fix NOT landed")

	// 10. statements: re-add the builder's own totals for the
	// fixture store in the current Tehran month.
	cur, _, _, _ := fin009Months()
	stmt, err := NewFinanceRepository(db).GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if stmt.NetBalance != stmt.TotalCredit-stmt.TotalDebit {
		t.Errorf("oracle statements: net=%d, want credit-debit=%d (credit=%d debit=%d)", stmt.NetBalance, stmt.TotalCredit-stmt.TotalDebit, stmt.TotalCredit, stmt.TotalDebit)
	}
	if stmt.ClosingAvailable != stmt.OpeningAvailable+stmt.NetBalance {
		t.Errorf("oracle statements: closing=%d, want opening+net=%d (opening=%d net=%d)", stmt.ClosingAvailable, stmt.OpeningAvailable+stmt.NetBalance, stmt.OpeningAvailable, stmt.NetBalance)
	}
	if stmt.CurrentAvailable != stmt.ClosingAvailable-stmt.ClosingReserved {
		t.Errorf("oracle statements: current=%d, want closing-reserved=%d (closing=%d reserved=%d)", stmt.CurrentAvailable, stmt.ClosingAvailable-stmt.ClosingReserved, stmt.ClosingAvailable, stmt.ClosingReserved)
	}
	_ = warehouse
}
