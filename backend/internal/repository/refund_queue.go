package repository

import (
	"context"
	"time"

	"github.com/drup/platform/internal/pricing"
)

// FIN-013 §2.4: the unknown-outcome work queue as an admin surface.
// Unknown refunds were queryable in SQL only; this is the read side the
// admin panel lists, and the manual trigger's counterpart. The rows
// carry no provider reference, token, or customer PII — only the
// obligation identity, the order, the amount (Toman), and the age the
// operator triages on.

// UnknownRefundQueueItem is one row of the unknown-outcome work queue.
type UnknownRefundQueueItem struct {
	ObligationID string    `json:"obligationId"`
	OrderID      string    `json:"orderId"`
	AmountToman  int64     `json:"amountToman"`
	Purpose      string    `json:"purpose"`
	Destination  string    `json:"destination"`
	CurrentState string    `json:"currentState"`
	CreatedAt    time.Time `json:"createdAt"`
	AgeSeconds   int64     `json:"ageSeconds"`
}

// ListUnknownRefunds returns the oldest unknown-outcome refund
// obligations first, bounded to limit rows (1..100). The predicate is
// the work-queue predicate (stored status 'unknown', served index-only
// by order_refund_obligations_work); CurrentState applies the same
// latest-event resolution the treasury and the summary use, so an
// obligation the reconciler already moved to 'pending' shows as such
// instead of disappearing from the list. A nil provider does not
// affect this read — the queue is visible even while FIN-010 blocks
// execution.
func (r *OrderRepository) ListUnknownRefunds(ctx context.Context, limit int) ([]UnknownRefundQueueItem, error) {
	if limit < 1 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT o.id,o.order_id,o.amount_toman,o.purpose,o.destination,COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=o.id ORDER BY e.event_no DESC LIMIT 1),CASE WHEN o.status='unknown' AND o.destination<>'review' AND EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.payment_id=o.source_payment_id AND r.funding_kind='gateway' AND r.amount_toman=o.source_amount_toman) THEN 'pending' ELSE o.status END),o.created_at,EXTRACT(EPOCH FROM (clock_timestamp()-o.created_at))::bigint FROM order_refund_obligations o WHERE o.status='unknown' ORDER BY o.created_at,o.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UnknownRefundQueueItem, 0, limit)
	for rows.Next() {
		var item UnknownRefundQueueItem
		if err := rows.Scan(&item.ObligationID, &item.OrderID, &item.AmountToman, &item.Purpose, &item.Destination, &item.CurrentState, &item.CreatedAt, &item.AgeSeconds); err != nil {
			return nil, err
		}
		if err := treasuryMoneyCheck(item.AmountToman); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// treasuryMoneyCheck is the repository-side FIN-012 bound check for a
// single money value read off a queue row. The treasury handler owns
// the same check for report sums; this keeps the queue read from
// returning an out-of-range amount to an admin client.
func treasuryMoneyCheck(v int64) error {
	if v > pricing.MaxTransportMoney || v < -pricing.MaxTransportMoney {
		return pricing.ErrInvalidMoney
	}
	return nil
}
