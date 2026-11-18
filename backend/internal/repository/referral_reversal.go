package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"github.com/google/uuid"
)

// FIN-011 referral-commission reversals (Q-01 retained-value, owner decision
// Nov 18 ~23:35 Tehran: "کم بشه با برگشت (برگشت کامل = صفر)").
//
// A referral commission is a share of RETAINED order value. The reversal is
// an order-level compensating event in referral_commission_reversals (see
// migration 202611180005), paired with a platform ledger debit, and is
// NEVER a mutation of the order row or of the referral_commission_released
// flag. This file is the single implementation every writer and the
// release gate consult, so the truncation rule cannot drift between them.
//
// Pro-rata base (audit §9): platform profit is order-level only, so the
// returned share is derived from the returned final_unit_price proportion:
//   target = min(trunc(commission * cumulativeRefunded / orderTotal), commission)
//   amount = target - alreadyReversed   (floored at 0)
// The CUMULATIVE form matters: each event is the running total target minus
// what is already reversed, so split returns sum exactly to the
// whole-order target whatever the split (per-event truncation would leak
// up to a Toman per return and could never reconcile). big.Int division
// truncates toward zero, the same rule pricing.TruncatedPercent uses.

// referralReversalTarget is the pure pro-rata rule: the reversal amount
// for bringing an order's cumulative refunded value to cumulativeRefunded,
// given what is already reversed. All amounts are Toman.
func referralReversalTarget(commission, cumulativeRefunded, orderTotal, alreadyReversed int64) int64 {
	if commission <= 0 || cumulativeRefunded <= 0 || orderTotal <= 0 || alreadyReversed >= commission {
		return 0
	}
	target := new(big.Int).Mul(big.NewInt(commission), big.NewInt(cumulativeRefunded))
	target.Quo(target, big.NewInt(orderTotal))
	if target.Cmp(big.NewInt(commission)) > 0 {
		target.SetInt64(commission)
	}
	amount := new(big.Int).Sub(target, big.NewInt(alreadyReversed))
	if !amount.IsInt64() || amount.Sign() <= 0 {
		return 0
	}
	return amount.Int64()
}

// referralCommissionOrderSumsTx reads the order-time referral snapshot and
// the totals the reversal rule needs. orderTotal is the merchandise value
// (SUM final_unit_price*quantity); a 0 total means the commission has no
// value base and no reversal is computable.
func referralCommissionOrderSumsTx(ctx context.Context, tx *sql.Tx, orderID string) (referrer sql.NullString, commission, orderTotal, alreadyReversed int64, err error) {
	if err = tx.QueryRowContext(ctx, `SELECT referrer_seller_id, COALESCE(referral_commission,0) FROM orders WHERE id=$1`, orderID).Scan(&referrer, &commission); err != nil {
		return referrer, 0, 0, 0, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(final_unit_price*quantity),0) FROM order_items WHERE order_id=$1`, orderID).Scan(&orderTotal); err != nil {
		return referrer, 0, 0, 0, err
	}
	// No referrer / no commission: nothing could ever have been written
	// to referral_commission_reversals for this order, so the recorded
	// total is 0 by construction. Do NOT touch the table here:
	// minimal-schema fixtures (FIN-005 notifyWritersTestDB) do not
	// create it, and the release gate must not fail on non-referral
	// orders there. Referral orders always reach the query below (the
	// table exists post-migration 202611180005); if it is missing for a
	// referral order, the query errors loudly instead of silently passing.
	if !referrer.Valid || referrer.String == "" || commission <= 0 {
		return referrer, commission, orderTotal, 0, nil
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM referral_commission_reversals WHERE order_id=$1`, orderID).Scan(&alreadyReversed); err != nil {
		return referrer, 0, 0, 0, err
	}
	return referrer, commission, orderTotal, alreadyReversed, nil
}

// referralSucceededRefundsTx is the money record of refunded value: the
// SUM of the order's refund obligations whose current state is 'succeeded'
// (latest order_refund_events row wins, else the obligation status).
// It covers wallet returns, dispute refunds and admin refunds uniformly.
func referralSucceededRefundsTx(ctx context.Context, tx *sql.Tx, orderID string) (int64, error) {
	var refunded int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(ob.amount_toman),0) FROM order_refund_obligations ob WHERE ob.order_id=$1 AND COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=ob.id ORDER BY e.event_no DESC LIMIT 1), ob.status)='succeeded'`, orderID).Scan(&refunded)
	return refunded, err
}

// insertReferralCommissionReversalTx writes one reversal row plus its
// platform ledger debit (target: the referrer store, type debit). Both
// writes are idempotent on the source key, so a replayed refund writes
// nothing twice; the caller must hold the order finance lock.
func insertReferralCommissionReversalTx(ctx context.Context, tx *sql.Tx, orderID, referrerStoreID, source, sourceID, reason, actorUserID string, amount int64) error {
	idemKey := "rcr:" + source + ":" + sourceID
	ledgerID := "RCR-LED-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(idemKey)).String()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute)
		VALUES ($1, NOW(), $2, 'seller', 'debit', $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`, ledgerID, referrerStoreID, amount, reason, actorUserID, sourceID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO referral_commission_reversals (id, order_id, referrer_store_id, amount, source, source_id, ledger_entry_id, idempotency_key, reason, actor_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, "RCR-"+uuid.NewString(), orderID, referrerStoreID, amount, source, sourceID, ledgerID, idemKey, reason, actorUserID)
	return err
}

// recordReferralCommissionReversalForReturnTx writes the referral reversal
// for one refunded customer return, inside RefundCustomerReturn's
// transaction. cumulativeRefunded is the return's own refunded value plus
// every earlier refunded return of the same order (returns-table sums,
// the same source RefundCustomerReturn refunds from; fin005 proves them
// equal to the obligation sums before any release).
func recordReferralCommissionReversalForReturnTx(ctx context.Context, tx *sql.Tx, returnID, orderID, actorUserID string) error {
	referrer, commission, orderTotal, alreadyReversed, err := referralCommissionOrderSumsTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if !referrer.Valid || referrer.String == "" || commission <= 0 || orderTotal <= 0 {
		return nil
	}
	var thisReturn, priorRefunded int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(oi.final_unit_price*ri.qty),0)
		FROM return_items ri JOIN order_items oi ON oi.id::text = ri.order_item_id
		WHERE ri.return_id = $1 AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok', 'defective')
	`, returnID).Scan(&thisReturn); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(oi.final_unit_price*ri.qty),0)
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN customer_returns cr ON cr.id = ri.return_id
		WHERE cr.order_id = $1 AND cr.status = 'refunded' AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok', 'defective')
	`, orderID).Scan(&priorRefunded); err != nil {
		return err
	}
	amount := referralReversalTarget(commission, priorRefunded+thisReturn, orderTotal, alreadyReversed)
	if amount <= 0 {
		return nil
	}
	reason := fmt.Sprintf("برگشت کمیسیون معرفی — مرجوعی مشتری %s", returnID)
	return insertReferralCommissionReversalTx(ctx, tx, orderID, referrer.String, "customer_return", returnID, reason, actorUserID, amount)
}

// recordReferralCommissionReversalForOrderRefundTx writes the referral
// reversal when an original_payment refund obligation reaches 'succeeded'
// (dispute / admin_refund). Call it only on the succeeded transition,
// inside the same transaction that records the outcome, while the order
// finance lock is held. Customer-return (wallet) refunds are handled by
// recordReferralCommissionReversalForReturnTx instead; cancellations
// write nothing (a cancelled order can never release, so its commission
// is never earned).
func recordReferralCommissionReversalForOrderRefundTx(ctx context.Context, tx *sql.Tx, obligationID, actorUserID string) error {
	var orderID, purpose, businessID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id, purpose, business_id FROM order_refund_obligations WHERE id=$1`, obligationID).Scan(&orderID, &purpose, &businessID); err != nil {
		return err
	}
	if purpose != "dispute" && purpose != "admin_refund" {
		return nil
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&status); err != nil {
		return err
	}
	if status == "cancelled" {
		return nil
	}
	referrer, commission, orderTotal, alreadyReversed, err := referralCommissionOrderSumsTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if !referrer.Valid || referrer.String == "" || commission <= 0 || orderTotal <= 0 {
		return nil
	}
	cumulative, err := referralSucceededRefundsTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	amount := referralReversalTarget(commission, cumulative, orderTotal, alreadyReversed)
	if amount <= 0 {
		return nil
	}
	reason := fmt.Sprintf("برگشت کمیسیون معرفی — استرداد %s", businessID)
	return insertReferralCommissionReversalTx(ctx, tx, orderID, referrer.String, purpose, obligationID, reason, actorUserID, amount)
}

// referralReversalCompleteTx is the release-gate completeness check: the
// order's recorded referral reversals must equal the total the succeeded
// refunds imply (same cumulative rule as the writers). A mismatch holds
// the release for review; it never errors and never silently passes.
// Orders without a referrer or commission pass trivially (expected 0).
func referralReversalCompleteTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error) {
	referrer, commission, orderTotal, alreadyReversed, err := referralCommissionOrderSumsTx(ctx, tx, orderID)
	if err != nil {
		return false, err
	}
	if !referrer.Valid || referrer.String == "" || commission <= 0 {
		return alreadyReversed == 0, nil
	}
	if orderTotal <= 0 {
		// No value base: any succeeded refund is incoherent data.
		refunded, err := referralSucceededRefundsTx(ctx, tx, orderID)
		if err != nil {
			return false, err
		}
		return refunded == 0 && alreadyReversed == 0, nil
	}
	refunded, err := referralSucceededRefundsTx(ctx, tx, orderID)
	if err != nil {
		return false, err
	}
	expected := referralReversalTarget(commission, refunded, orderTotal, 0)
	return alreadyReversed == expected, nil
}

// referredStoreHasRetainedClosedOrderTx implements Q-02/I-01: the locked
// referral bonus unlocks only when the referred store has >=1 order that
// CLOSED its 7-day return window (profit_released) with retained value
// > 0. Retained value = merchandise value (SUM final_unit_price*quantity;
// an order with no items falls back to payable_amount, for legacy
// item-less rows) minus succeeded refunds. A fully returned first order
// retains 0 and does not unlock; a later order closing with retained
// value > 0 unlocks the flat 100,000 bonus.
func referredStoreHasRetainedClosedOrderTx(ctx context.Context, tx *sql.Tx, storeID string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, COALESCE(payable_amount,0) FROM orders WHERE store_id=$1 AND profit_released AND status='delivered_confirmed'`, storeID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	type closedOrder struct {
		id      string
		payable int64
	}
	var orders []closedOrder
	for rows.Next() {
		var o closedOrder
		if err := rows.Scan(&o.id, &o.payable); err != nil {
			return false, err
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, o := range orders {
		var merchandise int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(final_unit_price*quantity),0) FROM order_items WHERE order_id=$1`, o.id).Scan(&merchandise); err != nil {
			return false, err
		}
		base := merchandise
		if base <= 0 {
			base = o.payable
		}
		refunded, err := referralSucceededRefundsTx(ctx, tx, o.id)
		if err != nil {
			return false, err
		}
		if base > refunded {
			return true, nil
		}
	}
	return false, nil
}
