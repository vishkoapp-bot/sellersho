package repository

// Storage receipt cohorts and physical departure projection share the canonical
// inventory transaction. New receipts use storage_receipt_history.go and exact
// captured source/placement departures. These older FIFO helpers serve only
// unresolved pre-cutover positions; they never establish a billable receipt age.
// Paid capture and cancellation do not close a physically held receipt cohort.

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// storagePositionsExistTx reports whether the positions table is
// present in the database this transaction runs against.
func storagePositionsExistTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT to_regclass('warehouse_storage_positions') IS NOT NULL`).Scan(&exists)
	return exists, err
}

// creditStoragePositionTx preserves unattributed legacy custody as its own
// unresolved row. It never merges a top-up into another receipt's free window.
// New canonical QC arrivals use creditQCStorageReceiptTx in the same transaction.
func creditStoragePositionTx(ctx context.Context, tx *sql.Tx, productID, supplierID, warehouseID, inboundID string, qty int) error {
	if qty <= 0 || productID == "" || supplierID == "" || warehouseID == "" {
		return nil
	}
	exists, err := storagePositionsExistTx(ctx, tx)
	if err != nil || !exists {
		return err
	}
	// Pre-cutover/unattributed receipts remain separate unknown histories.
	// New canonical QC events use creditQCStorageReceiptTx instead. Never merge
	// a legacy fallback into a certified receipt position or inherit its age.
	_, err = tx.ExecContext(ctx, `INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,inbound_id,goods_class,qty,stocked_at,original_stocked_at) VALUES($1,$2,$3,$4,NULLIF($5,''),'normal',$6,NOW(),NOW())`, "WSP-"+uuid.NewString(), productID, supplierID, warehouseID, inboundID, qty)
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
	return decrementStoragePositionsWithCoverageTx(ctx, tx, productID, supplierID, warehouseID, qty, false)
}
func decrementStoragePositionsWithCoverageTx(ctx context.Context, tx *sql.Tx, productID, supplierID, warehouseID string, qty int, strict bool) error {

	if qty <= 0 || productID == "" {
		return nil
	}
	exists, err := storagePositionsExistTx(ctx, tx)
	if err != nil {
		return err
	}
	if !exists {
		if strict {
			return ErrInsufficientInventory
		}
		return nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, qty,stocked_at FROM warehouse_storage_positions
		WHERE product_id = $1 AND closed_at IS NULL AND qty > 0
		  AND ($2 = '' OR supplier_id = $2)
		  AND ($3 = '' OR warehouse_id = $3)
		ORDER BY id COLLATE "C"
		FOR UPDATE`, productID, supplierID, warehouseID)
	if err != nil {
		return err
	}
	type openPosition struct {
		id      string
		qty     int
		stocked time.Time
	}
	positions := []openPosition{}
	for rows.Next() {
		var p openPosition
		if err := rows.Scan(&p.id, &p.qty, &p.stocked); err != nil {
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
	total := 0
	for _, p := range positions {
		if p.qty > 0 && total < qty {
			take := p.qty
			if take > qty-total {
				take = qty - total
			}
			total += take
		}
	}
	if strict && total < qty {
		return fmt.Errorf("%w: named-origin storage positions are incomplete", ErrInsufficientInventory)
	}
	sort.Slice(positions, func(i, j int) bool {
		if positions[i].stocked.Equal(positions[j].stocked) {
			return positions[i].id < positions[j].id
		}
		return positions[i].stocked.Before(positions[j].stocked)
	})
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

// consumeShippedOrderPositionsTx consumes every persisted warehouse split.
// Historical missing origins/positions require explicit reconciliation; they can
// never debit another warehouse or silently leave a split's storage outstanding.
func consumeShippedOrderPositionsTx(ctx context.Context, tx *sql.Tx, orderID string, actors ...string) error {
	rows, err := tx.QueryContext(ctx, `SELECT oi.product_id,COALESCE(x.quantity,0),COALESCE(x.supplier_id,p.supplier_id,''),COALESCE(x.warehouse_id,''),COALESCE(x.origin_type,'legacy'),oi.quantity,COALESCE(SUM(x.quantity) OVER(PARTITION BY oi.id),0)
 FROM order_items oi JOIN products p ON p.id=oi.product_id LEFT JOIN order_item_origins x ON x.order_item_id::text=oi.id::text
 WHERE oi.order_id=$1 ORDER BY oi.id,x.leg_index`, orderID)
	if err != nil {
		return err
	}
	type split struct {
		product, supplier, warehouse, kind string
		qty, lineQty, total                int
	}
	var splits []split
	for rows.Next() {
		var s split
		if err := rows.Scan(&s.product, &s.qty, &s.supplier, &s.warehouse, &s.kind, &s.lineQty, &s.total); err != nil {
			rows.Close()
			return err
		}
		splits = append(splits, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	warehouseSplits := 0
	for _, s := range splits {
		if s.qty <= 0 || s.total != s.lineQty {
			return fmt.Errorf("%w: fulfillment origin reconciliation required", ErrInsufficientInventory)
		}
		if s.kind == "supplier" {
			continue
		}
		if s.kind != "warehouse" || s.warehouse == "" || s.supplier == "" {
			return fmt.Errorf("%w: unknown shipment origin", ErrInsufficientInventory)
		}
		warehouseSplits++
	}
	if warehouseSplits == 0 {
		return nil
	}
	var knownReceipts bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots l JOIN order_items oi ON oi.product_id=l.product_id WHERE oi.order_id=$1)`, orderID).Scan(&knownReceipts); err != nil {
		return err
	}
	if knownReceipts {
		actor := "warehouse-dispatch"
		if len(actors) > 0 {
			actor = actors[0]
		}
		return consumeStorageBusinessEventTx(ctx, tx, "warehouse_shipment", orderID, actor)
	}
	exists, err := storagePositionsExistTx(ctx, tx)
	if err != nil {
		return err
	}
	if !exists {
		return ErrInsufficientInventory
	}
	// Acquire all position rows in canonical ID order before choosing FIFO within
	// each origin. The caller holds order/account/product/SKU guards.
	guards, err := tx.QueryContext(ctx, `SELECT p.id FROM warehouse_storage_positions p WHERE p.closed_at IS NULL AND EXISTS(SELECT 1 FROM order_items oi JOIN order_item_origins x ON x.order_item_id::text=oi.id::text WHERE oi.order_id=$1 AND x.origin_type='warehouse' AND oi.product_id=p.product_id AND x.warehouse_id=p.warehouse_id) ORDER BY p.id COLLATE "C" FOR UPDATE`, orderID)
	if err != nil {
		return err
	}
	for guards.Next() {
		var id string
		if err := guards.Scan(&id); err != nil {
			guards.Close()
			return err
		}
	}
	err = guards.Err()
	guards.Close()
	if err != nil {
		return err
	}
	for _, s := range splits {
		if s.kind == "warehouse" {
			if err := decrementStoragePositionsWithCoverageTx(ctx, tx, s.product, s.supplier, s.warehouse, s.qty, true); err != nil {
				return err
			}
		}
	}
	return nil
}
