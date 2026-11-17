package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
)

var ErrRefundPrincipalUnresolved = errors.New("refund source principal requires reconciliation")

// Caller owns the order/payment/account/product guard and cancellation transaction.
// Recorded paid rows create a reserved unknown obligation, never proof of cash
// moving or an automatic wallet credit. Confirmed receipts may later drive PSP work.
func ensureCancellationRefundsTx(ctx context.Context, tx *sql.Tx, orderID, actor string) error {
	sources, err := refundRecordedSourcesTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}
	if err = reserveLegacyRefundEvidenceTx(ctx, tx, orderID, actor, sources); err != nil {
		return err
	}
	for _, s := range sources {
		if s.amount <= 0 || s.amount > pricing.MaxTransportMoney {
			return pricing.ErrInvalidMoney
		}
		var existing int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_refund_obligations WHERE purpose='cancellation' AND business_id=$1 AND source_payment_id=$2`, orderID, s.id).Scan(&existing); err != nil {
			return err
		}
		if existing == 1 {
			continue
		}
		var used int64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE source_payment_id=$1`, s.id).Scan(&used); err != nil {
			return err
		}
		if used < 0 || used > s.amount {
			return ErrRefundPrincipalUnresolved
		}
		amount := s.amount - used
		if amount == 0 {
			continue
		}
		id := "ORF-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("cancellation:"+orderID+":"+s.id)).String()
		if _, err = tx.ExecContext(ctx, `INSERT INTO order_refund_obligations(id,order_id,source_payment_id,customer_id,source_amount_toman,amount_toman,currency,purpose,business_id,destination,policy_version,status,actor_user_id) VALUES($1,$2,$3,$4::uuid,$5,$6,'TOMAN','cancellation',$2,'original_payment','original_payment_v1','unknown',$7)`, id, orderID, s.id, s.customer, s.amount, amount, actor); err != nil {
			return err
		}
	}
	return nil
}

type refundRecordedSource struct {
	id       string
	amount   int64
	customer sql.NullString
	status   string
}

func reserveLegacyRefundEvidenceTx(ctx context.Context, tx *sql.Tx, orderID, actor string, sources []refundRecordedSource) error {
	// Legacy PSP claims have no trustworthy source linkage or outcome event.
	// Hold the remaining pool once; ledger/status projections may describe the
	// same claim and must not be summed into a second obligation.
	var oldPSPClaim bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM refunds WHERE order_id=$1)`, orderID).Scan(&oldPSPClaim); err != nil {
		return err
	}
	if oldPSPClaim {
		for _, s := range sources {
			available, e := refundSourceAvailableTx(ctx, tx, s)
			if e != nil {
				return e
			}
			if available > 0 {
				if e = insertLegacyRefundHoldTx(ctx, tx, orderID, actor, "refund-history:"+orderID, s, available); e != nil {
					return e
				}
			}
		}
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,amount FROM ledger_entries WHERE related_order_or_dispute=$1 AND target_account=$1 AND target_type='platform' AND type='credit' AND reason LIKE 'استرداد وجه سفارش:%' ORDER BY id COLLATE "C" LIMIT 201`, orderID)
	if err != nil {
		return err
	}
	type oldClaim struct {
		id     string
		amount int64
	}
	var claims []oldClaim
	for rows.Next() {
		var c oldClaim
		if err = rows.Scan(&c.id, &c.amount); err != nil {
			rows.Close()
			return err
		}
		claims = append(claims, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(claims) > 200 {
		return ErrRefundPrincipalUnresolved
	}
	// An old order-level refund does not identify which of multiple captures
	// funded it. Never choose an arbitrary PSP transaction for the remainder.
	if len(claims) > 0 && len(sources) > 1 {
		for _, s := range sources {
			available, e := refundSourceAvailableTx(ctx, tx, s)
			if e != nil {
				return e
			}
			if available > 0 {
				if e = insertLegacyRefundHoldTx(ctx, tx, orderID, actor, "ambiguous-history:"+orderID, s, available); e != nil {
					return e
				}
			}
		}
		return nil
	}
	for _, c := range claims {
		if c.amount <= 0 || c.amount > pricing.MaxTransportMoney {
			return pricing.ErrInvalidMoney
		}
		business := "ledger:" + c.id
		var allocated int64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE purpose='legacy_reservation' AND business_id=$1`, business).Scan(&allocated); err != nil {
			return err
		}
		if allocated < 0 || allocated > c.amount {
			return ErrRefundPrincipalUnresolved
		}
		left := c.amount - allocated
		for _, s := range sources {
			available, e := refundSourceAvailableTx(ctx, tx, s)
			if e != nil {
				return e
			}
			take := min(left, available)
			if take > 0 {
				if e = insertLegacyRefundHoldTx(ctx, tx, orderID, actor, business, s, take); e != nil {
					return e
				}
				left -= take
			}
			if left == 0 {
				break
			}
		}
		if left != 0 {
			return ErrRefundPrincipalUnresolved
		}
	}
	var unprovedReturn bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM customer_returns r WHERE r.order_id=$1 AND r.status='refunded' AND NOT EXISTS(SELECT 1 FROM order_refund_obligations c WHERE c.purpose='customer_return' AND c.business_id=r.id::text))`, orderID).Scan(&unprovedReturn); err != nil {
		return err
	}
	if unprovedReturn {
		for _, s := range sources {
			available, e := refundSourceAvailableTx(ctx, tx, s)
			if e != nil {
				return e
			}
			if available > 0 {
				if e = insertLegacyRefundHoldTx(ctx, tx, orderID, actor, "return-history:"+orderID, s, available); e != nil {
					return e
				}
			}
		}
	}
	var unprovedDispute bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM disputes d WHERE d.order_id=$1 AND d.status='resolved' AND d.resolution_action LIKE 'refund%' AND d.claim_amount>0 AND NOT EXISTS(SELECT 1 FROM order_refund_obligations c WHERE c.purpose='dispute' AND c.business_id=d.id))`, orderID).Scan(&unprovedDispute); err != nil {
		return err
	}
	if unprovedDispute {
		for _, s := range sources {
			available, e := refundSourceAvailableTx(ctx, tx, s)
			if e != nil {
				return e
			}
			if available > 0 {
				if e = insertLegacyRefundHoldTx(ctx, tx, orderID, actor, "dispute-history:"+orderID, s, available); e != nil {
					return e
				}
			}
		}
	}
	for _, s := range sources {
		if s.status != "refunded" {
			continue
		}
		available, e := refundSourceAvailableTx(ctx, tx, s)
		if e != nil {
			return e
		}
		if available > 0 {
			if e = insertLegacyRefundHoldTx(ctx, tx, orderID, actor, "payment-refunded:"+s.id, s, available); e != nil {
				return e
			}
		}
	}
	return nil
}

func refundSourceAvailableTx(ctx context.Context, tx *sql.Tx, s refundRecordedSource) (int64, error) {
	if s.amount <= 0 || s.amount > pricing.MaxTransportMoney {
		return 0, pricing.ErrInvalidMoney
	}
	var used int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE source_payment_id=$1`, s.id).Scan(&used); err != nil {
		return 0, err
	}
	if used < 0 || used > s.amount {
		return 0, ErrRefundPrincipalUnresolved
	}
	return s.amount - used, nil
}

func insertLegacyRefundHoldTx(ctx context.Context, tx *sql.Tx, orderID, actor, business string, s refundRecordedSource, amount int64) error {
	id := "ORF-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("legacy:"+business+":"+s.id)).String()
	_, err := tx.ExecContext(ctx, `INSERT INTO order_refund_obligations(id,order_id,source_payment_id,customer_id,source_amount_toman,amount_toman,currency,purpose,business_id,destination,policy_version,status,actor_user_id) VALUES($1,$2,$3,$4::uuid,$5,$6,'TOMAN','legacy_reservation',$7,'review','legacy_review_v1','unknown',$8)`, id, orderID, s.id, s.customer, s.amount, amount, business, actor)
	return err
}

// Order/account/product prefix is held. This path never captures inventory.
func recordLatePaidCancellationTx(ctx context.Context, tx *sql.Tx, p *PaymentTransaction, rrn, mask string) error {
	if err := assertOrderCancellationAllowedTx(ctx, tx, p.OrderID); err != nil {
		return err
	}
	var captured bool
	if err := tx.QueryRowContext(ctx, `SELECT inventory_captured FROM orders WHERE id=$1`, p.OrderID).Scan(&captured); err != nil {
		return err
	}
	if captured {
		return ErrRefundPrincipalUnresolved
	}
	// Cancellation closes fulfillment admission before classifying a legacy
	// booked capture. Its unknown-source claim must cover it before COMMIT; it
	// cannot authorize a real PSP transfer or wallet credit without a receipt.
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status='cancelled',payment_status='paid',updated_at=NOW() WHERE id=$1`, p.OrderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_transactions SET status='paid',rrn=$2,card_mask=$3,compensation_capture=TRUE WHERE id=$1 AND status<>'refunded'`, p.ID, rrn, mask); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_shipments SET status='cancelled',updated_at=NOW() WHERE order_id=$1 AND status IN ('pending','processing')`, p.OrderID); err != nil {
		return err
	}
	if err := cancelOrderCouponTx(ctx, tx, p.OrderID, "late_payment"); err != nil {
		return err
	}
	return ensureCancellationRefundsTx(ctx, tx, p.OrderID, "system:late-payment")
}
