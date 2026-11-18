package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/drup/platform/internal/payment"
	"github.com/google/uuid"
)

// ProcessOrderRefund starts at most one transfer operation per immutable claim.
// Once an attempt is durable, every subsequent run uses inquiry only. A timeout,
// crash or unknown outcome must never release or redirect its reserved principal.
// nil is the production configuration until FIN-010 adopts a real PSP adapter.
func (r *OrderRepository) ProcessOrderRefund(ctx context.Context, id string, provider payment.RefundProvider) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	_, err = lockRefundOrderTx(ctx, tx, id)
	if err != nil {
		return "", err
	}
	state, err := currentRefundStateTx(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if state == "succeeded" || provider == nil {
		return state, nil
	}
	var request payment.RefundRequest
	var reference sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT 'refund:'||o.id,o.source_payment_id,o.amount_toman,o.currency,o.destination,r.provider_reference FROM order_refund_obligations o LEFT JOIN order_payment_receipts r ON r.payment_id=o.source_payment_id AND r.funding_kind='gateway' WHERE o.id=$1`, id).Scan(&request.OperationKey, &request.PaymentID, &request.AmountToman, &request.Currency, &request.Destination, &reference); err != nil {
		return state, err
	}
	if request.Destination != "original_payment" || !reference.Valid {
		return state, ErrRefundPrincipalUnresolved
	}
	request.OriginalReference = reference.String
	var attempted bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM order_refund_attempts WHERE obligation_id=$1)`, id).Scan(&attempted); err != nil {
		return state, err
	}
	if !attempted {
		if _, err = tx.ExecContext(ctx, `INSERT INTO order_refund_attempts(id,obligation_id,source_payment_id,amount_toman,currency,destination,operation_key,original_reference) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, "ORFA-"+uuid.NewSHA1(uuid.NameSpaceURL, []byte(id)).String(), id, request.PaymentID, request.AmountToman, request.Currency, request.Destination, request.OperationKey, request.OriginalReference); err != nil {
			return state, err
		}
		if err = appendRefundOutcomeTx(ctx, tx, id, "unknown", "", "provider_request_started"); err != nil {
			return state, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO order_refund_poll_state(obligation_id,next_reconcile_at) VALUES($1,clock_timestamp()+INTERVAL '5 minutes') ON CONFLICT(obligation_id) DO UPDATE SET next_reconcile_at=EXCLUDED.next_reconcile_at`, id); err != nil {
		return state, err
	}
	if err = tx.Commit(); err != nil {
		return state, err
	}

	// Neither an SQL transaction nor an account/stock lock spans network I/O.
	var outcome *payment.RefundOutcome
	if attempted {
		outcome, err = provider.LookupRefund(ctx, request)
	} else {
		outcome, err = provider.Refund(ctx, request)
	}
	status, providerReference, reason := "unknown", "", "provider_result_unknown"
	var identityError error
	if err == nil && outcome != nil {
		if outcome.OperationKey != request.OperationKey || outcome.PaymentID != request.PaymentID || outcome.AmountToman != request.AmountToman || outcome.Currency != request.Currency || outcome.Destination != request.Destination || (outcome.Status != "succeeded" && outcome.Status != "failed" && outcome.Status != "unknown" && outcome.Status != "pending") || (outcome.Status == "succeeded" && strings.TrimSpace(outcome.ProviderReference) == "") {
			reason, identityError = "provider_result_mismatch", ErrPaymentVerificationIdentity
		} else {
			status, providerReference, reason = outcome.Status, outcome.ProviderReference, "provider_confirmed_"+outcome.Status
		}
	}
	// A cancelled process can leave the retained attempt unresolved; a later
	// bounded pass inquires. Never claim an outcome that failed to commit.
	state, err = r.recordRefundOutcome(ctx, id, status, providerReference, reason)
	if err != nil {
		return "unknown", err
	}
	return state, identityError
}

func lockRefundOrderTx(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var orderID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM order_refund_obligations WHERE id=$1`, id).Scan(&orderID); err != nil {
		return "", err
	}
	return orderID, lockOrderFinanceTx(ctx, tx, orderID, true)
}

// One SQL snapshot projects immutable initial evidence plus subsequent outcomes.
func currentRefundStateTx(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var state string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=o.id ORDER BY e.event_no DESC LIMIT 1),CASE WHEN o.status='unknown' AND o.destination<>'review' AND EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.payment_id=o.source_payment_id AND r.funding_kind='gateway' AND r.amount_toman=o.source_amount_toman) THEN 'pending' ELSE o.status END) FROM order_refund_obligations o WHERE o.id=$1`, id).Scan(&state)
	return state, err
}

func appendRefundOutcomeTx(ctx context.Context, tx *sql.Tx, id, status, reference, reason string) error {
	eventID := "ORFE-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(id+":"+status+":"+reference+":"+reason)).String()
	_, err := tx.ExecContext(ctx, `INSERT INTO order_refund_events(id,obligation_id,source_payment_id,amount_toman,currency,destination,status,operation_key,provider_reference,reason) SELECT $1,id,source_payment_id,amount_toman,currency,destination,$3,'refund:'||id,$4,$5 FROM order_refund_obligations WHERE id=$2 ON CONFLICT(id) DO NOTHING`, eventID, id, status, reference, reason)
	return err
}

func (r *OrderRepository) recordRefundOutcome(ctx context.Context, id, status, reference, reason string) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = lockRefundOrderTx(ctx, tx, id); err != nil {
		return "", err
	}
	state, err := currentRefundStateTx(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if state == "succeeded" {
		// A concurrent inquiry may have completed while this call was in flight.
		return state, nil
	}
	if err = appendRefundOutcomeTx(ctx, tx, id, status, reference, reason); err != nil {
		return "", err
	}
	// FIN-011 (Q-01 retained-value): when an original_payment refund
	// reaches 'succeeded', the referral commission reverses pro-rata
	// (dispute / admin_refund). The reversal is a compensating event
	// in referral_commission_reversals, never a mutation of the order
	// row or the released flag; a party that already withdrew the
	// money goes negative, the established pattern for return charges.
	// Customer-return (wallet) refunds reverse in
	// recordReturnReversalsTx instead; cancellations write nothing.
	if status == "succeeded" {
		if err = recordReferralCommissionReversalForOrderRefundTx(ctx, tx, id, "system:refund-execution"); err != nil {
			return "unknown", err
		}
		// FIN-009 (D-01): the same succeeded transition reverses the
		// seller's released profit pro-rata (dispute / admin_refund).
		// The reversal is a compensating event in
		// seller_profit_reversals, never a mutation of the order row
		// or the released flag; a party that already withdrew the
		// money goes negative, the established pattern for return
		// charges. Customer-return (wallet) refunds reverse in
		// recordReturnReversalsTx instead; cancellations write nothing.
		if err = recordSellerProfitReversalForOrderRefundTx(ctx, tx, id, "system:refund-execution"); err != nil {
			return "unknown", err
		}
	}
	return status, tx.Commit()
}

// ReconcileOrderRefunds is bounded, and unavailable adapters leave the durable
// outbox pending. Real provider wiring is deliberately deferred to FIN-010.
func (r *OrderRepository) ReconcileOrderRefunds(ctx context.Context, provider payment.RefundProvider, limit int) (int, error) {
	if provider == nil {
		return 0, nil
	}
	if limit < 1 || limit > 100 {
		return 0, ErrStoreInput
	}
	rows, err := r.db.QueryContext(ctx, `SELECT o.id FROM order_refund_obligations o WHERE o.destination='original_payment' AND NOT EXISTS(SELECT 1 FROM order_refund_poll_state s WHERE s.obligation_id=o.id AND s.next_reconcile_at>clock_timestamp()) AND EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.payment_id=o.source_payment_id AND r.funding_kind='gateway') AND NOT EXISTS(SELECT 1 FROM order_refund_events e WHERE e.obligation_id=o.id AND e.status='succeeded') ORDER BY o.created_at,o.id COLLATE "C" LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	completed := 0
	var failures []error
	for _, id := range ids {
		state, err := r.ProcessOrderRefund(ctx, id, provider)
		if err != nil {
			failures = append(failures, err)
		} else if state == "succeeded" {
			completed++
		}
	}
	return completed, errors.Join(failures...)
}
