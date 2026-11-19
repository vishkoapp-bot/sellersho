package repository

import (
	"context"
	"database/sql"
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
		SELECT o.id, o.payment_expires_at, oi.product_id, COALESCE(oi.variant_id::text,''), oi.quantity
		FROM orders o JOIN order_items oi ON oi.order_id = o.id
		WHERE o.payment_status = 'pending' AND o.status IN ('pending','processing')
		  AND NOT o.inventory_captured AND o.payment_expires_at > NOW()
		ORDER BY o.id, oi.product_id, oi.variant_id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type holdGroup struct {
		orderID  string
		deadline time.Time
		ref      InventoryHoldRef
		quantity int
	}
	var groups []*holdGroup
	index := make(map[string]*holdGroup)
	for rows.Next() {
		var g holdGroup
		var variant string
		if err := rows.Scan(&g.orderID, &g.deadline, &g.ref.ProductID, &variant, &g.quantity); err != nil {
			rows.Close()
			return rebuilt, err
		}
		g.ref.VariantID = variant
		key := g.orderID + "\x00" + g.ref.ProductID + "\x00" + variant
		if existing, ok := index[key]; ok {
			existing.quantity += g.quantity
			continue
		}
		cp := g
		index[key] = &cp
		groups = append(groups, &cp)
	}
	if err := rows.Err(); err != nil {
		return rebuilt, err
	}

	var failures []error
	for _, g := range groups {
		stock, stockErr := r.holdAvailableStock(ctx, g.ref)
		if stockErr != nil {
			failures = append(failures, stockErr)
			continue
		}
		deadline := g.deadline
		if maximum := time.Now().Add(15 * time.Minute); maximum.Before(deadline) {
			deadline = maximum
		}
		if err := redisClient.HoldInventoryUntil(ctx, g.orderID, g.ref.ProductID, g.ref.VariantID, g.quantity, stock, deadline); err != nil {
			failures = append(failures, err)
			continue
		}
		rebuilt++
	}
	return rebuilt, errors.Join(failures...)
}

// holdAvailableStock resolves the stock bound for one hold pool with the
// same semantics as the payment-time hold (PERF-04 D-06): a tracked
// variant's own stock binds; legacy variants and variant-less lines use
// the product stock. A missing product or variant fails the rebuild of
// that pool instead of guessing a bound.
func (r *OrderRepository) holdAvailableStock(ctx context.Context, ref InventoryHoldRef) (int, error) {
	var stock int
	if err := r.db.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, ref.ProductID).Scan(&stock); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInsufficientInventory
		}
		return 0, err
	}
	if ref.VariantID == "" {
		return stock, nil
	}
	var variantStock int
	var mode string
	var active bool
	if err := r.db.QueryRowContext(ctx, `SELECT stock, COALESCE(inventory_mode,'legacy'), COALESCE(is_active, TRUE) FROM product_variants WHERE id=$1 AND product_id=$2`, ref.VariantID, ref.ProductID).Scan(&variantStock, &mode, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInsufficientInventory
		}
		return 0, err
	}
	if mode == "tracked" {
		if !active {
			return 0, ErrInsufficientInventory
		}
		return variantStock, nil
	}
	return stock, nil
}
