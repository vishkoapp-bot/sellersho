package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
)

// FIN-008 treasury cash position (rework of the cash-position case in
// finance_parity_handlers.go, D-01..D-17).
//
// The whole report is computed in ONE read-only REPEATABLE READ
// snapshot (D-14), from verified cash only:
//
//	liquidCash = verified captured/settled gateway receipts
//	             + verified (evidenced) opening cash
//	             - confirmed money out (completed/transfer_confirmed
//	               payouts + paid customer withdrawals)
//	             - succeeded gateway refunds
//	// UNKNOWN (null) while no verified cash-in exists and paid-order
//	// value is unresolved; never 0, never allTimeIn (D-01/D-16).
//
//	liability = Σ max(available,0) per seller store
//	            + Σ max(available,0) per supplier
//	            + Σ customer_wallets.balance
//	            + Σ pending/unknown/failed refund obligations (gateway + wallet;
//	              a failed refund is a failed *attempt*, not a decision the
//	              customer is not owed — FIN-003 keeps it reserved and the
//	              reconciler retries it, so it stays a liability (F-01))
//	            + Σ unconfirmed holds (payouts pending/approved/
//	              processing + pending early-withdrawal gross)
//	            + Σ unreleased seller profit on delivered paid orders
//	// Each hold is already netted inside canonical available, so
//	// liability = available + holds (never holds alone, never twice).
//	// Negative per-principal balances are reported separately as
//	// recovery receivables and NEVER netted (D-10).
//
//	safeToSpend = liquidCash - liability (checked arithmetic, D-17);
//	null + reconciliation_required while liquidCash is unknown.
//
// Money unit: Toman everywhere (FIN-012). Every amount is validated
// against MaxTransportMoney; any failed read or overflow fails the
// report (HTTP 500/409, no figures) instead of rendering 0 (D-15).

// treasuryRefundCurrentStateFmt is the FIN-003 current-state
// projection for an order_refund_obligations row (same derivation as
// refund_summary.go): the latest event status, else pending when an
// unknown obligation has a verified gateway source, else the row
// status. %s is the obligations table alias.
const treasuryRefundCurrentStateFmt = `COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=%s.id ORDER BY e.event_no DESC LIMIT 1),CASE WHEN %s.status='unknown' AND %s.destination<>'review' AND EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.payment_id=%s.source_payment_id AND r.funding_kind='gateway' AND r.amount_toman=%s.source_amount_toman) THEN 'pending' ELSE %s.status END)`

func treasuryRefundCurrentState(alias string) string {
	return fmt.Sprintf(treasuryRefundCurrentStateFmt, alias, alias, alias, alias, alias, alias)
}

// treasuryPaidPayoutError is raised when a payout_requests row carries
// the undefined status 'paid' (D-05): no writer in the tree sets it,
// so the row cannot be classified as a hold or as money-out. The
// report fails with reconciliation_required instead of guessing.
type treasuryPaidPayoutError struct{ Count int64 }

func (e *treasuryPaidPayoutError) Error() string {
	return fmt.Sprintf("treasury: %d payout_requests row(s) with undefined status 'paid'; reconciliation required", e.Count)
}

// treasuryMoney validates one DB sum as FIN-012 money: an integer
// number of Toman inside the JSON-safe bound.
func treasuryMoney(v int64) (int64, error) {
	if v > pricing.MaxTransportMoney || v < -pricing.MaxTransportMoney {
		return 0, pricing.ErrInvalidMoney
	}
	return v, nil
}

// treasuryCheckedSum adds credits and subtracts debits through the
// checked big.Int path (repository.SumSignedFinanceBalances); an
// out-of-range result fails instead of wrapping (D-17). Every input
// must already be treasuryMoney-validated (the negation below is then
// safe).
func treasuryCheckedSum(credits, debits []int64) (int64, error) {
	args := make([]int64, 0, len(credits)+len(debits))
	args = append(args, credits...)
	for _, d := range debits {
		if d == math.MinInt64 {
			return 0, pricing.ErrInvalidMoney
		}
		args = append(args, -d)
	}
	return repository.SumSignedFinanceBalances(args...)
}

// treasuryQuerySum runs one SUM query on the snapshot querier and fails
// closed on any error or out-of-range value. It never returns 0 for a
// failed read (D-15).
func treasuryQuerySum(ctx context.Context, q repository.TreasuryQuerier, query string, args ...interface{}) (int64, error) {
	var n sql.NullInt64
	if err := q.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, fmt.Errorf("treasury: sum query returned NULL")
	}
	return treasuryMoney(n.Int64)
}

// treasuryQueryCount runs one COUNT(*) query on the snapshot querier
// and fails closed on any error (D-15).
func treasuryQueryCount(ctx context.Context, q repository.TreasuryQuerier, query string, args ...interface{}) (int64, error) {
	var n int64
	if err := q.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// treasuryTrendPoint is one day of the period trend. available is
// always true for a computed day: a trend that cannot be computed
// fails the whole report (D-15), so a missing day is never rendered
// as 0.
type treasuryTrendPoint struct {
	Date      string `json:"date"`
	In        int64  `json:"in"`
	Out       int64  `json:"out"`
	Available bool   `json:"available"`
}

// treasuryCashPosition is the cash-position response (CashPositionDto).
// liquidCash / safeToSpend are *int64 so unknown cash serialises as
// JSON null, never 0 (D-16).
type treasuryCashPosition struct {
	Success                     bool                   `json:"success"`
	Days                        int                    `json:"days"`
	AllTimeIn                   int64                  `json:"allTimeIn"`
	AllTimeOut                  int64                  `json:"allTimeOut"`
	OverObligated               bool                   `json:"overObligated"`
	OverObligatedAmount         int64                  `json:"overObligatedAmount"`
	LiquidCash                  *int64                 `json:"liquidCash"`
	SafeToSpend                 *int64                 `json:"safeToSpend"`
	CashStatus                  string                 `json:"cashStatus"`
	ReconciliationRequired      bool                   `json:"reconciliationRequired"`
	UnresolvedProviderCash      int64                  `json:"unresolvedProviderCash"`
	UnverifiedPaidOrders        int64                  `json:"unverifiedPaidOrders"`
	LegacySimulator             int64                  `json:"legacySimulator"`
	Liability                   int64                  `json:"liability"`
	RecoveryReceivableStores    int64                  `json:"recoveryReceivableStores"`
	RecoveryReceivableSuppliers int64                  `json:"recoveryReceivableSuppliers"`
	WalletLiability             int64                  `json:"walletLiability"`
	RefundLiability             int64                  `json:"refundLiability"`
	RefundLiabilityGateway      int64                  `json:"refundLiabilityGateway"`
	RefundLiabilityWallet       int64                  `json:"refundLiabilityWallet"`
	RefundLiabilityReview       int64                  `json:"refundLiabilityReview"`
	Holds                       int64                  `json:"holds"`
	HoldsByStatus               map[string]int64       `json:"holdsByStatus"`
	PeriodIn                    int64                  `json:"periodIn"`
	PeriodOut                   int64                  `json:"periodOut"`
	PeriodTrend                 []treasuryTrendPoint   `json:"-"`
	Period                      map[string]interface{} `json:"period"`
	HeldOrderCount              int64                  `json:"heldOrderCount"`
	HeldInOrders                int64                  `json:"heldInOrders"`
	UnreleasedProfitHold        int64                  `json:"unreleasedProfitHold"`
	OwedToStores                int64                  `json:"owedToStores"`
	OwedToSuppliers             int64                  `json:"owedToSuppliers"`
	PendingRefundCount          int64                  `json:"pendingRefundCount"`
	PendingRefunds              int64                  `json:"pendingRefunds"`
	PendingCount                int64                  `json:"pendingCount"`
	PendingPayouts              int64                  `json:"pendingPayouts"`
	PlatformEarned              int64                  `json:"platformEarned"`
	SellerInFlight              int64                  `json:"sellerInFlight"`
	SupplierInFlight            int64                  `json:"supplierInFlight"`
	SnapshotAt                  string                 `json:"snapshotAt"`
	SnapshotTxid                int64                  `json:"snapshotTxid"`
	CustomerPayoutCompletion    string                 `json:"customerPayoutCompletion"`
	Note                        string                 `json:"note,omitempty"`
}

// computeTreasuryCashPosition computes the whole cash position from
// one snapshot querier (a REPEATABLE READ tx in production). Every
// read error or overflow aborts the report with an error.
func computeTreasuryCashPosition(ctx context.Context, q repository.TreasuryQuerier, days int) (*treasuryCashPosition, error) {
	pos := &treasuryCashPosition{Success: true, Days: days, CashStatus: "verified"}

	// Snapshot identity (D-14): the database clock and the transaction
	// id. NOW() is the transaction start timestamp, stable for the
	// whole snapshot.
	var dbNow time.Time
	var txid int64
	if err := q.QueryRowContext(ctx, `SELECT NOW(), txid_current()`).Scan(&dbNow, &txid); err != nil {
		return nil, err
	}
	pos.SnapshotAt = dbNow.UTC().Format(time.RFC3339)
	pos.SnapshotTxid = txid
	cutoff := dbNow.AddDate(0, 0, -days)

	// D-05: any payout row with the undefined status 'paid' (or any
	// status outside the known state machine) fails the report —
	// it can be neither a hold nor money-out.
	{
		rows, err := q.QueryContext(ctx, `SELECT status, COUNT(*) FROM payout_requests WHERE status NOT IN ('pending','approved','rejected','processing','completed','transfer_confirmed') GROUP BY status`)
		if err != nil {
			return nil, err
		}
		var bad []string
		var paidCount int64
		for rows.Next() {
			var status string
			var n int64
			if err := rows.Scan(&status, &n); err != nil {
				rows.Close()
				return nil, err
			}
			if status == "paid" {
				paidCount += n
			} else {
				bad = append(bad, status)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if paidCount > 0 {
			return nil, &treasuryPaidPayoutError{Count: paidCount}
		}
		if len(bad) > 0 {
			return nil, fmt.Errorf("treasury: payout_requests contains unknown status(es) %v; reconciliation required", bad)
		}
	}

	// --- verified cash-in (D-01/D-02) -------------------------------------
	// Gateway receipts are the only captured/settled cash evidence.
	// The wallet-funded slice of an order is money already held, so it
	// is never fresh cash-in; it is recognised once at wallet top-up.
	verifiedReceipts, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount_toman),0) FROM order_payment_receipts WHERE funding_kind='gateway' AND currency='TOMAN'`)
	if err != nil {
		return nil, err
	}
	legacySimulator, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount_toman),0) FROM order_payment_receipts WHERE funding_kind='simulated' AND currency='TOMAN'`)
	if err != nil {
		return nil, err
	}
	openingCash, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount_toman),0) FROM treasury_opening_cash WHERE currency='TOMAN'`)
	if err != nil {
		return nil, err
	}
	// Unverified paid-order value: payable minus the wallet slice,
	// minus any receipt (gateway or simulated) for that order, floored
	// at 0 per order. Simulator-origin orders contribute 0 here — their
	// value is itemised as legacySimulator instead (G-13).
	unverifiedPaidOrders, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(GREATEST(o.payable_amount - COALESCE(o.wallet_amount,0)
		- COALESCE((SELECT SUM(r.amount_toman) FROM order_payment_receipts r WHERE r.order_id=o.id AND r.funding_kind='gateway'),0)
		- COALESCE((SELECT SUM(r.amount_toman) FROM order_payment_receipts r WHERE r.order_id=o.id AND r.funding_kind='simulated'),0), 0)),0)
		FROM orders o WHERE o.payment_status='paid' AND o.status != 'cancelled'`)
	if err != nil {
		return nil, err
	}

	// --- confirmed money out (C-01/D-32) -----------------------------------
	paidOut, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM payout_requests WHERE status IN ('completed','transfer_confirmed')`)
	if err != nil {
		return nil, err
	}
	customerPaid, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM customer_withdrawal_requests WHERE status='paid'`)
	if err != nil {
		return nil, err
	}

	// --- FIN-003 refund obligations (D-03/D-09) ---------------------------
	// current_state is the FIN-003 projection (latest event, else
	// pending when an unknown obligation has a verified gateway
	// source). Pending/unknown/failed gateway refunds are a liability
	// (F-01: a 'failed' event is a failed provider attempt, not a
	// decision that the customer is not owed — FIN-003 keeps failed
	// obligations reserved and retries them, so dropping them would
	// raise safeToSpend with no money movement);
	// succeeded gateway refunds already left the bank, so they reduce
	// liquidCash. Wallet-destination obligations count once — as the
	// obligation before credit, as wallet balance after (G-05).
	var refundGateway, refundWallet, refundSucceededGateway, refundReview int64
	{
		err := q.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(amount_toman) FILTER (WHERE current_state IN ('pending','unknown','failed') AND destination='original_payment'),0),
			COALESCE(SUM(amount_toman) FILTER (WHERE current_state IN ('pending','unknown','failed') AND destination='wallet'),0),
			COALESCE(SUM(amount_toman) FILTER (WHERE current_state='succeeded' AND destination='original_payment'),0),
			COALESCE(SUM(amount_toman) FILTER (WHERE current_state IN ('pending','unknown') AND destination='review'),0)
			FROM (SELECT o.amount_toman, o.destination, `+treasuryRefundCurrentState("o")+` AS current_state FROM order_refund_obligations o) s`).Scan(&refundGateway, &refundWallet, &refundSucceededGateway, &refundReview)
		if err != nil {
			return nil, err
		}
		for _, v := range []int64{refundGateway, refundWallet, refundSucceededGateway, refundReview} {
			if _, err := treasuryMoney(v); err != nil {
				return nil, err
			}
		}
	}

	// --- customer wallet balances (D-07/D-08) ------------------------------
	// Σ balance is the liability. locked_balance is a subset marker
	// inside balance, never another balance; pending withdrawal
	// requests are a subset too (kept as informational display only).
	walletLiability, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(balance),0) FROM customer_wallets`)
	if err != nil {
		return nil, err
	}

	// --- canonical per-principal availability, set-based (D-10/D-13) -----
	owedToStores, recoveryStores, err := repository.SumAllStoresAvailableSplit(ctx, q)
	if err != nil {
		return nil, err
	}
	owedToSuppliers, recoverySuppliers, err := repository.SumAllSuppliersAvailableSplit(ctx, q)
	if err != nil {
		return nil, err
	}

	// --- unified holds, exactly once (D-04/D-12) ---------------------------
	// Payouts pending/approved/processing are unconfirmed holds; the
	// 'paid' status is rejected above (D-05). Pending early
	// withdrawals contribute their gross. On approval the gross hold
	// converts to fee (platform revenue, out of liability) + net payout
	// request (an approved hold until confirmed) — see G-10.
	holdsByStatus := map[string]int64{"pending": 0, "approved": 0, "processing": 0, "earlyWithdrawal": 0}
	var payoutHolds int64
	{
		rows, err := q.QueryContext(ctx, `SELECT status, COALESCE(SUM(amount),0) FROM payout_requests WHERE status IN ('pending','approved','processing') GROUP BY status`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var status string
			var v int64
			if err := rows.Scan(&status, &v); err != nil {
				rows.Close()
				return nil, err
			}
			if _, err := treasuryMoney(v); err != nil {
				rows.Close()
				return nil, err
			}
			holdsByStatus[status] = v
			payoutHolds += v
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if _, err := treasuryMoney(payoutHolds); err != nil {
			return nil, err
		}
	}
	earlyGross, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(gross_amount),0) FROM seller_early_withdrawals WHERE status='pending'`)
	if err != nil {
		return nil, err
	}
	holdsByStatus["earlyWithdrawal"] = earlyGross

	// --- unreleased seller profit only (D-11, F-02) ----------------------
	// Never the whole order: the supplier wholesale slice is already
	// inside owedToSuppliers and the platform fee is platform money.
	// Only delivered/delivered_confirmed paid orders count — profit on
	// an undelivered order is not earned yet.
	// F-02: a refunded return's seller-profit reversal is netted
	// against THIS hold, exactly once, per order (floored at 0):
	// the reversal is written unconditionally by
	// recordReturnReversalsTx even when the profit was never released,
	// so counting the full profit here while the canonical available
	// already subtracts the reversal would state the same money as
	// both owed-to-the-seller (hold) and owed-by-the-seller
	// (recovery). The matching add-back in
	// SumAllStoresAvailableSplit keeps the store side coherent: the
	// reversal reduces the hold, never the released availability.
	var heldInOrders, heldOrderCount int64
	{
		err := q.QueryRowContext(ctx, `SELECT COALESCE(SUM(net),0), COALESCE(COUNT(*) FILTER (WHERE net > 0),0) FROM (
			SELECT GREATEST(COALESCE(o.dropshipper_profit,0) - COALESCE((SELECT SUM(CASE WHEN operation='debit' THEN amount ELSE -amount END) FROM customer_return_financial_events WHERE order_id=o.id AND component='seller_profit'),0), 0) AS net
			FROM orders o WHERE o.payment_status='paid' AND COALESCE(o.profit_released,FALSE)=FALSE AND o.status IN ('delivered','delivered_confirmed')
		) s`).Scan(&heldInOrders, &heldOrderCount)
		if err != nil {
			return nil, err
		}
		if _, err := treasuryMoney(heldInOrders); err != nil {
			return nil, err
		}
	}

	// --- diagnostics + informational counts --------------------------------
	// allTimeIn stays as the labelled diagnostic "gross paid-order
	// value, not cash" (D-01) — it is never an input to safeToSpend.
	allTimeIn, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(payable_amount),0) FROM orders WHERE payment_status='paid' AND status != 'cancelled'`)
	if err != nil {
		return nil, err
	}
	pendingRefundCount, err := treasuryQueryCount(ctx, q, `SELECT COUNT(*) FROM customer_withdrawal_requests WHERE status IN ('pending','approved')`)
	if err != nil {
		return nil, err
	}
	pendingRefunds, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM customer_withdrawal_requests WHERE status IN ('pending','approved')`)
	if err != nil {
		return nil, err
	}
	pendingCount, err := treasuryQueryCount(ctx, q, `SELECT COUNT(*) FROM payout_requests WHERE status='pending'`)
	if err != nil {
		return nil, err
	}
	pendingPayouts, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM payout_requests WHERE status='pending'`)
	if err != nil {
		return nil, err
	}
	sellerInFlight, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM payout_requests WHERE status='approved' AND recipient_type='seller'`)
	if err != nil {
		return nil, err
	}
	supplierInFlight, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM payout_requests WHERE status='approved' AND recipient_type='supplier'`)
	if err != nil {
		return nil, err
	}

	// --- realised platform share (D-19) -------------------------------------
	// Only profit on orders with verified capture and no succeeded
	// gateway refund counts as realised; anything else is labelled
	// "recorded", not "realised".
	platformEarned, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(o.platform_profit),0) FROM orders o
		WHERE o.payment_status='paid' AND o.status != 'cancelled'
		AND EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id AND r.funding_kind='gateway')
		AND NOT EXISTS (SELECT 1 FROM order_refund_obligations ob WHERE ob.order_id=o.id AND ob.destination='original_payment' AND `+treasuryRefundCurrentState("ob")+`='succeeded')`)
	if err != nil {
		return nil, err
	}

	// --- period windows by money-movement date, DB clock (D-20) ------------
	// Cash moves at capture/settlement/confirmation time, not at row
	// creation time; the cutoff comes from the snapshot's own clock.
	periodInReceipts, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount_toman),0) FROM order_payment_receipts WHERE funding_kind='gateway' AND verified_at >= $1`, cutoff)
	if err != nil {
		return nil, err
	}
	periodInOpening, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount_toman),0) FROM treasury_opening_cash WHERE verified_at >= $1`, cutoff)
	if err != nil {
		return nil, err
	}
	periodOutPayouts, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM payout_requests WHERE status IN ('completed','transfer_confirmed') AND COALESCE(transfer_confirmed_at, processed_at, created_at) >= $1`, cutoff)
	if err != nil {
		return nil, err
	}
	periodOutWithdrawals, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(amount),0) FROM customer_withdrawal_requests WHERE status='paid' AND COALESCE(transfer_confirmed_at, transfer_date, reviewed_at, created_at) >= $1`, cutoff)
	if err != nil {
		return nil, err
	}
	periodOutRefunds, err := treasuryQuerySum(ctx, q, `SELECT COALESCE(SUM(e.amount_toman),0) FROM order_refund_events e JOIN order_refund_obligations o ON o.id=e.obligation_id WHERE e.status='succeeded' AND o.destination='original_payment' AND e.created_at >= $1 AND e.event_no=(SELECT MAX(x.event_no) FROM order_refund_events x WHERE x.obligation_id=e.obligation_id)`, cutoff)
	if err != nil {
		return nil, err
	}

	// --- daily trend (D-15/D-20): any trend error fails the report --------
	inByDay := map[string]int64{}
	outByDay := map[string]int64{}
	trendQuery := func(query string, into map[string]int64) error {
		rows, err := q.QueryContext(ctx, query, cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var day string
			var v int64
			if err := rows.Scan(&day, &v); err != nil {
				return err
			}
			if _, err := treasuryMoney(v); err != nil {
				return err
			}
			into[day] += v
		}
		return rows.Err()
	}
	if err := trendQuery(`SELECT TO_CHAR(verified_at AT TIME ZONE 'UTC','YYYY-MM-DD') AS day, COALESCE(SUM(amount_toman),0) FROM order_payment_receipts WHERE funding_kind='gateway' AND verified_at >= $1 GROUP BY day ORDER BY day`, inByDay); err != nil {
		return nil, err
	}
	if err := trendQuery(`SELECT TO_CHAR(verified_at AT TIME ZONE 'UTC','YYYY-MM-DD') AS day, COALESCE(SUM(amount_toman),0) FROM treasury_opening_cash WHERE verified_at >= $1 GROUP BY day ORDER BY day`, inByDay); err != nil {
		return nil, err
	}
	if err := trendQuery(`SELECT TO_CHAR(COALESCE(transfer_confirmed_at, processed_at, created_at) AT TIME ZONE 'UTC','YYYY-MM-DD') AS day, COALESCE(SUM(amount),0) FROM payout_requests WHERE status IN ('completed','transfer_confirmed') AND COALESCE(transfer_confirmed_at, processed_at, created_at) >= $1 GROUP BY day ORDER BY day`, outByDay); err != nil {
		return nil, err
	}
	if err := trendQuery(`SELECT TO_CHAR(COALESCE(transfer_confirmed_at, transfer_date, reviewed_at, created_at) AT TIME ZONE 'UTC','YYYY-MM-DD') AS day, COALESCE(SUM(amount),0) FROM customer_withdrawal_requests WHERE status='paid' AND COALESCE(transfer_confirmed_at, transfer_date, reviewed_at, created_at) >= $1 GROUP BY day ORDER BY day`, outByDay); err != nil {
		return nil, err
	}
	if err := trendQuery(`SELECT TO_CHAR(e.created_at AT TIME ZONE 'UTC','YYYY-MM-DD') AS day, COALESCE(SUM(e.amount_toman),0) FROM order_refund_events e JOIN order_refund_obligations o ON o.id=e.obligation_id WHERE e.status='succeeded' AND o.destination='original_payment' AND e.created_at >= $1 AND e.event_no=(SELECT MAX(x.event_no) FROM order_refund_events x WHERE x.obligation_id=e.obligation_id) GROUP BY day ORDER BY day`, outByDay); err != nil {
		return nil, err
	}
	labels := make([]string, 0, days)
	for i := days - 1; i >= 0; i-- {
		labels = append(labels, dbNow.UTC().AddDate(0, 0, -i).Format("2006-01-02"))
	}
	trend := make([]treasuryTrendPoint, 0, days)
	for _, day := range labels {
		trend = append(trend, treasuryTrendPoint{Date: day, In: inByDay[day], Out: outByDay[day], Available: true})
	}

	// --- checked assembly (D-17) --------------------------------------------
	// allTimeOut is every outflow liquidCash subtracts: confirmed
	// payouts + paid customer withdrawals + succeeded gateway refunds
	// (a wallet credit is internal, never cash out, so it is not here).
	allTimeOut, err := treasuryCheckedSum([]int64{paidOut, customerPaid, refundSucceededGateway}, nil)
	if err != nil {
		return nil, err
	}
	periodIn, err := treasuryCheckedSum([]int64{periodInReceipts, periodInOpening}, nil)
	if err != nil {
		return nil, err
	}
	periodOut, err := treasuryCheckedSum([]int64{periodOutPayouts, periodOutWithdrawals, periodOutRefunds}, nil)
	if err != nil {
		return nil, err
	}
	refundLiability, err := treasuryCheckedSum([]int64{refundGateway, refundWallet}, nil)
	if err != nil {
		return nil, err
	}
	holds, err := treasuryCheckedSum([]int64{payoutHolds, earlyGross}, nil)
	if err != nil {
		return nil, err
	}
	liability, err := treasuryCheckedSum([]int64{owedToStores, owedToSuppliers, walletLiability, refundLiability, holds, heldInOrders}, nil)
	if err != nil {
		return nil, err
	}
	verifiedIn, err := treasuryCheckedSum([]int64{verifiedReceipts, openingCash}, nil)
	if err != nil {
		return nil, err
	}
	unresolved, err := treasuryCheckedSum([]int64{unverifiedPaidOrders, legacySimulator}, nil)
	if err != nil {
		return nil, err
	}

	// liquidCash is null while no verified cash-in exists and paid-order
	// value is unresolved (D-16). Verified cash-in that exists is
	// reported numerically even while other value is unresolved
	// (cashStatus "partial", G-13).
	var liquidCash *int64
	if verifiedIn == 0 && unresolved > 0 {
		liquidCash = nil
	} else {
		v, err := treasuryCheckedSum([]int64{verifiedIn}, []int64{paidOut, customerPaid, refundSucceededGateway})
		if err != nil {
			return nil, err
		}
		if v < 0 {
			// F-03: a negative computed liquidCash is never rendered
			// as a verified number. Its negativity proves the
			// outflows were funded by unverified cash, so the cash
			// is not verified at all — report it unknown (null,
			// cashStatus "unknown", reconciliationRequired) under
			// the D-16 unknown-cash contract, never as -X Toman.
			liquidCash = nil
		} else {
			liquidCash = &v
		}
	}

	var safeToSpend *int64
	var overObligated bool
	var overObligatedAmount int64
	if liquidCash != nil {
		safeRaw, err := treasuryCheckedSum([]int64{*liquidCash}, []int64{liability})
		if err != nil {
			return nil, err
		}
		if safeRaw < 0 {
			zero := int64(0)
			safeToSpend = &zero
			overObligated = true
			overObligatedAmount = safeRaw
		} else {
			safeToSpend = &safeRaw
		}
	}

	cashStatus := "verified"
	if liquidCash == nil {
		cashStatus = "unknown"
	} else if unresolved > 0 {
		cashStatus = "partial"
	}
	reconciliationRequired := liquidCash == nil || unresolved > 0

	pos.AllTimeIn = allTimeIn
	pos.AllTimeOut = allTimeOut
	pos.OverObligated = overObligated
	pos.OverObligatedAmount = overObligatedAmount
	pos.LiquidCash = liquidCash
	pos.SafeToSpend = safeToSpend
	pos.CashStatus = cashStatus
	pos.ReconciliationRequired = reconciliationRequired
	pos.UnresolvedProviderCash = unresolved
	pos.UnverifiedPaidOrders = unverifiedPaidOrders
	pos.LegacySimulator = legacySimulator
	pos.Liability = liability
	pos.RecoveryReceivableStores = recoveryStores
	pos.RecoveryReceivableSuppliers = recoverySuppliers
	pos.WalletLiability = walletLiability
	pos.RefundLiability = refundLiability
	pos.RefundLiabilityGateway = refundGateway
	pos.RefundLiabilityWallet = refundWallet
	pos.RefundLiabilityReview = refundReview
	pos.Holds = holds
	pos.HoldsByStatus = holdsByStatus
	pos.PeriodIn = periodIn
	pos.PeriodOut = periodOut
	pos.PeriodTrend = trend
	pos.Period = map[string]interface{}{"in": periodIn, "out": periodOut, "trend": trend, "trendAvailable": true}
	pos.HeldOrderCount = heldOrderCount
	pos.HeldInOrders = heldInOrders
	pos.UnreleasedProfitHold = heldInOrders
	pos.OwedToStores = owedToStores
	pos.OwedToSuppliers = owedToSuppliers
	pos.PendingRefundCount = pendingRefundCount
	pos.PendingRefunds = pendingRefunds
	pos.PendingCount = pendingCount
	pos.PendingPayouts = pendingPayouts
	pos.PlatformEarned = platformEarned
	pos.SellerInFlight = sellerInFlight
	pos.SupplierInFlight = supplierInFlight
	// D-06: no writer in the tree produces a paid customer withdrawal
	// (transfer confirmation + ledger debit is a separate follow-up).
	// Approved requests are holds inside the wallet-balance liability;
	// the missing completion path is exposed, not hidden.
	pos.CustomerPayoutCompletion = "not_implemented"
	if reconciliationRequired {
		pos.Note = "Unverified provider cash — reconcile provider/bank evidence before asserting safe-to-spend (FIN-008)."
	}
	return pos, nil
}

// handleTreasuryCashPosition serves GET
// /api/v1/finance/treasury/cash-position (FIN-008). The whole report
// runs in one read-only REPEATABLE READ transaction; any failed read
// or overflow fails the response with no figures (D-15).
func handleTreasuryCashPosition(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	days := t01TreasuryDays(r)
	ctx := r.Context()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer tx.Rollback()
	pos, err := computeTreasuryCashPosition(ctx, tx, days)
	if err != nil {
		if errors.Is(err, repository.ErrFinancePrincipalUnresolved) {
			sellerError(w, err)
			return
		}
		var paidErr *treasuryPaidPayoutError
		if errors.As(err, &paidErr) {
			log.Printf("finance treasury cash-position: %v", paidErr)
			t01WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"success":                false,
				"error":                  paidErr.Error(),
				"reconciliationRequired": true,
			})
			return
		}
		writeInternalError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, pos)
}

// t01SumErr / t01CountErr are the checked variants of t01Sum/t01Count
// for treasury-adjacent endpoints (D-15): a failed read returns its
// error instead of silently rendering 0.
func t01SumErr(ctx context.Context, db *sql.DB, query string, args ...interface{}) (int64, error) {
	var n sql.NullInt64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, fmt.Errorf("treasury sum query returned NULL")
	}
	return treasuryMoney(n.Int64)
}

func t01CountErr(ctx context.Context, db *sql.DB, query string, args ...interface{}) (int64, error) {
	var n int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
