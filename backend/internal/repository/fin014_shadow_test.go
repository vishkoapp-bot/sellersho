package repository

import (
	"context"
	"encoding/json"
	"testing"
)

// FIN-014 W3 — shadow-read rehearsal (audit §7).
//
// The W1 report, the FIN-013 oracle checks, and the treasury /
// statement builders run over the SAME W2 fixture snapshot, and the
// diff is recorded as the first fin014_report_runs entry — in-test
// here, because the fin014_report_runs table does not exist until W4;
// W4's migration test persists the same shape.
//
// Scoped oracle expectations for one fixture copy (prefix f14w-,
// cycle 2026-10):
//   check 1 orders_payments     = 5 (c2m, c2u, c8a, c8b, c8c — the
//                                seeded paid-without-receipt shapes)
//   check 2 payments_ledger     = 0 (every released order has a
//                                ledger entry)
//   check 3 cancellations       = 1 (c2m: missing obligation)
//   check 4 obligations_outcomes = 0
//   check 5 settlement          = 0 (cycle totals consistent)
//   check 6 coupons             = 1 (coupon-2 drift, scoped to the
//                                f14w- copy — the oracle itself is
//                                global; see the scoping note below)
//   check 7 referral            = 0 in fixture scope; the pinned
//                                FIN-011 asymmetry comes separately
//                                from the fin011 helpers (see
//                                TestFIN013ReconciliationOracle) and
//                                is recorded via report.FIN011Pinned
//   check 8 storage             = 0
//   check 9 treasury            = 0
//   check 10 statements         = builder identities hold for
//                                store-a month 2026-10 and
//                                OpeningVerified is false (FIN-014
//                                owns the cutover record that could
//                                set it true) — agreeing with the
//                                report's ambiguous opening row.
//
// The report and the builders must agree on totals: the statement
// identities above plus the report's per-class counts.

func TestFIN014ShadowRehearsal(t *testing.T) {
	db := fin014FixtureDB(t)
	unresolvedLot, _ := loadFIN014Fixture(t, db, "f14w-", "2026-10")
	ctx := context.Background()

	report := fin014ReportForFixture(t, db, "f14w-")
	assertFIN014ExpectedCounts(t, report)
	lotReport := fin014ReportForFixture(t, db, unresolvedLot)
	if lotReport.TotalRows != 1 {
		t.Fatalf("shadow: unresolved lot report rows=%d, want 1", lotReport.TotalRows)
	}
	if !report.FIN011Pinned {
		t.Fatalf("shadow: report must carry the pinned FIN-011 asymmetry")
	}

	// Oracle checks, scoped to the fixture prefix where the oracle
	// predicate is per-order; checks 8/9 are global in the oracle
	// itself and stay global (their shapes are per-copy-unique, so
	// other copies cannot move them).
	count := func(name, q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatalf("shadow oracle %s: %v", name, err)
		}
		return n
	}
	checks := map[string]int64{}
	checks["orders_payments"] = count("orders_payments", `SELECT COUNT(*) FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND ((SELECT COUNT(*) FROM payment_transactions p WHERE p.order_id=o.id AND p.status='paid')<>1 OR (SELECT COUNT(*) FROM order_payment_receipts r WHERE r.order_id=o.id)<>1 OR o.payable_amount<>(SELECT p.amount FROM payment_transactions p WHERE p.order_id=o.id AND p.status='paid' LIMIT 1))`, "f14w-")
	checks["payments_ledger"] = count("payments_ledger", `SELECT COUNT(*) FROM orders o WHERE o.id LIKE $1||'%' AND o.profit_released AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id)`, "f14w-")
	checks["cancellations_obligations"] = count("cancellations_obligations", `SELECT COUNT(*) FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND o.status='cancelled' AND (SELECT COUNT(*) FROM order_refund_obligations r WHERE r.order_id=o.id AND r.purpose='cancellation')<>1`, "f14w-")
	checks["obligations_outcomes"] = count("obligations_outcomes", `SELECT COUNT(*) FROM orders o WHERE o.id LIKE $1||'%' AND ((SELECT COALESCE(SUM(r.amount_toman),0) FROM order_refund_obligations r WHERE r.order_id=o.id) > COALESCE(o.payable_amount,0) OR EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=o.id AND COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=r.id ORDER BY e.event_no DESC LIMIT 1), r.status) NOT IN ('succeeded','failed','unknown','pending')))`, "f14w-")
	// Checks 5/6 are global in the FIN-013 oracle itself, so this
	// rehearsal scopes their item/coupon sums to the f14w- copy:
	// several FIN-014 tests load their own fixture copy into the
	// same test DB, and the shared 2026-10 cycle row carries one
	// copy's totals. Scoping keeps the rehearsal order-independent
	// while still proving the report, the oracle predicates, and the
	// builders agree on THIS snapshot.
	checks["settlement"] = count("settlement", `SELECT COUNT(*) FROM (SELECT cycle_period, store_id FROM monthly_settlement_items WHERE cycle_period=$1 AND id LIKE $2||'%' GROUP BY cycle_period, store_id HAVING COUNT(*)>1 UNION ALL SELECT c.period, '' FROM monthly_settlement_cycles c WHERE c.period=$1 AND (c.total_gross<>(SELECT COALESCE(SUM(i.gross_amount),0) FROM monthly_settlement_items i WHERE i.cycle_period=c.period AND i.id LIKE $2||'%') OR c.total_net<>(SELECT COALESCE(SUM(i.net_amount),0) FROM monthly_settlement_items i WHERE i.cycle_period=c.period AND i.id LIKE $2||'%') OR c.total_stores<>(SELECT COUNT(*) FROM monthly_settlement_items i WHERE i.cycle_period=c.period AND i.id LIKE $2||'%'))) s`, "2026-10", "f14w-")
	checks["coupons"] = count("coupons", `SELECT COUNT(*) FROM seller_discount_codes c WHERE c.id LIKE $1||'%' AND c.used_count<>(SELECT COUNT(*) FROM order_coupon_reservations r WHERE r.coupon_id=c.id AND r.state IN ('reserved','consumed','review_hold'))`, "f14w-")
	checks["storage"] = count("storage", `SELECT COUNT(*) FROM (SELECT position_id, fee_date FROM warehouse_storage_fee_charges GROUP BY position_id, fee_date HAVING COUNT(*)>1 UNION ALL SELECT id, NULL FROM warehouse_storage_fee_charges WHERE amount_toman<=0 OR qty<=0) s`)
	checks["treasury"] = count("treasury", `SELECT COUNT(*) FROM (SELECT 1 WHERE EXISTS (SELECT 1 FROM orders o WHERE o.payment_status='paid' AND o.status<>'cancelled' AND o.payable_amount<0) UNION ALL SELECT 1 WHERE (SELECT COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE status='unknown')<0) s`)
	// Check 7 in fixture scope: no referral rows at all for fixture
	// orders. The pinned asymmetry itself is exercised by the fin011
	// helpers in TestFIN013ReconciliationOracle and recorded here via
	// report.FIN011Pinned — never by editing the oracle to pass.
	checks["referral_fixture_scope"] = count("referral_fixture_scope", `SELECT COUNT(*) FROM referral_commission_reversals r JOIN orders o ON o.id=r.order_id WHERE o.id LIKE $1||'%'`, "f14w-")

	want := map[string]int64{
		"orders_payments": 5, "payments_ledger": 0,
		"cancellations_obligations": 1, "obligations_outcomes": 0,
		"settlement": 0, "coupons": 1, "storage": 0, "treasury": 0,
		"referral_fixture_scope": 0,
	}
	for name, w := range want {
		if checks[name] != w {
			t.Errorf("shadow oracle %s = %d, want %d", name, checks[name], w)
		}
	}
	// Check 1 identities: the five flagged orders are exactly the
	// seeded paid-without-receipt shapes.
	rows, err := db.Query(`SELECT o.id FROM orders o WHERE o.id LIKE $1||'%' AND o.payment_status='paid' AND ((SELECT COUNT(*) FROM payment_transactions p WHERE p.order_id=o.id AND p.status='paid')<>1 OR (SELECT COUNT(*) FROM order_payment_receipts r WHERE r.order_id=o.id)<>1 OR o.payable_amount<>(SELECT p.amount FROM payment_transactions p WHERE p.order_id=o.id AND p.status='paid' LIMIT 1)) ORDER BY o.id`, "f14w-")
	if err != nil {
		t.Fatal(err)
	}
	var flagged []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		flagged = append(flagged, id)
	}
	rows.Close()
	wantFlagged := []string{"f14w-order-c2m", "f14w-order-c2u", "f14w-order-c8a", "f14w-order-c8b", "f14w-order-c8c"}
	if len(flagged) != len(wantFlagged) {
		t.Fatalf("shadow check-1 flagged=%v, want %v", flagged, wantFlagged)
	}
	for i := range wantFlagged {
		if flagged[i] != wantFlagged[i] {
			t.Errorf("shadow check-1 flagged[%d]=%s, want %s", i, flagged[i], wantFlagged[i])
		}
	}

	// Check 10: the statement builder agrees with the report for
	// store-a month 2026-10 — identities hold, opening unverified
	// (the report carries the same opening as an ambiguous row).
	stmt, err := NewFinanceRepository(db).GetStoreStatement(ctx, "f14w-store-a", "2026-10")
	if err != nil {
		t.Fatalf("shadow statements: %v", err)
	}
	if stmt.NetBalance != stmt.TotalCredit-stmt.TotalDebit {
		t.Errorf("shadow statements: net=%d, want credit-debit=%d", stmt.NetBalance, stmt.TotalCredit-stmt.TotalDebit)
	}
	if stmt.ClosingAvailable != stmt.OpeningAvailable+stmt.NetBalance {
		t.Errorf("shadow statements: closing=%d, want opening+net=%d", stmt.ClosingAvailable, stmt.OpeningAvailable+stmt.NetBalance)
	}
	// CurrentAvailable is the LIVE balance, not the month-end cutoff:
	// it also reserves payout-1 (pending 1,500,000, created after the
	// October month-end), so current = closing - reserved - that
	// post-month pending payout. No store-a seller ledger entry
	// postdates the month, so the month net is the live net.
	var postMonthPending int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM payout_requests WHERE store_id=$1 AND status IN ('pending','approved') AND created_at >= '2026-11-01'::timestamptz`, "f14w-store-a").Scan(&postMonthPending); err != nil {
		t.Fatal(err)
	}
	if postMonthPending != 1500000 {
		t.Errorf("shadow statements: post-month pending payouts=%d, want 1500000 (payout-1)", postMonthPending)
	}
	var postMonthLedger int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE target_account=$1 AND target_type='seller' AND date >= '2026-11-01'::timestamptz`, "f14w-store-a").Scan(&postMonthLedger); err != nil {
		t.Fatal(err)
	}
	if postMonthLedger != 0 {
		t.Errorf("shadow statements: %d store-a ledger entries postdate the month, want 0", postMonthLedger)
	}
	if stmt.CurrentAvailable != stmt.ClosingAvailable-stmt.ClosingReserved-postMonthPending {
		t.Errorf("shadow statements: current=%d, want closing-reserved-postMonthPending=%d (closing=%d reserved=%d postMonth=%d)", stmt.CurrentAvailable, stmt.ClosingAvailable-stmt.ClosingReserved-postMonthPending, stmt.ClosingAvailable, stmt.ClosingReserved, postMonthPending)
	}
	if stmt.OpeningVerified {
		t.Errorf("shadow statements: OpeningVerified=true, want false (FIN-014 owns the cutover record)")
	}
	openingRow := false
	for _, r := range report.Rows {
		if r.RecordID == "f14w-store-a:2026-10" && r.Class == FIN014ClassStatementMismatch && r.Confidence == FIN014ConfidenceAmbiguous {
			openingRow = true
		}
	}
	if !openingRow {
		t.Errorf("shadow: report has no ambiguous opening row for f14w-store-a:2026-10, disagreeing with the builder")
	}

	// First fin014_report_runs entry, in-test (pre-schema): the run
	// record W4 will persist. Assert its shape here so the migration
	// test can reuse it verbatim.
	run := map[string]any{
		"prefix": "f14w-", "cyclePeriod": "2026-10", "schemaVersion": 212,
		"totalRows": report.TotalRows, "certain": report.CertainCount,
		"ambiguous": report.AmbiguousCount, "unresolved": report.UnresolvedCount,
		"oracleChecks": checks, "fin011Pinned": report.FIN011Pinned,
		"openingVerified": stmt.OpeningVerified,
	}
	raw, _ := json.Marshal(run)
	t.Logf("shadow run record (pre-schema fin014_report_runs): %s", raw)
}
