package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/lib/pq"
)

type inventoryHoldRequest struct {
	ref      InventoryHoldRef
	quantity int
}

// codedOrderHoldRequests keeps the established SKU hold for callback and
// adjustment compatibility, and adds one hold per persisted origin of that
// SKU. The SKU hold alone cannot protect two orders at the same origin when
// another origin still has coded stock.
func (r *OrderRepository) codedOrderHoldRequests(ctx context.Context, orderID string) ([]inventoryHoldRequest, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT oi.id, oi.product_id, COALESCE(oi.variant_id::text,''), oi.quantity,
		       COALESCE(o.origin_type,''), COALESCE(o.warehouse_id,''), COALESCE(o.supplier_warehouse_id,''),
		       COALESCE(o.supplier_id,''), COALESCE(p.supplier_id,''), COALESCE(o.quantity,0)
		FROM order_items oi
		LEFT JOIN order_item_origins o ON o.order_item_id=oi.id AND o.order_id=oi.order_id
		LEFT JOIN products p ON p.id=oi.product_id
		WHERE oi.order_id=$1
		ORDER BY oi.id,o.id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[InventoryHoldRef]int{}
	var previousItem string
	var remaining int
	for rows.Next() {
		var item, kind, supplier, owner string
		var quantity, originQty int
		var ref InventoryHoldRef
		if err := rows.Scan(&item, &ref.ProductID, &ref.VariantID, &quantity, &kind, &ref.WarehouseID, &ref.SupplierWarehouseID, &supplier, &owner, &originQty); err != nil {
			return nil, err
		}
		if item != previousItem {
			if remaining != 0 {
				return nil, ErrInsufficientInventory
			}
			remaining = quantity
			previousItem = item
		}
		if ref.VariantID == "" || originQty <= 0 || originQty > remaining {
			return nil, ErrInsufficientInventory
		}
		switch kind {
		case "warehouse":
			if ref.WarehouseID == "" || ref.SupplierWarehouseID != "" || (supplier != "" && supplier != owner) {
				return nil, ErrInsufficientInventory
			}
		case "supplier":
			if ref.WarehouseID != "" || ref.SupplierWarehouseID == "" || supplier == "" || supplier != owner {
				return nil, ErrInsufficientInventory
			}
		default:
			return nil, ErrInsufficientInventory
		}
		remaining -= originQty
		for _, key := range []InventoryHoldRef{ref, {ProductID: ref.ProductID, VariantID: ref.VariantID}} {
			if totals[key] > int(^uint(0)>>1)-originQty {
				return nil, ErrInsufficientInventory
			}
			totals[key] += originQty
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if remaining != 0 || len(totals) == 0 {
		return nil, ErrInsufficientInventory
	}
	requests := make([]inventoryHoldRequest, 0, len(totals))
	for ref, quantity := range totals {
		requests = append(requests, inventoryHoldRequest{ref: ref, quantity: quantity})
	}
	sort.Slice(requests, func(i, j int) bool {
		a, b := requests[i].ref, requests[j].ref
		// Claim scoped pools before aggregate pools; a failed origin claim
		// cannot contaminate an unrelated origin's aggregate admission.
		if a.scoped() != b.scoped() {
			return a.scoped()
		}
		return holdPool(a) < holdPool(b)
	})
	return requests, nil
}

func codedStockForReservation(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, ref InventoryHoldRef) (int, error) {
	var total int
	err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(c.quantity),0)
 FROM products p CROSS JOIN LATERAL (
 SELECT LEAST(b.quantity,count(*)) quantity`+codedStockLots+`
 AND v.id=$2 AND ($3='' OR w.id=$3) AND ($4='' OR sw.id=$4)
 GROUP BY b.id,b.quantity
 ) c WHERE p.id=$1`, ref.ProductID, ref.VariantID, ref.WarehouseID, ref.SupplierWarehouseID).Scan(&total)
	return total, err
}

// ReserveCodedOrderInventory is shared by payment admission and Redis recovery.
// PostgreSQL still serializes and revalidates exact units at capture.
func (r *OrderRepository) ReserveCodedOrderInventory(ctx context.Context, redis *RedisClient, orderID string, deadline time.Time) ([]InventoryHoldRef, error) {
	requests, err := r.codedOrderHoldRequests(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if maximum := time.Now().Add(15 * time.Minute); maximum.Before(deadline) {
		deadline = maximum
	}
	if !time.Now().Before(deadline) {
		return nil, ErrPaymentExpired
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Inventory writers take exclusive product guards. Keep shared guards
	// from capacity read through the Redis claim: a retirement either sees
	// the new hold before committing or finishes before we count capacity.
	products := make([]string, 0, len(requests))
	seen := map[string]bool{}
	for _, req := range requests {
		id := req.ref.ProductID
		if !seen[id] {
			products = append(products, id)
			seen[id] = true
		}
	}
	guards, err := tx.QueryContext(ctx, `SELECT id FROM products WHERE id=ANY($1::text[]) ORDER BY id COLLATE "C" FOR SHARE`, pq.Array(products))
	if err != nil {
		return nil, err
	}
	for guards.Next() {
		var id string
		if err := guards.Scan(&id); err != nil {
			guards.Close()
			return nil, err
		}
	}
	err = guards.Err()
	guards.Close()
	if err != nil {
		return nil, err
	}

	bounds := make([]int, len(requests))
	for i, req := range requests {
		bounds[i], err = codedStockForReservation(ctx, tx, req.ref)
		if err != nil {
			return nil, err
		}
		if bounds[i] < req.quantity {
			return nil, fmt.Errorf("%w: موجودی محصول %s برای پرداخت کافی نیست", ErrInsufficientInventory, req.ref.ProductID)
		}
	}
	reserved := make([]InventoryHoldRef, 0, len(requests))
	release := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cleanupCancel()
		_ = redis.ReleaseOrderInventory(cleanupCtx, orderID, reserved...)
	}
	for i, req := range requests {
		if err := redis.holdInventoryRefUntil(ctx, orderID, req.ref, req.quantity, bounds[i], deadline); err != nil {
			release()
			if errors.Is(err, ErrInsufficientInventory) {
				return nil, fmt.Errorf("%w: موجودی محصول %s برای پرداخت کافی نیست", err, req.ref.ProductID)
			}
			return nil, err
		}
		reserved = append(reserved, req.ref)
	}
	if err := tx.Commit(); err != nil {
		release()
		return nil, err
	}
	return reserved, nil
}

// ReleaseCodedOrderOriginHolds releases scoped pools after the established
// callback has finished processing the generic SKU holds.
func (r *OrderRepository) ReleaseCodedOrderOriginHolds(ctx context.Context, redis *RedisClient, orderID string) error {
	requests, err := r.codedOrderHoldRequests(ctx, orderID)
	if err != nil {
		return err
	}
	var refs []InventoryHoldRef
	for _, req := range requests {
		if req.ref.scoped() {
			refs = append(refs, req.ref)
		}
	}
	return redis.ReleaseOrderInventory(ctx, orderID, refs...)
}
