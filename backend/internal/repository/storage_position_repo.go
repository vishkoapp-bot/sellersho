package repository

// Storage-position lifecycle (gap 7 follow-up). The storage-fee
// engine (storage_fee_repo.go) assesses warehouse_storage_positions
// (migration 202610280005), but positions are a ledger this file's
// helpers maintain alongside the inventory_balances movements they
// accompany:
//
//   - CREATION: when an inbound QC pass mints sellable stock at a
//     warehouse (RecordQCPass / CompleteQC in warehouse_repo.go —
//     the physical -> sellable moveInventoryTx into location
//     'warehouse:{inboundID}'), the passed quantity is credited to
//     the open position for (product, supplier, warehouse), or a
//     new position is opened. stocked_at / original_stocked_at are
//     set ONLY on insert: a top-up never resets the free-window
//     clock (the engine measures from original_stocked_at).
//   - CONSUMPTION: when warehouse stock physically leaves — an
//     order's ShipOrder handover, or a supplier stock-return's
//     HandoverReturn — open positions are decremented oldest-first
//     (FIFO by stocked_at), splitting across positions; a position
//     that reaches zero is closed (closed_at = NOW()).
//
// Positions are the FEE ledger only: inventory_balances remain the
// stock truth. Consumption therefore never fails on a shortfall —
// legacy stock predates positions, so positions may under-record;
// the decrement takes what is there and stops at zero. Both helpers
// no-op when the positions table does not exist yet (un-migrated
// database), mirroring the existence-guard posture the balance SQL
// uses for the fee tables, so the warehouse flows behave exactly as
// before on such databases.

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// storagePositionsExistTx reports whether the positions table is
// present in the database this transaction runs against.
func storagePositionsExistTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT to_regclass('warehouse_storage_positions') IS NOT NULL`).Scan(&exists)
	return exists, err
}

// creditStoragePositionTx adds qty to the open storage position for
// (productID, supplierID, warehouseID), opening a new position when
// none is open. The caller must run it in the same transaction as
// the inventory movement that made the stock sellable, and only
// when that movement was genuinely new (see
// qcSellableAlreadyCreditedTx) so a replayed QC pass cannot
// double-count. A position's inbound_id records the inbound that
// OPENED it; top-ups from later inbounds leave it (and both
// timestamps) untouched. warehouseID == "" (legacy inbound with no
// recorded destination warehouse) skips the credit: positions carry
// a NOT NULL warehouse FK, and an unattributed position would bill
// the wrong warehouse's storage.
func creditStoragePositionTx(ctx context.Context, tx *sql.Tx, productID, supplierID, warehouseID, inboundID string, qty int) error {
	if qty <= 0 || productID == "" || supplierID == "" || warehouseID == "" {
		return nil
	}
	exists, err := storagePositionsExistTx(ctx, tx)
	if err != nil || !exists {
		return err
	}
	var positionID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM warehouse_storage_positions
		WHERE product_id = $1 AND supplier_id = $2 AND warehouse_id = $3 AND closed_at IS NULL
		ORDER BY stocked_at ASC, id ASC
		LIMIT 1 FOR UPDATE`, productID, supplierID, warehouseID).Scan(&positionID)
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO warehouse_storage_positions
				(id, product_id, supplier_id, warehouse_id, inbound_id, goods_class, qty, stocked_at, original_stocked_at)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), 'normal', $6, NOW(), NOW())`,
			"WSP-"+uuid.NewString(), productID, supplierID, warehouseID, inboundID, qty)
		return err
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE warehouse_storage_positions
		SET qty = qty + $2, updated_at = NOW()
		WHERE id = $1`, positionID, qty)
	return err
}

// decrementStoragePositionsFIFO removes qty from the open positions
// of a product, oldest stocked first, splitting across positions and
// closing any position that reaches zero. supplierID / warehouseID
// narrow the FIFO when known ("" = no narrowing — used when the
// outflow's warehouse is not recorded, e.g. a supplier stock-return
// whose reservations were drawn from bin lots that carry no
// warehouse id). It never drives a position negative and never
// errors on a shortfall: positions under-record by design (legacy
// stock has no positions), so the decrement simply stops at zero.
func decrementStoragePositionsFIFO(ctx context.Context, tx *sql.Tx, productID, supplierID, warehouseID string, qty int) error {
	if qty <= 0 || productID == "" {
		return nil
	}
	exists, err := storagePositionsExistTx(ctx, tx)
	if err != nil || !exists {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, qty FROM warehouse_storage_positions
		WHERE product_id = $1 AND closed_at IS NULL AND qty > 0
		  AND ($2 = '' OR supplier_id = $2)
		  AND ($3 = '' OR warehouse_id = $3)
		ORDER BY stocked_at ASC, id ASC
		FOR UPDATE`, productID, supplierID, warehouseID)
	if err != nil {
		return err
	}
	type openPosition struct {
		id  string
		qty int
	}
	positions := []openPosition{}
	for rows.Next() {
		var p openPosition
		if err := rows.Scan(&p.id, &p.qty); err != nil {
			rows.Close()
			return err
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	remaining := qty
	for _, p := range positions {
		if remaining <= 0 {
			break
		}
		take := p.qty
		if take > remaining {
			take = remaining
		}
		if take == p.qty {
			if _, err := tx.ExecContext(ctx, `
				UPDATE warehouse_storage_positions
				SET qty = 0, closed_at = NOW(), updated_at = NOW()
				WHERE id = $1`, p.id); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `
				UPDATE warehouse_storage_positions
				SET qty = qty - $2, updated_at = NOW()
				WHERE id = $1`, p.id, take); err != nil {
				return err
			}
		}
		remaining -= take
	}
	return nil
}

// qcSellableAlreadyCreditedTx reports whether a QC pass has already
// minted sellable stock for this product at this inbound's warehouse
// location (a 'qc_pass' movement into 'warehouse:{inboundID}').
// RecordQCPass and CompleteQC are two endpoints for the same QC
// action and guard their replays differently (qc_results state vs
// the movement's idempotency key), so the position credit keys off
// the movement itself: the credit runs only when this transaction
// is the one that first creates the qc_pass movement. The caller
// checks BEFORE its moveInventoryTx; the inbound row lock both QC
// functions hold serializes competing passes on the same inbound.
func qcSellableAlreadyCreditedTx(ctx context.Context, tx *sql.Tx, productID, inboundID string) (bool, error) {
	var credited bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM inventory_movements
			WHERE product_id = $1 AND reference_type = 'qc_pass'
			  AND to_status = 'sellable' AND to_location_key = $2)`,
		productID, "warehouse:"+inboundID).Scan(&credited)
	return credited, err
}

// consumeShippedOrderPositionsTx decrements storage positions for
// the warehouse-routed items of an order that has just shipped, in
// ShipOrder's transaction (ShipOrder's packed -> shipped status
// guard makes the whole transaction run exactly once per order).
// Per-item routing uses the SAME persisted-origin rule as the
// warehouse pick queue (getOrdersByStatuses): the lowest-leg_index
// order_item_origins row decides, origin-less items fall back to
// the product's fulfillment_method flag. The origin's warehouse_id
// narrows the FIFO when recorded. Supplier-direct items never sat
// in a warehouse position and are skipped.
func consumeShippedOrderPositionsTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT oi.product_id, oi.quantity, COALESCE(p.supplier_id, ''), COALESCE(oio.warehouse_id, ''),
		       CASE
		           WHEN oio.origin_type = 'supplier' THEN 'supplier_direct'
		           WHEN oio.origin_type = 'warehouse' THEN 'warehouse'
		           ELSE COALESCE(p.fulfillment_method, 'warehouse')
		       END
		FROM order_items oi
		JOIN products p ON p.id = oi.product_id
		LEFT JOIN LATERAL (
			SELECT origin_type, warehouse_id FROM order_item_origins
			WHERE order_item_id = oi.id
			ORDER BY leg_index ASC
			LIMIT 1
		) oio ON true
		WHERE oi.order_id = $1`, orderID)
	if err != nil {
		return err
	}
	type shippedItem struct {
		productID   string
		quantity    int
		supplierID  string
		warehouseID string
		routing     string
	}
	items := []shippedItem{}
	for rows.Next() {
		var it shippedItem
		if err := rows.Scan(&it.productID, &it.quantity, &it.supplierID, &it.warehouseID, &it.routing); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, it := range items {
		if it.routing != "warehouse" || it.quantity <= 0 {
			continue
		}
		if err := decrementStoragePositionsFIFO(ctx, tx, it.productID, it.supplierID, it.warehouseID, it.quantity); err != nil {
			return err
		}
	}
	return nil
}
