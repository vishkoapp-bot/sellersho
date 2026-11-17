package repository

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
)

// Caller owns the complete canonical finance prefix; no helper reacquires it.
func disputeReleaseStateTx(ctx context.Context, tx *sql.Tx, id string) (string, []string, error) {
	var status string
	var released, referral bool
	var confirmed, ends sql.NullTime
	var profit int64
	var store sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT status,profit_released,referral_commission_released,confirmed_at,return_window_ends_at,dropshipper_profit,store_id FROM orders WHERE id=$1`, id).Scan(&status, &released, &referral, &confirmed, &ends, &profit, &store); err != nil {
		return "", nil, err
	}
	result := func(state, reason string) (string, []string, error) {
		if reason == "" {
			return state, []string{}, nil
		}
		return state, []string{reason}, nil
	}
	if validateSignedFinanceMoney(profit) != nil || profit < 0 {
		return result("needs_review", "recorded_profit_invalid")
	}
	var credits, debits, totalRows int
	var creditAmount, debitAmount int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE target_type='seller' AND target_account=$2 AND type='credit'),COUNT(*) FILTER(WHERE target_type='platform' AND target_account='platform' AND type='debit'),COALESCE(SUM(amount) FILTER(WHERE target_type='seller' AND target_account=$2 AND type='credit'),0),COALESCE(SUM(amount) FILTER(WHERE target_type='platform' AND target_account='platform' AND type='debit'),0) FROM ledger_entries WHERE related_order_or_dispute=$1 AND reason='Staged profit release after return window closed'`, id, store.String).Scan(&totalRows, &credits, &debits, &creditAmount, &debitAmount); err != nil {
		return "", nil, err
	}
	if released {
		if !referral || profit == 0 && totalRows != 0 || profit > 0 && (totalRows != 2 || credits != 1 || debits != 1 || creditAmount != profit || debitAmount != profit) {
			return result("needs_review", "release_history_mismatch")
		}
		return result("released", "")
	}
	if referral || totalRows != 0 {
		return result("needs_review", "release_history_mismatch")
	}
	failed, err := retainedCarrierFailureTx(ctx, tx, id)
	if err != nil {
		return "", nil, err
	}
	if failed {
		return result("held", "carrier_failure")
	}
	var held bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM disputes WHERE order_id=$1 AND status='pending')`, id).Scan(&held); err != nil {
		return "", nil, err
	}
	if held {
		return result("held", "pending_dispute")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,status FROM customer_returns WHERE order_id=$1 ORDER BY id`, id)
	if err != nil {
		return "", nil, err
	}
	type ret struct{ id, status string }
	var returns []ret
	for rows.Next() {
		var r ret
		if err = rows.Scan(&r.id, &r.status); err != nil {
			rows.Close()
			return "", nil, err
		}
		returns = append(returns, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", nil, err
	}
	for _, r := range returns {
		switch r.status {
		case "rejected", "cancelled":
		case "refunded":
			valid, e := provenReturnedEntitlementTx(ctx, tx, id, r.id)
			if e != nil {
				return "", nil, e
			}
			if !valid {
				return result("needs_review", "return_evidence_incomplete")
			}
		default:
			return result("held", "active_return")
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_refund_obligations o WHERE o.order_id=$1 AND (o.purpose<>'customer_return' OR COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=o.id ORDER BY event_no DESC LIMIT 1),o.status)<>'succeeded'))`, id).Scan(&held); err != nil {
		return "", nil, err
	}
	if held {
		return result("needs_review", "refund_entitlement_unproven")
	}
	var reviewHold bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dispute_order_transitions t WHERE t.order_id=$1 AND t.result='needs_review' AND t.reason NOT IN ('pending_dispute','active_return') AND NOT EXISTS(SELECT 1 FROM dispute_order_transitions fixed WHERE fixed.order_id=t.order_id AND fixed.created_at>t.created_at AND fixed.result='restored'))`, id).Scan(&reviewHold); err != nil {
		return "", nil, err
	}
	if reviewHold {
		return result("needs_review", "review_hold")
	}
	if status == "delivery_disputed" {
		return result("needs_review", "hold_evidence_required")
	}
	if status != "delivered_confirmed" {
		return result("awaiting_delivery", "")
	}
	if !confirmed.Valid || !ends.Valid || ends.Time.Before(confirmed.Time) {
		return result("needs_review", "confirmation_window_missing")
	}
	var due bool
	if err = tx.QueryRowContext(ctx, `SELECT return_window_ends_at<=NOW() FROM orders WHERE id=$1`, id).Scan(&due); err != nil {
		return "", nil, err
	}
	if !due {
		return result("awaiting_window", "")
	}
	return result("eligible", "")
}

// Per-return proof; an order-level total is never sufficient entitlement evidence.
func provenReturnedEntitlementTx(ctx context.Context, tx *sql.Tx, orderID, returnID string) (bool, error) {
	var proven bool
	err := tx.QueryRowContext(ctx, `SELECT fin005_return_entitlement_proven($1,$2)`, orderID, returnID).Scan(&proven)
	return proven, err
}
func reconcileDisputeEligibilityTx(ctx context.Context, tx *sql.Tx, orderID string) (string, error) {
	var id, prior, current string
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT f.id,e.prior_status,o.status,e.hold_at IS NOT DISTINCT FROM o.disputed_at AND o.confirmed_at IS NOT DISTINCT FROM e.prior_confirmed_at AND o.return_window_ends_at IS NOT DISTINCT FROM e.prior_window_ends_at FROM disputes f JOIN dispute_order_evidence e ON e.dispute_id=f.id JOIN orders o ON o.id=f.order_id WHERE f.order_id=$1 AND f.status='resolved' AND f.resolution_action='release_to_seller' ORDER BY f.resolution_date DESC,f.id LIMIT 1`, orderID).Scan(&id, &prior, &current, &matches)
	if err == sql.ErrNoRows {
		return "already_current", nil
	}
	if err != nil {
		return "", err
	}
	failed, err := retainedCarrierFailureTx(ctx, tx, orderID)
	if err != nil {
		return "", err
	}
	if failed {
		if _, err = applyCarrierFailureHoldTx(ctx, tx, orderID); err != nil {
			return "", err
		}
		if err = recordDisputeReviewTx(ctx, tx, id, orderID, "system:dispute-reconciliation", "carrier_failure"); err != nil {
			return "", err
		}
		return "needs_review", nil
	}
	var pending bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM disputes WHERE order_id=$1 AND status='pending')`, orderID).Scan(&pending); err != nil {
		return "", err
	}
	reason, result, next := "current_state_preserved", "already_current", current
	if pending {
		reason, result = "pending_dispute", "needs_review"
	} else if current == "delivery_disputed" {
		if !matches {
			reason, result = "hold_linkage_mismatch", "needs_review"
		} else if prior != "shipped" && prior != "delivered_unconfirmed" && prior != "delivered" && prior != "delivered_confirmed" {
			reason, result = "physical_evidence_invalid", "needs_review"
		} else {
			if err = markDisputeWriterTx(ctx, tx); err != nil {
				return "", err
			}
			_, err = tx.ExecContext(ctx, `UPDATE orders o SET status=e.prior_status,disputed_at=e.prior_disputed_at,confirmed_at=e.prior_confirmed_at,return_window_ends_at=e.prior_window_ends_at FROM dispute_order_evidence e WHERE o.id=$1 AND e.dispute_id=$2`, orderID, id)
			if err != nil {
				return "", err
			}
			reason, result, next = "seller_verdict_restoration", "restored", prior
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO dispute_order_transitions(id,dispute_id,order_id,old_status,new_status,actor_user_id,reason,evidence_reference,result) VALUES($1,$2,$3,$4,$5,'system:dispute-reconciliation',$6,$2,$7) ON CONFLICT(dispute_id,reason,result,new_status) DO NOTHING`, uuid.NewString(), id, orderID, current, next, reason, result)
	return result, err
}

func recordDisputeEligibilityTx(ctx context.Context, tx *sql.Tx, id, order, actor string) error {
	state, reasons, err := disputeReleaseStateTx(ctx, tx, order)
	if err != nil {
		return err
	}
	result, reason := "already_current", state
	if state == "needs_review" {
		result = "needs_review"
	}
	if len(reasons) > 0 {
		reason = reasons[0]
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO dispute_order_transitions(id,dispute_id,order_id,old_status,new_status,actor_user_id,reason,evidence_reference,result) SELECT $1,$2,o.id,o.status,o.status,$4,$5,(SELECT dispute_id FROM dispute_order_evidence WHERE dispute_id=$2),$6 FROM orders o WHERE o.id=$3 ON CONFLICT(dispute_id,reason,result,new_status) DO NOTHING`, uuid.NewString(), id, order, actor, reason, result)
	return err
}

// Caller owns the canonical finance prefix. A current failed shipment remains
// authoritative even if an older binary crashed before updating the order.
func retainedCarrierFailureTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error) {
	var failed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier_shipments WHERE order_id=$1 AND status='failed')`, orderID).Scan(&failed)
	return failed, err
}

// applyCarrierFailureHoldTx records the independently accepted failure once.
// Already-disputed admission holds need a new identity; duplicate carrier holds
// do not refresh their timestamp. Confirmation/window/money facts are untouched.
func applyCarrierFailureHoldTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error) {
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&status); err != nil {
		return false, err
	}
	switch status {
	case "processing", "picking", "picked", "packed", "shipped", "delivered_unconfirmed", "delivered":
	case "delivery_disputed":
		var admissionHold bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM dispute_order_evidence e JOIN orders o ON o.id=e.order_id WHERE o.id=$1 AND e.hold_at=o.disputed_at)`, orderID).Scan(&admissionHold); err != nil {
			return false, err
		}
		if !admissionHold {
			return false, nil
		}
	default:
		return false, nil
	}
	if err := markDisputeWriterTx(ctx, tx); err != nil {
		return false, err
	}
	_, err := tx.ExecContext(ctx, `UPDATE orders SET status='delivery_disputed',disputed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, orderID)
	return err == nil, err
}
