package repository

import (
	"context"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/lib/pq"
)

type OrderCancellationResult struct {
	Success bool                      `json:"success"`
	OrderID string                    `json:"orderId"`
	Status  string                    `json:"status"`
	Refund  domain.OrderRefundSummary `json:"refund"`
}

func (r *OrderRepository) CancelOrderWithRefund(ctx context.Context, idOrNumber, customer string, admin bool) (OrderCancellationResult, error) {
	var result OrderCancellationResult
	q := `SELECT id FROM orders WHERE (id=$1 OR order_number=$1)`
	args := []any{idOrNumber}
	if !admin {
		q += ` AND customer_user_id=$2::uuid`
		args = append(args, customer)
	}
	var id string
	if err := r.db.QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
		return result, err
	}
	// Ownership and child shipment state are rechecked under the mutation guard.
	if err := r.cancelOrder(ctx, id, customer, admin, ""); err != nil {
		return result, err
	}
	orders := []domain.Order{{ID: id}}
	if err := r.attachRefundSummaries(ctx, orders); err != nil {
		return result, err
	}
	return OrderCancellationResult{true, id, "cancelled", *orders[0].Refund}, nil
}

// Bounded batched projection; no tokens, provider references, raw source IDs or
// another customer's journal appear in the customer DTO. Read errors fail closed.
func (r *OrderRepository) attachRefundSummaries(ctx context.Context, orders []domain.Order) error {
	for start := 0; start < len(orders); start += 200 {
		end := min(start+200, len(orders))
		ids := make([]string, 0, end-start)
		byID := map[string]int{}
		for i := start; i < end; i++ {
			ids = append(ids, orders[i].ID)
			byID[orders[i].ID] = i
		}
		rows, err := r.db.QueryContext(ctx, `WITH claims AS (
 SELECT o.*,COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=o.id ORDER BY event_no DESC LIMIT 1),CASE WHEN o.status='unknown' AND o.destination<>'review' AND EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.payment_id=o.source_payment_id AND r.funding_kind='gateway' AND r.amount_toman=o.source_amount_toman) THEN 'pending' ELSE o.status END) AS current_state
 FROM order_refund_obligations o WHERE o.order_id=ANY($1)
) SELECT o.id,COALESCE(SUM(c.amount_toman),0),COALESCE(SUM(c.amount_toman) FILTER(WHERE c.current_state='pending'),0),COALESCE(SUM(c.amount_toman) FILTER(WHERE c.current_state='succeeded'),0),COALESCE(SUM(c.amount_toman) FILTER(WHERE c.current_state='failed'),0),COALESCE(SUM(c.amount_toman) FILTER(WHERE c.current_state='unknown'),0),COUNT(DISTINCT c.destination),COALESCE(MIN(c.destination),'none'),
 COALESCE((to_jsonb(o)->>'wallet_amount')::bigint,0)>0
 OR EXISTS(SELECT 1 FROM order_payment_verifications v JOIN order_payment_verification_events e ON e.verification_id=v.id WHERE v.order_id=o.id AND e.status='amount_mismatch')
 OR (o.status='cancelled' AND EXISTS(SELECT 1 FROM order_payment_verifications v WHERE v.order_id=o.id AND NOT EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.verification_id=v.id)))
 OR (o.status='cancelled' AND COUNT(c.id)=0 AND EXISTS(SELECT 1 FROM payment_transactions p WHERE p.order_id=o.id AND p.status IN ('paid','refunded')))
 FROM orders o LEFT JOIN claims c ON c.order_id=o.id WHERE o.id=ANY($1) GROUP BY o.id`, pq.Array(ids))
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var s domain.OrderRefundSummary
			var destinations int
			if err = rows.Scan(&id, &s.AmountToman, &s.PendingToman, &s.SucceededToman, &s.FailedToman, &s.UnknownToman, &destinations, &s.Destination, &s.NeedsReview); err != nil {
				rows.Close()
				return err
			}
			for _, n := range []int64{s.AmountToman, s.PendingToman, s.SucceededToman, s.FailedToman, s.UnknownToman} {
				if n < 0 || n > pricing.MaxTransportMoney {
					rows.Close()
					return pricing.ErrInvalidMoney
				}
			}
			s.Currency = "TOMAN"
			s.NeedsReview = s.NeedsReview || s.UnknownToman > 0 || s.FailedToman > 0
			if destinations > 1 {
				s.Destination = "mixed"
			}
			s.Status = "none"
			switch {
			case s.UnknownToman > 0:
				s.Status = "unknown"
			case s.FailedToman > 0:
				s.Status = "failed"
			case s.NeedsReview:
				s.Status = "unknown"
			case s.PendingToman > 0:
				s.Status = "pending"
			case s.SucceededToman > 0:
				s.Status = "succeeded"
			}
			orders[byID[id]].Refund = &s
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
