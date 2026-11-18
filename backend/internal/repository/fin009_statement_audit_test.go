// FIN-009 statement audit — reproduction tests, re-scoped to the FIXED
// behaviour by the fix leg (see ~/workspace/fin009-work/fin009-fix.md).
//
// Each TestFIN009Audit* case drives the REAL repository methods against
// the FULL migrated schema in a disposable DB (provisioner:
// ~/workspace/.dev/test-isolated-native.sh). Amounts are Toman.
//
// Cases assert the FIN-009 acceptance bullets and the frozen gates
// G-01..G-13 (fin009-audit.md §5): G-01..G-07 are the regression guards
// (verified-correct behaviours from the audit), G-08 is the D-01
// dispute-refund reversal (full + partial + before-release), G-09 is
// the D-02 fixture fix (the two FIN-001 statement tests, in
// finance_balance_integration_test.go), G-10 is D-03 stable paging,
// G-11 is D-04 Tehran month, G-12 is D-05 the explicit opening marker,
// G-13 is D-06 the pinned fail-closed undated behaviour. Round 2
// (fin009-fix.md §12) adds the mixed-path sub-cap: (e2d) wallet return
// then dispute refund and (e2e) the reverse order, each refunded value
// counted once in the SPR cumulative rule.
//
// Fixture economics: fin003VerifiedPaidOrder gives a paid 10,000,000
// order; fin009ReshapeProfit2M reshapes it to seller profit exactly
// 2,000,000 (wholesale 7,000,000 + margin 2,000,000 + platform profit
// 1,000,000) so a full return refunds 10,000,000 to the customer while
// the seller statement must show +2,000,000 / -2,000,000, net 0.
//
// The early-withdrawal approval step has NO repository method: approval
// lives in the cmd/server handler (finance_parity_handlers.go:648-660).
// fin009ApproveEarlyWithdrawal replicates that handler's two SQL
// statements verbatim (INSERT net payout 'approved' + UPDATE withdrawal
// 'approved'); the confirmation step uses the real
// ConfirmManualTransfer repository method.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fin009ReshapeProfit2M reshapes a fin003 10,000,000 order to seller
// profit exactly 2,000,000 while keeping the 10,000,000 payable intact
// (so payment facts stay consistent).
func fin009ReshapeProfit2M(t *testing.T, db *sql.DB, order string) {
	t.Helper()
	opsExec(t, db, `UPDATE order_items SET wholesale_cost=7000000,seller_margin=2000000,final_unit_price=10000000 WHERE order_id=$1`, order)
	opsExec(t, db, `UPDATE orders SET dropshipper_profit=2000000,platform_profit=1000000 WHERE id=$1`, order)
}

// fin009Release2M confirms, closes the window and runs the real release
// for a reshaped order. Returns true iff the release ran.
func fin009Release2M(t *testing.T, db *sql.DB, f opsFixture, order string) bool {
	t.Helper()
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	return fin011Release(t, db, order)
}

// fin009Months returns the current UTC month string, the previous UTC
// month string, and mid-month instants in each (for date pinning).
func fin009Months() (cur, prev string, prevMid, curMid time.Time) {
	now := time.Now().UTC()
	curStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	prevStart := curStart.AddDate(0, -1, 0)
	return curStart.Format("2006-01"), prevStart.Format("2006-01"),
		prevStart.AddDate(0, 0, 14), curStart.AddDate(0, 0, 14)
}

// fin009CountReason counts statement entries with the given displayed
// reason, split by direction, and sums their amounts.
func fin009CountReason(stmt *StoreStatement, reason string) (credits, debits, n int, creditSum, debitSum int64) {
	for _, e := range stmt.Entries {
		if e.Reason != reason {
			continue
		}
		n++
		if e.Type == "credit" {
			credits++
			creditSum += e.Amount
		} else {
			debits++
			debitSum += e.Amount
		}
	}
	return credits, debits, n, creditSum, debitSum
}

// fin009ApproveEarlyWithdrawal replicates the approval handler's two SQL
// statements verbatim (cmd/server/finance_parity_handlers.go:648-660):
// a payout_requests row for the NET amount in status 'approved', and the
// withdrawal row flipped to 'approved'. The handler also takes the
// account lock first; fixture replication omits the lock (single writer).
func fin009ApproveEarlyWithdrawal(t *testing.T, db *sql.DB, ewID, storeID, storeName, shaba, bankName string, net int64) string {
	t.Helper()
	payoutID := "pay-req-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO payout_requests (id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status, available_balance, created_at)
		VALUES ($1, $2, 'seller', $3, $4, $5, $6, 'approved', $6, NOW())`,
		payoutID, storeID, storeName, shaba, bankName, net)
	opsExec(t, db, `UPDATE seller_early_withdrawals SET status = 'approved', payout_request_id = $1, processed_at = NOW() WHERE id = $2`,
		payoutID, ewID)
	return payoutID
}

// fin009AdjustmentLegs inserts one signed finance adjustment exactly as
// the POST /finance/adjustments handler does
// (cmd/server/finance_adjustments_handlers.go:166-188): the payable leg
// (store_payable:<id>, direction) plus the platform counterpart leg
// (target_account 'platform', opposite direction), sharing one docId and
// the 'سند تعدیل ' reason prefix.
func fin009AdjustmentLegs(t *testing.T, db *sql.DB, storeID, docID, direction string, amount int64) {
	t.Helper()
	counter := "credit"
	if direction == "credit" {
		counter = "debit"
	}
	opsExec(t, db, `INSERT INTO finance_adjustments(id,target_kind,target_id,direction,amount,reason,operator,request_key,request_fingerprint) VALUES($1,'seller',$2,$3,$4,'fin009 audit','tester',$5,$6)`,
		docID, storeID, direction, amount, docID, "fp-"+docID)
	for _, leg := range []struct{ account, typ, dir string }{
		{"store_payable:" + storeID, "seller", direction},
		{"platform", "platform", counter},
	} {
		opsExec(t, db, `INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
			VALUES ($1, NOW(), $2, $3, $4, $5, $6, 'tester', $7)`,
			"ADJ-LED-"+uuid.NewString(), leg.account, leg.typ, leg.dir, amount, "سند تعدیل fin009 audit", docID)
	}
}

// (a) Acceptance bullet 1: 2m profit then full return shows +2m and -2m,
// net 0. The -2m leg is the seller_profit customer_return_financial_event
// (operation 'debit'), dated at refund time; the order row is NOT mutated
// by the refund (status stays delivered_confirmed).
func TestFIN009AuditProfit2MFullReturn(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	fin009ReshapeProfit2M(t, db, order)
	if released := fin009Release2M(t, db, f, order); !released {
		t.Fatalf("setup: release did not run")
	}
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, f.user); err != nil || amount != 10000000 {
		t.Fatalf("setup: full refund amount=%d err=%v", amount, err)
	}
	var status string
	var releasedAt sql.NullTime
	if err := db.QueryRow(`SELECT status,profit_released_at FROM orders WHERE id=$1`, order).Scan(&status, &releasedAt); err != nil {
		t.Fatal(err)
	}
	if status != "delivered_confirmed" || !releasedAt.Valid {
		t.Fatalf("setup: refund mutated the order row: status=%s released_at valid=%v", status, releasedAt.Valid)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := NewFinanceRepository(db).GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatalf("GetStoreStatement: %v", err)
	}
	if stmt.TotalCredit != 2000000 || stmt.TotalDebit != 2000000 || stmt.NetBalance != 0 {
		t.Errorf("(a) 2m profit/full return: credit=%d debit=%d net=%d, want 2000000/2000000/0; entries=%+v",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance, stmt.Entries)
	}
	if _, _, n, cs, _ := fin009CountReason(stmt, "سود آزادشده سفارش"); n != 1 || cs != 2000000 {
		t.Errorf("(a) released-profit entries=%d sum=%d, want 1/2000000", n, cs)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "برگشت سود فروشنده"); n != 1 || ds != 2000000 {
		t.Errorf("(a) seller-return entries=%d sum=%d, want 1/2000000", n, ds)
	}
	if stmt.OpeningAvailable != 0 || stmt.ClosingAvailable != 0 || stmt.CurrentAvailable != 0 {
		t.Errorf("(a) opening/closing/current=%d/%d/%d, want 0/0/0", stmt.OpeningAvailable, stmt.ClosingAvailable, stmt.CurrentAvailable)
	}
}

// (b) Acceptance bullet 2, same month: early withdrawal gross 1m, fee
// 50k, net payout 950k reconciles to exactly 1m total reduction.
// Lifecycle: pending (gross reserved) -> approved (fee posted, net
// reserved) -> transfer-confirmed (net posted, reservation released).
func TestFIN009AuditEarlyWithdrawalFullLifecycle(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin009ReshapeProfit2M(t, db, order)
	if released := fin009Release2M(t, db, f, order); !released {
		t.Fatalf("setup: release did not run")
	}
	opsExec(t, db, `UPDATE platform_settings SET early_withdrawal_fee_pct=5 WHERE id=1`)
	ew, err := finRepo.RequestEarlyWithdrawal(ctx, f.user, f.store, 1000000, "IR820170000000123456789012", "بانک ملی", "fin009-t02")
	if err != nil {
		t.Fatalf("RequestEarlyWithdrawal: %v", err)
	}
	if ew.FeeAmount != 50000 || ew.NetAmount != 950000 || ew.Status != "pending" {
		t.Fatalf("setup: fee=%d net=%d status=%s, want 50000/950000/pending", ew.FeeAmount, ew.NetAmount, ew.Status)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if stmt.ClosingReserved != 1000000 || stmt.ClosingAvailable != 1000000 {
		t.Errorf("(b) pending: reserved=%d available=%d, want 1000000/1000000 (gross reserved, nothing posted)", stmt.ClosingReserved, stmt.ClosingAvailable)
	}
	payoutID := fin009ApproveEarlyWithdrawal(t, db, ew.ID, f.store, "fin009 store", "IR820170000000123456789012", "بانک ملی", 950000)
	stmt, err = finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "کارمزد برداشت زودهنگام"); n != 1 || ds != 50000 {
		t.Errorf("(b) approved: early-fee entries=%d sum=%d, want 1/50000", n, ds)
	}
	if _, _, n, _, _ := fin009CountReason(stmt, "تسویه و واریز به حساب شبا"); n != 0 {
		t.Errorf("(b) approved: payout posted early (%d entries) — an approved payout must stay a reservation", n)
	}
	if stmt.ClosingReserved != 950000 || stmt.ClosingAvailable != 1000000 {
		t.Errorf("(b) approved: reserved=%d available=%d, want 950000/1000000 (fee posted, net reserved)", stmt.ClosingReserved, stmt.ClosingAvailable)
	}
	var payoutAmount int64
	if err := db.QueryRow(`SELECT amount FROM payout_requests WHERE id=$1`, payoutID).Scan(&payoutAmount); err != nil || payoutAmount != 950000 {
		t.Fatalf("(b) payout_requests.amount=%d, want 950000 (net, not gross 1000000)", payoutAmount)
	}
	if err := finRepo.ConfirmManualTransfer(ctx, payoutID, "TRF-fin009-t02", "admin"); err != nil {
		t.Fatalf("ConfirmManualTransfer: %v", err)
	}
	stmt, err = finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if stmt.TotalCredit != 2000000 || stmt.TotalDebit != 1000000 || stmt.NetBalance != 1000000 {
		t.Errorf("(b) confirmed: credit=%d debit=%d net=%d, want 2000000/1000000/1000000 (fee 50k + net payout 950k = exactly 1m reduction)",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "تسویه و واریز به حساب شبا"); n != 1 || ds != 950000 {
		t.Errorf("(b) confirmed: payout entries=%d sum=%d, want 1/950000", n, ds)
	}
	if stmt.ClosingReserved != 0 || stmt.ClosingAvailable != 1000000 || stmt.CurrentAvailable != 1000000 {
		t.Errorf("(b) confirmed: reserved=%d closing=%d current=%d, want 0/1000000/1000000", stmt.ClosingReserved, stmt.ClosingAvailable, stmt.CurrentAvailable)
	}
}

// (b) cross-month: approved in month N (fee posts in N, net reserved),
// transfer-confirmed in month N+1 (net posts in N+1, fee NOT re-posted).
// Month N's statement must be unchanged by the N+1 confirmation.
func TestFIN009AuditEarlyWithdrawalCrossMonth(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin009ReshapeProfit2M(t, db, order)
	if released := fin009Release2M(t, db, f, order); !released {
		t.Fatalf("setup: release did not run")
	}
	opsExec(t, db, `UPDATE platform_settings SET early_withdrawal_fee_pct=5 WHERE id=1`)
	ew, err := finRepo.RequestEarlyWithdrawal(ctx, f.user, f.store, 1000000, "IR820170000000123456789012", "بانک ملی", "fin009-t02b")
	if err != nil {
		t.Fatalf("RequestEarlyWithdrawal: %v", err)
	}
	payoutID := fin009ApproveEarlyWithdrawal(t, db, ew.ID, f.store, "fin009 store", "IR820170000000123456789012", "بانک ملی", 950000)
	cur, prev, prevMid, _ := fin009Months()
	// Pin the approval into the previous month (fee + payout creation).
	opsExec(t, db, `UPDATE seller_early_withdrawals SET created_at=$1,processed_at=$1 WHERE id=$2`, prevMid, ew.ID)
	opsExec(t, db, `UPDATE payout_requests SET created_at=$1 WHERE id=$2`, prevMid, payoutID)
	// The profit release itself is in the current month; pin it back too
	// so the previous month carries the opening profit.
	opsExec(t, db, `UPDATE orders SET profit_released_at=$1 WHERE id=$2`, prevMid, order)
	prevStmt, err := finRepo.GetStoreStatement(ctx, f.store, prev)
	if err != nil {
		t.Fatal(err)
	}
	if prevStmt.TotalCredit != 2000000 || prevStmt.TotalDebit != 50000 || prevStmt.ClosingReserved != 950000 || prevStmt.ClosingAvailable != 1000000 {
		t.Errorf("(b) prev month: credit=%d debit=%d reserved=%d closing=%d, want 2000000/50000/950000/1000000",
			prevStmt.TotalCredit, prevStmt.TotalDebit, prevStmt.ClosingReserved, prevStmt.ClosingAvailable)
	}
	if err := finRepo.ConfirmManualTransfer(ctx, payoutID, "TRF-fin009-t02b", "admin"); err != nil {
		t.Fatalf("ConfirmManualTransfer: %v", err)
	}
	prevAgain, err := finRepo.GetStoreStatement(ctx, f.store, prev)
	if err != nil {
		t.Fatal(err)
	}
	if prevAgain.TotalCredit != prevStmt.TotalCredit || prevAgain.TotalDebit != prevStmt.TotalDebit ||
		prevAgain.ClosingAvailable != prevStmt.ClosingAvailable || prevAgain.ClosingReserved != prevStmt.ClosingReserved ||
		len(prevAgain.Entries) != len(prevStmt.Entries) {
		t.Errorf("(b) prev-month statement CHANGED after N+1 confirmation: before=%+v after=%+v", prevStmt, prevAgain)
	}
	curStmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if curStmt.TotalCredit != 0 || curStmt.TotalDebit != 950000 || curStmt.OpeningAvailable != 1000000 ||
		curStmt.OpeningReserved != 950000 || curStmt.ClosingAvailable != 1000000 || curStmt.ClosingReserved != 0 {
		t.Errorf("(b) cur month: credit=%d debit=%d opening=%d/%d closing=%d/%d, want 0/950000/1000000/950000/1000000/0",
			curStmt.TotalCredit, curStmt.TotalDebit, curStmt.OpeningAvailable, curStmt.OpeningReserved, curStmt.ClosingAvailable, curStmt.ClosingReserved)
	}
	if _, _, n, _, _ := fin009CountReason(curStmt, "کارمزد برداشت زودهنگام"); n != 0 {
		t.Errorf("(b) cur month: fee re-posted (%d entries) — fee must post once, in the approval month", n)
	}
}

// (c) Referral terms + payout appear ONCE: the FIN-011 referral reversal
// ledger leg (RCR-LED-, target_account = bare store id, reason
// 'برگشت کمیسیون معرفی — ...') and the payout ledger leg (PAY-LED-,
// reason 'تسویه پایا بانکی تایید شده') must NOT match the
// finance_adjustment filter (target_account 'store_payable:<id>' AND
// reason LIKE 'سند تعدیل%'), so neither is double-counted.
func TestFIN009AuditReferralAndPayoutCountedOnce(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, false)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	repo := NewOrderRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("setup: release did not run")
	}
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := repo.RefundCustomerReturn(ctx, ret, f.user); err != nil || amount != 10000000 {
		t.Fatalf("setup: full refund amount=%d err=%v", amount, err)
	}
	// Referrer keeps the released bonus (100k, FIN-011 G-10) and the
	// commission nets to 0 after the 10k reversal: available = 100k.
	var refOwner string
	if err := db.QueryRow(`SELECT owner_user_id FROM stores WHERE id=$1`, refStore).Scan(&refOwner); err != nil {
		t.Fatal(err)
	}
	req, err := finRepo.RequestOwnedStorePayout(ctx, refOwner, refStore, "IR820170000000123456789012", "بانک ملی", 100000, "fin009-t03")
	if err != nil {
		t.Fatalf("RequestOwnedStorePayout: %v", err)
	}
	if err := finRepo.ReviewSettlement(ctx, req.ID, "approved", "fin009", "admin"); err != nil {
		t.Fatalf("ReviewSettlement: %v", err)
	}
	if err := finRepo.ConfirmManualTransfer(ctx, req.ID, "TRF-fin009-t03", "admin"); err != nil {
		t.Fatalf("ConfirmManualTransfer: %v", err)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, refStore, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "برگشت کمیسیون معرفی"); n != 1 || ds != 10000 {
		t.Errorf("(c) referral-reversal entries=%d sum=%d, want 1/10000 (commission reversal counted once)", n, ds)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "تسویه و واریز به حساب شبا"); n != 1 || ds != 100000 {
		t.Errorf("(c) payout entries=%d sum=%d, want 1/100000 (payout + its PAY-LED leg counted once)", n, ds)
	}
	if _, _, n, _, _ := fin009CountReason(stmt, "سند تعدیل حساب"); n != 0 {
		t.Errorf("(c) %d finance_adjustment entries — the RCR-LED/PAY-LED ledger legs leak into the adjustment filter (double count)", n)
	}
	var adjustmentMatches int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ledger_entries WHERE target_account='store_payable:' || $1 AND target_type='seller' AND reason LIKE 'سند تعدیل%'`, refStore).Scan(&adjustmentMatches); err != nil || adjustmentMatches != 0 {
		t.Errorf("(c) ledger rows matching the adjustment filter=%d, want 0", adjustmentMatches)
	}
	if stmt.TotalCredit != 110000 || stmt.TotalDebit != 110000 || stmt.NetBalance != 0 || stmt.CurrentAvailable != 0 {
		t.Errorf("(c) referrer statement: credit=%d debit=%d net=%d current=%d, want 110000/110000/0/0",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance, stmt.CurrentAvailable)
	}
}

// (c) Signed finance adjustments appear once: the handler writes TWO
// ledger rows per document (payable leg + platform counterpart, same
// docId/reason prefix); the statement filter (target_account
// 'store_payable:<id>') must surface exactly the payable leg. The
// ledger is append-only and the request identity is unique.
func TestFIN009AuditAdjustmentAppearsOnce(t *testing.T) {
	db, f, _, _ := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin009AdjustmentLegs(t, db, f.store, "ADJ-fin009-t04a", "credit", 3000000)
	fin009AdjustmentLegs(t, db, f.store, "ADJ-fin009-t04b", "debit", 1000000)
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	credits, debits, n, cs, ds := fin009CountReason(stmt, "سند تعدیل حساب")
	if n != 2 || credits != 1 || debits != 1 || cs != 3000000 || ds != 1000000 {
		t.Errorf("(c) adjustment entries=%d (credits=%d debits=%d) sums=%d/%d, want 2 (1/1) 3000000/1000000 — the platform counterpart leg must not surface",
			n, credits, debits, cs, ds)
	}
	if stmt.TotalCredit != 3000000 || stmt.TotalDebit != 1000000 || stmt.NetBalance != 2000000 {
		t.Errorf("(c) totals: credit=%d debit=%d net=%d, want 3000000/1000000/2000000", stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance)
	}
	// Append-only: UPDATE and DELETE on ledger_entries must fail.
	if _, err := db.Exec(`UPDATE ledger_entries SET amount=999 WHERE related_order_or_dispute='ADJ-fin009-t04a' AND target_type='seller'`); err == nil {
		t.Errorf("(i) ledger UPDATE succeeded — ledger_entries is not append-only")
	}
	if _, err := db.Exec(`DELETE FROM ledger_entries WHERE related_order_or_dispute='ADJ-fin009-t04a' AND target_type='seller'`); err == nil {
		t.Errorf("(i) ledger DELETE succeeded — ledger_entries is not append-only")
	}
	// Request identity is unique: a replayed finance_adjustments row
	// with the same (kind, id, key) must conflict.
	if _, err := db.Exec(`INSERT INTO finance_adjustments(id,target_kind,target_id,direction,amount,reason,operator,request_key,request_fingerprint) VALUES('ADJ-fin009-t04a','seller',$1,'credit',3000000,'fin009 audit','tester','ADJ-fin009-t04a','fp-ADJ-fin009-t04a')`, f.store); err == nil {
		t.Errorf("(i) duplicate finance_adjustments request identity inserted — same-key replay can double-post")
	}
}

// (d) Multi-month arithmetic: opening + credits - debits = closing and
// closing - active reservations = signed available, across a reservation
// created in month N and posted in month N+1. FIN-001 identity:
// closingAvailable - openingAvailable = netBalance - (closingReserved -
// openingReserved). Month N's statement must be unchanged by N+1 activity.
func TestFIN009AuditMultiMonthArithmetic(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin009ReshapeProfit2M(t, db, order)
	if released := fin009Release2M(t, db, f, order); !released {
		t.Fatalf("setup: release did not run")
	}
	cur, prev, prevMid, _ := fin009Months()
	opsExec(t, db, `UPDATE orders SET profit_released_at=$1 WHERE id=$2`, prevMid, order)
	opsExec(t, db, `INSERT INTO payout_requests(id,store_id,recipient_type,recipient_name,shaba,bank_name,amount,status,created_at) VALUES('fin009-t05-pending',$1,'seller',$1,'IR820170000000123456789012','بانک ملی',500000,'pending',$2)`, f.store, prevMid)
	prevStmt, err := finRepo.GetStoreStatement(ctx, f.store, prev)
	if err != nil {
		t.Fatal(err)
	}
	if prevStmt.TotalCredit != 2000000 || prevStmt.TotalDebit != 0 || prevStmt.OpeningAvailable != 0 ||
		prevStmt.OpeningReserved != 0 || prevStmt.ClosingReserved != 500000 || prevStmt.ClosingAvailable != 1500000 {
		t.Errorf("(d) prev month: credit=%d debit=%d opening=%d/%d closing=%d/%d, want 2000000/0/0/0/1500000/500000",
			prevStmt.TotalCredit, prevStmt.TotalDebit, prevStmt.OpeningAvailable, prevStmt.OpeningReserved, prevStmt.ClosingAvailable, prevStmt.ClosingReserved)
	}
	if err := finRepo.ReviewSettlement(ctx, "fin009-t05-pending", "approved", "fin009", "admin"); err != nil {
		t.Fatalf("ReviewSettlement: %v", err)
	}
	if err := finRepo.ConfirmManualTransfer(ctx, "fin009-t05-pending", "TRF-fin009-t05", "admin"); err != nil {
		t.Fatalf("ConfirmManualTransfer: %v", err)
	}
	fin009AdjustmentLegs(t, db, f.store, "ADJ-fin009-t05", "credit", 300000)
	curStmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if curStmt.TotalCredit != 300000 || curStmt.TotalDebit != 500000 || curStmt.NetBalance != -200000 ||
		curStmt.OpeningAvailable != 1500000 || curStmt.OpeningReserved != 500000 ||
		curStmt.ClosingAvailable != 1800000 || curStmt.ClosingReserved != 0 || curStmt.CurrentAvailable != 1800000 {
		t.Errorf("(d) cur month: credit=%d debit=%d net=%d opening=%d/%d closing=%d/%d current=%d, want 300000/500000/-200000/1500000/500000/1800000/0/1800000",
			curStmt.TotalCredit, curStmt.TotalDebit, curStmt.NetBalance, curStmt.OpeningAvailable, curStmt.OpeningReserved, curStmt.ClosingAvailable, curStmt.ClosingReserved, curStmt.CurrentAvailable)
	}
	if got := curStmt.ClosingAvailable - curStmt.OpeningAvailable; got != curStmt.NetBalance-(curStmt.ClosingReserved-curStmt.OpeningReserved) {
		t.Errorf("(d) FIN-001 identity broken: closing-opening=%d, net-(Δreserved)=%d", got, curStmt.NetBalance-(curStmt.ClosingReserved-curStmt.OpeningReserved))
	}
	prevAgain, err := finRepo.GetStoreStatement(ctx, f.store, prev)
	if err != nil {
		t.Fatal(err)
	}
	if prevAgain.TotalCredit != prevStmt.TotalCredit || prevAgain.TotalDebit != prevStmt.TotalDebit ||
		prevAgain.ClosingAvailable != prevStmt.ClosingAvailable || len(prevAgain.Entries) != len(prevStmt.Entries) {
		t.Errorf("(d) prev-month statement CHANGED after N+1 activity: before=%+v after=%+v", prevStmt, prevAgain)
	}
}

// (e) Cross-month reversal: profit released in month N, full return
// refunded in month N+1. Month N's statement (entries + opening +
// closing) must be byte-identical before/after the reversal, and month
// N+1 must show the -2m with its own (N+1) date.
func TestFIN009AuditCrossMonthReversal(t *testing.T) {
	db, f, warehouse, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin009ReshapeProfit2M(t, db, order)
	if released := fin009Release2M(t, db, f, order); !released {
		t.Fatalf("setup: release did not run")
	}
	cur, prev, prevMid, _ := fin009Months()
	opsExec(t, db, `UPDATE orders SET profit_released_at=$1 WHERE id=$2`, prevMid, order)
	before, err := finRepo.GetStoreStatement(ctx, f.store, prev)
	if err != nil {
		t.Fatal(err)
	}
	if before.TotalCredit != 2000000 || before.TotalDebit != 0 || before.ClosingAvailable != 2000000 {
		t.Fatalf("setup: prev-month statement unexpected: %+v", before)
	}
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, f.user); err != nil || amount != 10000000 {
		t.Fatalf("setup: full refund amount=%d err=%v", amount, err)
	}
	after, err := finRepo.GetStoreStatement(ctx, f.store, prev)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != len(before.Entries) || after.TotalCredit != before.TotalCredit ||
		after.TotalDebit != before.TotalDebit || after.OpeningAvailable != before.OpeningAvailable ||
		after.ClosingAvailable != before.ClosingAvailable || after.NetBalance != before.NetBalance {
		t.Errorf("(e) month-N statement REWRITTEN by the N+1 reversal: before=%+v after=%+v", before, after)
	}
	var releasedAt time.Time
	var status string
	if err := db.QueryRow(`SELECT profit_released_at,status FROM orders WHERE id=$1`, order).Scan(&releasedAt, &status); err != nil {
		t.Fatal(err)
	}
	if !releasedAt.Equal(prevMid) || status != "delivered_confirmed" {
		t.Errorf("(e) refund moved the profit date/status: released_at=%v status=%s, want %v/delivered_confirmed", releasedAt, status, prevMid)
	}
	curStmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if curStmt.TotalCredit != 0 || curStmt.TotalDebit != 2000000 || curStmt.NetBalance != -2000000 ||
		curStmt.OpeningAvailable != 2000000 || curStmt.ClosingAvailable != 0 {
		t.Errorf("(e) cur month: credit=%d debit=%d net=%d opening=%d closing=%d, want 0/2000000/-2000000/2000000/0",
			curStmt.TotalCredit, curStmt.TotalDebit, curStmt.NetBalance, curStmt.OpeningAvailable, curStmt.ClosingAvailable)
	}
	nowUTC := time.Now().UTC()
	for _, e := range curStmt.Entries {
		if e.Reason == "برگشت سود فروشنده" {
			if e.Date.Year() != nowUTC.Year() || e.Date.Month() != nowUTC.Month() {
				t.Errorf("(e) reversal entry dated %v, want the current (N+1) month", e.Date)
			}
		}
	}
}

// fin009ResolveDispute resolves a dispute refund of the given claim
// amount and drives the obligation to 'succeeded' through the real
// provider path (fake local provider, the FIN-003 pattern). Mirrors
// fin011ResolveFullDispute for an arbitrary (partial) claim.
func fin009ResolveDispute(t *testing.T, db *sql.DB, f opsFixture, order string, claim int64) string {
	t.Helper()
	ctx := context.Background()
	disputeID := fin011Dispute(t, db, order, claim)
	if err := NewFinanceRepository(db).ResolveDispute(ctx, disputeID, "refund_seller", "seller", f.user, claim); err != nil {
		t.Fatalf("ResolveDispute: %v", err)
	}
	var obligationID string
	if err := db.QueryRow(`SELECT id FROM order_refund_obligations WHERE order_id=$1 AND purpose='dispute'`, order).Scan(&obligationID); err != nil {
		t.Fatalf("dispute obligation: %v", err)
	}
	// Distinct provider reference per test: order_refund_events has a
	// UNIQUE index on provider_reference for succeeded original_payment
	// refunds, so the constant "local-refund-confirmation" can only be
	// used once per disposable DB.
	if state, err := NewOrderRepository(db).ProcessOrderRefund(ctx, obligationID, &fin003LocalRefundProvider{status: "succeeded", reference: "fin009-" + uuid.NewString()[:12]}); err != nil || state != "succeeded" {
		t.Fatalf("dispute refund execution state=%s err=%v", state, err)
	}
	if applied, err := NewOrderRepository(db).ApplyDisputeRefundEffect(ctx, disputeID); err != nil || !applied {
		t.Fatalf("ApplyDisputeRefundEffect applied=%v err=%v", applied, err)
	}
	return disputeID
}

// fin009SellerProfitReversalSum is the recorded dispute/admin
// seller-profit reversal total for one order (the compensating events).
func fin009SellerProfitReversalSum(t *testing.T, db *sql.DB, order string) int64 {
	t.Helper()
	var sum int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM seller_profit_reversals WHERE order_id=$1`, order).Scan(&sum); err != nil {
		t.Fatalf("seller-profit reversal sum: %v", err)
	}
	return sum
}

// fin009AdjustmentLegsAt inserts one signed finance adjustment exactly as
// fin009AdjustmentLegs does, but dated at the given instant (for
// month-boundary tests).
func fin009AdjustmentLegsAt(t *testing.T, db *sql.DB, storeID, docID, direction string, amount int64, at time.Time) {
	t.Helper()
	counter := "credit"
	if direction == "credit" {
		counter = "debit"
	}
	opsExec(t, db, `INSERT INTO finance_adjustments(id,target_kind,target_id,direction,amount,reason,operator,request_key,request_fingerprint) VALUES($1,'seller',$2,$3,$4,'fin009 audit','tester',$5,$6)`,
		docID, storeID, direction, amount, docID, "fp-"+docID)
	for _, leg := range []struct{ account, typ, dir string }{
		{"store_payable:" + storeID, "seller", direction},
		{"platform", "platform", counter},
	} {
		opsExec(t, db, `INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'tester', $8)`,
			"ADJ-LED-"+uuid.NewString(), at, leg.account, leg.typ, leg.dir, amount, "سند تعدیل fin009 audit", docID)
	}
}

// (e2) DISPUTE REFUND — G-08 FIXED BEHAVIOUR. A full dispute refund
// that reaches 'succeeded' reverses the seller profit pro-rata through
// seller_profit_reversals (FIN-009 D-01): after a full 10m dispute
// refund the seller's 8.5m released profit is 0 and the statement shows
// one -8.5m seller-return debit. The order row is never mutated; the
// referral commission on the same order reverses alongside (FIN-011).
func TestFIN009AuditDisputeRefundReversesSellerProfit(t *testing.T) {
	db, f, _, order, refStore := fin011ReferralOrder(t, false)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("setup: release did not run")
	}
	bal0, err := finRepo.GetStoreBalance(ctx, f.store)
	if err != nil || bal0.AvailableAmount != 8500000 {
		t.Fatalf("setup: seller available=%+v err=%v, want 8500000", bal0, err)
	}
	fin009ResolveDispute(t, db, f, order, 10000000)
	if got := fin009SellerProfitReversalSum(t, db, order); got != 8500000 {
		t.Errorf("(e2) G-08: seller-profit reversal event missing: sum=%d, want 8500000", got)
	}
	bal1, err := finRepo.GetStoreBalance(ctx, f.store)
	if err != nil {
		t.Fatal(err)
	}
	if bal1.AvailableAmount != 0 {
		t.Errorf("(e2) G-08: after a full 10m dispute refund the seller balance is %d, conserved 0", bal1.AvailableAmount)
	}
	if bal1.SellerReturnReversals != 8500000 {
		t.Errorf("(e2) G-08: SellerReturnReversals=%d, want 8500000", bal1.SellerReturnReversals)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "برگشت سود فروشنده"); n != 1 || ds != 8500000 {
		t.Errorf("(e2) G-08: seller-return entries=%d sum=%d, want 1/8500000", n, ds)
	}
	if stmt.TotalCredit != 8500000 || stmt.TotalDebit != 8500000 || stmt.NetBalance != 0 {
		t.Errorf("(e2) G-08: totals credit=%d debit=%d net=%d, want 8500000/8500000/0",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance)
	}
	var status string
	var releasedAt sql.NullTime
	if err := db.QueryRow(`SELECT status,profit_released_at FROM orders WHERE id=$1`, order).Scan(&status, &releasedAt); err != nil {
		t.Fatal(err)
	}
	if status != "delivered_confirmed" || !releasedAt.Valid {
		t.Errorf("(e2) G-08: refund mutated the order row: status=%s released_at valid=%v", status, releasedAt.Valid)
	}
	// The referral commission on the same order reverses alongside
	// (FIN-011 leg) — but on the REFERRER's statement, since the
	// reversal debits the referrer's account (referrer_store_id = refStore).
	refStmt, err := finRepo.GetStoreStatement(ctx, refStore, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(refStmt, "برگشت کمیسیون معرفی"); n != 1 || ds != 10000 {
		t.Errorf("(e2) setup check: referrer referral-reversal entries=%d sum=%d, want 1/10000 (referral reverses, seller profit reverses too)", n, ds)
	}
}

// (e2b) G-08 PARTIAL: a 50% dispute refund after release reverses half
// the profit (4,250,000 = 8,500,000 × ½); the seller keeps the retained
// 4,250,000, and the referral commission reverses 5,000 alongside.
func TestFIN009AuditDisputeRefundPartial(t *testing.T) {
	db, f, _, order, refStore := fin011ReferralOrder(t, false)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("setup: release did not run")
	}
	fin009ResolveDispute(t, db, f, order, 5000000)
	if got := fin009SellerProfitReversalSum(t, db, order); got != 4250000 {
		t.Errorf("(e2b) G-08 partial: seller-profit reversal sum=%d, want 4250000 (half of 8500000)", got)
	}
	bal, err := finRepo.GetStoreBalance(ctx, f.store)
	if err != nil {
		t.Fatal(err)
	}
	if bal.AvailableAmount != 4250000 {
		t.Errorf("(e2b) G-08 partial: seller balance=%d, want 4250000 (retained half)", bal.AvailableAmount)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "برگشت سود فروشنده"); n != 1 || ds != 4250000 {
		t.Errorf("(e2b) G-08 partial: seller-return entries=%d sum=%d, want 1/4250000", n, ds)
	}
	if stmt.TotalCredit != 8500000 || stmt.TotalDebit != 4250000 || stmt.NetBalance != 4250000 {
		t.Errorf("(e2b) G-08 partial: totals credit=%d debit=%d net=%d, want 8500000/4250000/4250000",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance)
	}
	if got := fin011ReversalSum(t, db, order); got != 5000 {
		t.Errorf("(e2b) G-08 partial: referral reversal sum=%d, want 5000 (half of 10000)", got)
	}
	refStmt, err := finRepo.GetStoreStatement(ctx, refStore, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(refStmt, "برگشت کمیسیون معرفی"); n != 1 || ds != 5000 {
		t.Errorf("(e2b) G-08 partial: referrer referral-reversal entries=%d sum=%d, want 1/5000", n, ds)
	}
}

// (e2c) G-08 BEFORE RELEASE: a full dispute refund before release writes
// the reversal event, but the profit never becomes available — the
// release is held for review (any dispute obligation blocks it), the
// balance stays 0, and the statement shows no seller-return debit
// (nothing was released to reverse).
func TestFIN009AuditDisputeRefundBeforeRelease(t *testing.T) {
	db, f, _, order, refStore := fin011ReferralOrder(t, false)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin009ResolveDispute(t, db, f, order, 10000000)
	if got := fin009SellerProfitReversalSum(t, db, order); got != 8500000 {
		t.Errorf("(e2c) G-08 before release: seller-profit reversal event missing: sum=%d, want 8500000", got)
	}
	bal, err := finRepo.GetStoreBalance(ctx, f.store)
	if err != nil {
		t.Fatal(err)
	}
	if bal.AvailableAmount != 0 {
		t.Errorf("(e2c) G-08 before release: seller balance=%d, want 0 (profit never released, reversal nets against the unreleased hold, not the balance)", bal.AvailableAmount)
	}
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); released {
		t.Fatalf("(e2c) G-08 before release: release ran on a dispute-refunded order (must stay held for review)")
	}
	if state := fin011ReleaseState(t, db, order); state != "needs_review" {
		t.Fatalf("(e2c) G-08 before release: release state=%q, want needs_review", state)
	}
	if _, earned, _ := fin011ReferrerBalance(t, db, refStore); earned != 0 {
		t.Errorf("(e2c) G-08 before release: referrer commission earned=%d, conserved=0", earned)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "برگشت سود فروشنده"); n != 0 || ds != 0 {
		t.Errorf("(e2c) G-08 before release: seller-return entries=%d sum=%d, want 0/0 (nothing released to reverse)", n, ds)
	}
	if stmt.TotalCredit != 0 || stmt.TotalDebit != 0 || stmt.NetBalance != 0 {
		t.Errorf("(e2c) G-08 before release: totals credit=%d debit=%d net=%d, want 0/0/0",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance)
	}
}

// fin009WalletSellerEventSum is the wallet-return seller-profit
// reversal total for one order (customer_return_financial_events,
// component 'seller_profit', operation 'debit') — the wallet leg of
// the total seller reversal, as distinct from the dispute/admin leg in
// seller_profit_reversals.
func fin009WalletSellerEventSum(t *testing.T, db *sql.DB, order string) int64 {
	t.Helper()
	var sum int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(amount),0) FROM customer_return_financial_events WHERE order_id=$1 AND component='seller_profit' AND operation='debit'`, order).Scan(&sum); err != nil {
		t.Fatalf("wallet seller-profit event sum: %v", err)
	}
	return sum
}

// (e2d) ROUND-2 MIXED PATH — wallet return THEN dispute refund (critic
// interim finding). 50% wallet return + 25% dispute refund on one
// released order = 75% refunded, BELOW the full-refund cap. Each
// refunded value must count ONCE in the SPR cumulative rule: wallet
// 5,000,000 + dispute 2,500,000 = 7,500,000 of 10,000,000, so the
// seller keeps 25% of the 8,500,000 profit = 2,125,000 and the total
// seller reversal is 6,375,000 (wallet 4,250,000 + SPR 2,125,000).
// Pre-fix the dispute leg double-counted the wallet 5m: the
// obligations sum already includes the wallet return (its
// customer_return obligation is succeeded), and
// sellerProfitRefundedValueTx added the wallet return_items sum on
// top, so the SPR leg wrote 4,250,000 (total 8,500,000, balance 0).
// The referral leg is correct in this order (it trusts obligations
// alone): total 7,500.
func TestFIN009AuditMixedWalletThenDisputeCountsOnce(t *testing.T) {
	db, f, warehouse, order, refStore := fin011ReferralOrder(t, true)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("setup: release did not run")
	}
	// 50% wallet return: one of the two units.
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, f.user); err != nil || amount != 5000000 {
		t.Fatalf("setup: wallet refund amount=%d err=%v, want 5000000", amount, err)
	}
	if got := fin009WalletSellerEventSum(t, db, order); got != 4250000 {
		t.Fatalf("setup: wallet seller-profit event sum=%d, want 4250000", got)
	}
	balMid, err := finRepo.GetStoreBalance(ctx, f.store)
	if err != nil || balMid.AvailableAmount != 4250000 {
		t.Fatalf("setup: balance after wallet return=%+v err=%v, want 4250000", balMid, err)
	}
	// 25% dispute refund on the same order.
	fin009ResolveDispute(t, db, f, order, 2500000)
	if got := fin009SellerProfitReversalSum(t, db, order); got != 2125000 {
		t.Errorf("(e2d) mixed wallet-then-dispute: SPR sum=%d, want 2125000 (75%% of 8500000 is 6375000 total; wallet already reversed 4250000)", got)
	}
	if got := fin009WalletSellerEventSum(t, db, order); got != 4250000 {
		t.Errorf("(e2d) mixed wallet-then-dispute: wallet seller-profit event sum=%d, want 4250000 (the dispute leg must not rewrite the wallet leg)", got)
	}
	if total := fin009WalletSellerEventSum(t, db, order) + fin009SellerProfitReversalSum(t, db, order); total != 6375000 {
		t.Errorf("(e2d) mixed wallet-then-dispute: total seller reversal=%d, want 6375000", total)
	}
	bal, err := finRepo.GetStoreBalance(ctx, f.store)
	if err != nil {
		t.Fatal(err)
	}
	if bal.AvailableAmount != 2125000 {
		t.Errorf("(e2d) mixed wallet-then-dispute: seller balance=%d, want 2125000 (retained quarter of the profit)", bal.AvailableAmount)
	}
	if got := fin011ReversalSum(t, db, order); got != 7500 {
		t.Errorf("(e2d) mixed wallet-then-dispute: referral reversal sum=%d, want 7500 (75%% of 10000)", got)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "برگشت سود فروشنده"); n != 2 || ds != 6375000 {
		t.Errorf("(e2d) mixed wallet-then-dispute: seller-return entries=%d sum=%d, want 2/6375000 (wallet 4250000 + dispute 2125000)", n, ds)
	}
	if stmt.TotalCredit != 8500000 || stmt.TotalDebit != 6375000 || stmt.NetBalance != 2125000 {
		t.Errorf("(e2d) mixed wallet-then-dispute: totals credit=%d debit=%d net=%d, want 8500000/6375000/2125000",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance)
	}
	refStmt, err := finRepo.GetStoreStatement(ctx, refStore, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(refStmt, "برگشت کمیسیون معرفی"); n != 2 || ds != 7500 {
		t.Errorf("(e2d) mixed wallet-then-dispute: referrer referral-reversal entries=%d sum=%d, want 2/7500 (wallet 5000 + dispute 2500)", n, ds)
	}
}

// (e2e) ROUND-2 MIXED PATH, REVERSE ORDER — dispute refund THEN wallet
// return. The SPR totals must be identical to (e2d): the cumulative
// rule counts each refunded value once whatever the order, so SPR
// 2,125,000 + wallet 4,250,000 = 6,375,000 total, balance 2,125,000,
// statement seller-return debit 6,375,000 in 2 entries. The referral
// leg is NOT asserted here: the FIN-011 return path counts
// return_items only (priorRefunded + thisReturn) and does not see a
// prior dispute refund, so in this order it totals 5,000, not 7,500
// — a pre-existing FIN-011 asymmetry, out of this fix's scope
// (wallet-then-dispute only), flagged in fin009-fix.md §12.
func TestFIN009AuditMixedDisputeThenWalletCountsOnce(t *testing.T) {
	db, f, warehouse, order, _ := fin011ReferralOrder(t, true)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("setup: release did not run")
	}
	// 25% dispute refund first.
	fin009ResolveDispute(t, db, f, order, 2500000)
	if got := fin009SellerProfitReversalSum(t, db, order); got != 2125000 {
		t.Fatalf("setup: SPR after dispute=%d, want 2125000", got)
	}
	// 50% wallet return: one of the two units.
	ret := fin003ReceivedReturn(t, db, f, warehouse, order)
	if amount, _, err := NewOrderRepository(db).RefundCustomerReturn(ctx, ret, f.user); err != nil || amount != 5000000 {
		t.Fatalf("setup: wallet refund amount=%d err=%v, want 5000000", amount, err)
	}
	if got := fin009WalletSellerEventSum(t, db, order); got != 4250000 {
		t.Errorf("(e2e) mixed dispute-then-wallet: wallet seller-profit event sum=%d, want 4250000", got)
	}
	if got := fin009SellerProfitReversalSum(t, db, order); got != 2125000 {
		t.Errorf("(e2e) mixed dispute-then-wallet: SPR sum=%d, want 2125000 (the wallet leg must not rewrite the dispute leg)", got)
	}
	// Referral leg, OBSERVED (not conserved): the FIN-011 return path
	// counts return_items only, so it sees 5,000,000 refunded and
	// writes 2,500 on top of the dispute's 2,500 — total 5,000, not
	// the conserved 7,500. Pre-existing FIN-011 asymmetry, pinned
	// here so a later FIN-011 fix flips this line deliberately.
	if got := fin011ReversalSum(t, db, order); got != 5000 {
		t.Errorf("(e2e) mixed dispute-then-wallet: referral reversal sum=%d, want 5000 (observed pre-existing FIN-011 return-path asymmetry, see comment above)", got)
	}
	if total := fin009WalletSellerEventSum(t, db, order) + fin009SellerProfitReversalSum(t, db, order); total != 6375000 {
		t.Errorf("(e2e) mixed dispute-then-wallet: total seller reversal=%d, want 6375000", total)
	}
	bal, err := finRepo.GetStoreBalance(ctx, f.store)
	if err != nil {
		t.Fatal(err)
	}
	if bal.AvailableAmount != 2125000 {
		t.Errorf("(e2e) mixed dispute-then-wallet: seller balance=%d, want 2125000", bal.AvailableAmount)
	}
	cur, _, _, _ := fin009Months()
	stmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, _, ds := fin009CountReason(stmt, "برگشت سود فروشنده"); n != 2 || ds != 6375000 {
		t.Errorf("(e2e) mixed dispute-then-wallet: seller-return entries=%d sum=%d, want 2/6375000 (dispute 2125000 + wallet 4250000)", n, ds)
	}
	if stmt.TotalCredit != 8500000 || stmt.TotalDebit != 6375000 || stmt.NetBalance != 2125000 {
		t.Errorf("(e2e) mixed dispute-then-wallet: totals credit=%d debit=%d net=%d, want 8500000/6375000/2125000",
			stmt.TotalCredit, stmt.TotalDebit, stmt.NetBalance)
	}
}

// (f) G-10 STABLE PAGING. GetStoreStatementPage pages the month in
// (occurred DESC, id, reason) order with a limit and a total; the
// unpaged GetStoreStatement keeps returning the whole month (its
// contract is unchanged). 150 posted entries plus the two legs of one
// released order (same occurred + same id, ordered by reason) must
// round-trip exactly once, in the exact same order as the unpaged
// statement, with month totals on every page.
func TestFIN009AuditStatementHasNoPaging(t *testing.T) {
	db, f, _, order, _ := fin011ReferralOrder(t, false)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin011Confirm(t, db, order, f.user)
	fin011CloseWindow(t, db, order)
	if released := fin011Release(t, db, order); !released {
		t.Fatalf("setup: release did not run")
	}
	// Tiebreak fixture: point the order's referrer at its own store, so
	// the released_profit and referral_commission legs of THIS order
	// land on the SAME statement with identical (occurred, id) — the
	// (occurred, id, reason) total order is what keeps their relative
	// order deterministic. (In production the referrer is a different
	// store, so the legs land on different statements; the tiebreak
	// must hold for the degenerate state too.)
	opsExec(t, db, `UPDATE orders SET referrer_seller_id=$1 WHERE id=$2`, f.store, order)
	opsExec(t, db, `INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute)
		SELECT 'fin009-t08-'||g, NOW(), 'store_payable:`+f.store+`', 'seller','credit',1000,'سند تعدیل bulk','tester','ADJ-fin009-t08-'||g
		FROM generate_series(1,150) g`)
	cur, _, _, _ := fin009Months()
	full, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Entries) != 152 || full.TotalCredit != 8660000 || full.TotalEntries != 152 {
		t.Fatalf("setup: entries=%d credit=%d total=%d, want 152/8660000/152", len(full.Entries), full.TotalCredit, full.TotalEntries)
	}
	var got []StatementEntry
	cursor := ""
	pages := 0
	for {
		page, err := finRepo.GetStoreStatementPage(ctx, f.store, cur, StatementPageOptions{Limit: 7, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if len(page.Entries) > 7 {
			t.Fatalf("(f) G-10: page %d has %d entries, limit 7 violated", pages, len(page.Entries))
		}
		if page.Limit != 7 || page.TotalEntries != 152 || page.TotalCredit != full.TotalCredit || page.TotalDebit != full.TotalDebit || page.NetBalance != full.NetBalance {
			t.Fatalf("(f) G-10: page %d limit=%d total=%d credit=%d debit=%d net=%d, want 7/152/%d/%d/%d",
				pages, page.Limit, page.TotalEntries, page.TotalCredit, page.TotalDebit, page.NetBalance, full.TotalCredit, full.TotalDebit, full.NetBalance)
		}
		got = append(got, page.Entries...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 40 {
			t.Fatalf("(f) G-10: paging did not terminate after 40 pages")
		}
	}
	if pages != 22 {
		t.Errorf("(f) G-10: %d pages for 152 entries at limit 7, want 22 (21 full + 1 of 5)", pages)
	}
	if len(got) != 152 {
		t.Fatalf("(f) G-10: paged round-trip collected %d entries, want 152 exactly once", len(got))
	}
	for i := range got {
		if got[i] != full.Entries[i] {
			t.Fatalf("(f) G-10: paged entry %d differs from the unpaged statement: %+v vs %+v", i, got[i], full.Entries[i])
		}
	}
	// Same-order legs: adjacent, commission before profit (reason order).
	foundLegs := false
	for i := 0; i+1 < len(got); i++ {
		if got[i].ID == order && got[i+1].ID == order {
			foundLegs = true
			if got[i].Reason != "کمیسیون معرفی آزادشده" || got[i+1].Reason != "سود آزادشده سفارش" {
				t.Fatalf("(f) G-10: same-order legs out of order: %+v then %+v", got[i], got[i+1])
			}
		}
	}
	if !foundLegs {
		t.Fatalf("(f) G-10: same-order profit/commission legs are not adjacent in the paged order")
	}
	// An invalid cursor is an error, not a silent first page.
	if _, err := finRepo.GetStoreStatementPage(ctx, f.store, cur, StatementPageOptions{Limit: 7, Cursor: "!!!not-a-cursor"}); err == nil {
		t.Errorf("(f) G-10: invalid cursor accepted silently")
	}
}

// (g) G-11 TEHRAN MONTH (Q-02: TEHRAN, owner decision Nov 19 —
// re-scoped from the UTC expectations the fix leg first wrote).
// Statement months are Asia/Tehran calendar months: the month window
// is [00:00 Tehran on the 1st, 00:00 Tehran on the 1st of next
// month), and the handler default derives from Tehran too
// (defaultStatementMonth in cmd/server). Three boundary events pin
// it: 2026-11-01 00:30 Tehran (= 2026-10-31 21:00 UTC) is November
// in Tehran but October in UTC — it must land in 2026-11;
// 2026-10-31 23:30 UTC (= 2026-11-01 03:00 Tehran) is November;
// 2026-10-31 20:00 UTC (= 2026-10-31 23:30 Tehran) is October.
func TestFIN009AuditMonthTimezoneDivergence(t *testing.T) {
	db, f, _, _ := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	tehran := time.FixedZone("Asia/Tehran", 3*3600+1800)
	inTehranNov := time.Date(2026, 11, 1, 0, 30, 0, 0, tehran)
	if inTehranNov.UTC().Format("2006-01") != "2026-10" {
		t.Fatalf("setup: instant UTC month=%s, want 2026-10 (the event is October in UTC, November in Tehran)", inTehranNov.UTC().Format("2006-01"))
	}
	fin009AdjustmentLegsAt(t, db, f.store, "ADJ-fin009-t09a", "credit", 700000, inTehranNov)
	fin009AdjustmentLegsAt(t, db, f.store, "ADJ-fin009-t09b", "credit", 800000, time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC))
	fin009AdjustmentLegsAt(t, db, f.store, "ADJ-fin009-t09c", "credit", 900000, time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC))
	oct, err := finRepo.GetStoreStatement(ctx, f.store, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, cs, _ := fin009CountReason(oct, "سند تعدیل حساب"); n != 1 || cs != 900000 {
		t.Errorf("(g) G-11: 2026-10 adjustment entries=%d sum=%d, want 1/900000 (only the 2026-10-31 23:30 Tehran event is an October event)", n, cs)
	}
	if oct.TotalCredit != 900000 || oct.CurrentAvailable != 2400000 {
		t.Errorf("(g) G-11: 2026-10 credit=%d current=%d, want 900000/2400000", oct.TotalCredit, oct.CurrentAvailable)
	}
	nov, err := finRepo.GetStoreStatement(ctx, f.store, "2026-11")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, n, cs, _ := fin009CountReason(nov, "سند تعدیل حساب"); n != 2 || cs != 1500000 {
		t.Errorf("(g) G-11: 2026-11 adjustment entries=%d sum=%d, want 2/1500000 (both Tehran-November events: 700000 + 800000)", n, cs)
	}
	if nov.TotalCredit != 1500000 || nov.OpeningAvailable != 900000 {
		t.Errorf("(g) G-11: 2026-11 credit=%d opening=%d, want 1500000/900000", nov.TotalCredit, nov.OpeningAvailable)
	}
}

// (h1) G-13 UNDATED NONZERO EVENT — FAIL-CLOSED PIN (coordinator
// decision: KEEP hard-fail, FIN-001 contract). A released order whose
// profit_released_at is NULL (legacy row) makes GetStoreStatement
// return ErrFinancePrincipalUnresolved for EVERY month of that store
// (per FIN-001: a nonzero posted term lacking its required date is
// reconciliation-required). Blast radius is per-store: another store's
// statement still works. The error is returned, never a silent 0.
func TestFIN009AuditUndatedProfitBricksStatement(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin009ReshapeProfit2M(t, db, order)
	if released := fin009Release2M(t, db, f, order); !released {
		t.Fatalf("setup: release did not run")
	}
	opsExec(t, db, `UPDATE orders SET profit_released_at=NULL WHERE id=$1`, order)
	cur, prev, _, _ := fin009Months()
	_, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if !errors.Is(err, ErrFinancePrincipalUnresolved) {
		t.Errorf("(h) G-13: undated nonzero profit: GetStoreStatement err=%v, want ErrFinancePrincipalUnresolved (fail-closed, not a silent 0)", err)
	}
	_, err = finRepo.GetStoreStatement(ctx, f.store, prev)
	if !errors.Is(err, ErrFinancePrincipalUnresolved) {
		t.Errorf("(h) G-13: undated nonzero profit: previous month err=%v, want ErrFinancePrincipalUnresolved for every month of the store", err)
	}
	if _, err := finRepo.GetStoreStatementPage(ctx, f.store, cur, StatementPageOptions{Limit: 10}); !errors.Is(err, ErrFinancePrincipalUnresolved) {
		t.Errorf("(h) G-13: paged statement err=%v, want ErrFinancePrincipalUnresolved (the guard applies to paging too)", err)
	}
	_, otherF, _, _ := fin003VerifiedPaidOrder(t)
	if _, err := finRepo.GetStoreStatement(ctx, otherF.store, cur); err != nil {
		t.Errorf("(h) blast radius: a clean store's statement failed too: %v", err)
	}
}

// (h) G-12 EXPLICIT OPENING MARKER. The opening amount stays derived,
// but it is now explicit in the response as OpeningEntry, and
// OpeningVerified flags that no reconciliation evidence backs it
// (FIN-009 records none; FIN-014 owns the cutover). History is never
// invented: without evidence the opening is flagged, not replaced.
// The entry is not a month flow: it never appears in Entries or in the
// month totals.
func TestFIN009AuditNoExplicitOpeningEntry(t *testing.T) {
	db, f, _, order := fin003VerifiedPaidOrder(t)
	ctx := context.Background()
	finRepo := NewFinanceRepository(db)
	fin009ReshapeProfit2M(t, db, order)
	if released := fin009Release2M(t, db, f, order); !released {
		t.Fatalf("setup: release did not run")
	}
	cur, prev, prevMid, _ := fin009Months()
	opsExec(t, db, `UPDATE orders SET profit_released_at=$1 WHERE id=$2`, prevMid, order)
	prevStmt, err := finRepo.GetStoreStatement(ctx, f.store, prev)
	if err != nil {
		t.Fatal(err)
	}
	curStmt, err := finRepo.GetStoreStatement(ctx, f.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if curStmt.OpeningAvailable != prevStmt.ClosingAvailable || curStmt.OpeningAvailable != 2000000 {
		t.Fatalf("setup: opening=%d prev closing=%d, want 2000000/2000000", curStmt.OpeningAvailable, prevStmt.ClosingAvailable)
	}
	if prevStmt.OpeningEntry != nil {
		t.Errorf("(h) G-12: prev-month opening entry present (%+v) for a 0 opening — want nil", prevStmt.OpeningEntry)
	}
	if curStmt.OpeningEntry == nil {
		t.Fatalf("(h) G-12: no explicit opening entry in the statement (OpeningAvailable=%d is still only implied)", curStmt.OpeningAvailable)
	}
	oe := curStmt.OpeningEntry
	if oe.Amount != 2000000 || oe.Type != "credit" || oe.Reason != "مانده ابتدای دوره" || oe.ID != "opening:"+cur || oe.Reference != "opening" {
		t.Errorf("(h) G-12: opening entry=%+v, want id=opening:%s credit 2000000 reason=مانده ابتدای دوره", oe, cur)
	}
	// The opening entry is dated at the month start — the Asia/Tehran
	// month start since Q-02 (TEHRAN), not the UTC start the fix leg
	// first pinned: statementMonthWindow defines start as 00:00
	// Asia/Tehran on the 1st.
	tehranLoc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	monthStart, err := time.ParseInLocation("2006-01", cur, tehranLoc)
	if err != nil {
		t.Fatal(err)
	}
	if !oe.Date.Equal(monthStart) {
		t.Errorf("(h) G-12: opening entry dated %v, want the Tehran month start %v", oe.Date, monthStart)
	}
	if curStmt.OpeningVerified {
		t.Errorf("(h) G-12: opening flagged VERIFIED with no reconciliation evidence — an unverified opening must be flagged, never presented as reconciled")
	}
	for _, e := range curStmt.Entries {
		if e.Reason == "مانده ابتدای دوره" {
			t.Fatalf("(h) G-12: opening entry leaked into month Entries (%+v) — it is not a month flow", e)
		}
	}
	if curStmt.TotalCredit != 0 || curStmt.TotalDebit != 0 || curStmt.NetBalance != 0 {
		t.Errorf("(h) G-12: cur-month totals credit=%d debit=%d net=%d, want 0/0/0 (the opening is not a month flow)", curStmt.TotalCredit, curStmt.TotalDebit, curStmt.NetBalance)
	}
	// Zero opening: no entry, still flagged unverified.
	_, freshF, _, _ := fin003VerifiedPaidOrder(t)
	fresh, err := finRepo.GetStoreStatement(ctx, freshF.store, cur)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.OpeningEntry != nil {
		t.Errorf("(h) G-12: zero-opening statement carries an opening entry (%+v) — want nil", fresh.OpeningEntry)
	}
	if fresh.OpeningVerified {
		t.Errorf("(h) G-12: zero opening flagged verified with no evidence")
	}
}
