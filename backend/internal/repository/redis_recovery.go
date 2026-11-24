package repository

import (
	"context"
	"errors"
	"time"
)

// RebuildInventoryHolds re-creates Redis holds for live pending orders from
// canonical PostgreSQL state (PERF-04 §4.5). It is the MANUAL recovery path
// after Redis data loss — see docs/operations/redis-recovery.md — and is
// deliberately not wired to boot: auto-rebuild risks resurrecting holds for
// orders whose payment already failed, while the manual path runs with PG
// as the authority and a human watching the count.
//
// A hold is rebuilt only for an order that is still payable: payment
// pending, status pending/processing, inventory not yet captured, and the
// payment deadline still in the future. Cancelled, expired, paid, and
// captured orders never get a hold back. Re-holding an order that already
// has one is idempotent (same member, quantity replaced, deadline renewed
// to the order's own deadline capped at 15 minutes, as in
// reserveOrderInventory). Per-order failures are joined and do not stop
// the remaining orders; the returned count is the number of pools rebuilt.
func (r *OrderRepository) RebuildInventoryHolds(ctx context.Context, redisClient *RedisClient) (rebuilt int, err error) {
	if redisClient == nil {
		return 0, errors.New("redis client unavailable")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.id, o.payment_expires_at FROM orders o
		WHERE o.payment_status = 'pending' AND o.status IN ('pending','processing')
		  AND NOT o.inventory_captured AND o.payment_expires_at > NOW()
		ORDER BY o.id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type payableOrder struct {
		id       string
		deadline time.Time
	}
	var orders []payableOrder
	for rows.Next() {
		var o payableOrder
		if err := rows.Scan(&o.id, &o.deadline); err != nil {
			return 0, err
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	var failures []error
	for _, o := range orders {
		refs, err := r.ReserveCodedOrderInventory(ctx, redisClient, o.id, o.deadline)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		rebuilt += len(refs)
	}
	return rebuilt, errors.Join(failures...)
}
