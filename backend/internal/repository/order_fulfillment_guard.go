package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrOrderPartiallyDispatched = errors.New("order has a dispatched fulfillment leg and cannot be cancelled")
var ErrOrderCancellationState = errors.New("order cannot be cancelled in its current state")

// The order row is the common serialization guard for cancellation and every
// physical dispatch writer. Acquire it before any shipment, product or capture.
func lockFulfillmentOrderTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return LockTransactionRowsTx(bounded, tx, &TransactionLockSet{}, TransactionRowLock{Table: "orders", ID: orderID})
}

// Caller holds the order guard. Never infer physical custody from aggregate
// status alone: carrier failures and legacy corrections can obscure dispatch.
func assertOrderCancellationAllowedTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	var dispatched bool
	err := tx.QueryRowContext(ctx, `SELECT shipped_at IS NOT NULL OR status IN ('shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed') OR EXISTS (
 SELECT 1 FROM supplier_shipments WHERE order_id=$1 AND (shipped_at IS NOT NULL OR delivered_at IS NOT NULL OR status IN ('shipped','in_transit','delivered'))
 ) FROM orders WHERE id=$1`, orderID).Scan(&dispatched)
	if err != nil {
		return err
	}
	if dispatched {
		return ErrOrderPartiallyDispatched
	}
	return nil
}

func assertOrderDispatchAllowedTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	if err := lockFulfillmentOrderTx(ctx, tx, orderID); err != nil {
		return err
	}
	var status, payment string
	if err := tx.QueryRowContext(ctx, `SELECT status,payment_status FROM orders WHERE id=$1`, orderID).Scan(&status, &payment); err != nil {
		return err
	}
	if payment != "paid" || status == "cancelled" {
		return ErrShipmentState
	}
	return nil
}

// Discover the complete restoration plan without taking child locks. The order
// guard stabilizes captures against the adopted dispatch/cancel flows.
func lockCancellationInventoryTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	var plan []TransactionRowLock
	for _, query := range []struct{ table, sql string }{
		{"products", `SELECT DISTINCT product_id FROM order_items WHERE order_id=$1`},
		{"product_variants", `SELECT DISTINCT variant_id::text FROM order_items WHERE order_id=$1 AND variant_id IS NOT NULL`},
		{"supplier_shipments", `SELECT id FROM supplier_shipments WHERE order_id=$1`},
	} {
		rows, err := tx.QueryContext(ctx, query.sql, orderID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			plan = append(plan, TransactionRowLock{Table: query.table, ID: id})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	enabled, err := hasSKUInventoryTx(ctx, tx)
	if err != nil {
		return err
	}
	if enabled {
		rows, err := tx.QueryContext(ctx, `SELECT order_item_id,allocations FROM order_inventory_captures WHERE order_id=$1`, orderID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var raw []byte
			if err := rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return err
			}
			plan = append(plan, TransactionRowLock{Table: "order_inventory_captures", ID: id})
			allocations, err := decodeCancellationAllocations(raw)
			if err != nil {
				rows.Close()
				return err
			}
			for _, a := range allocations {
				plan = append(plan, TransactionRowLock{Table: "inventory_balances", ID: a.BalanceID})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return LockTransactionRowsTx(bounded, tx, &TransactionLockSet{}, plan...)
}

func decodeCancellationAllocations(raw []byte) ([]skuAllocation, error) {
	var a []skuAllocation
	err := json.Unmarshal(raw, &a)
	return a, err
}

// Prehold products before storage rows; FIFO chooses consumption only after
// canonical row acquisition. This avoids deadlock with capture/return writers.
func lockWarehouseDispatchInventoryTx(ctx context.Context, tx *sql.Tx, orderID string) error {
	var plan []TransactionRowLock
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT product_id FROM order_items WHERE order_id=$1`, orderID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		plan = append(plan, TransactionRowLock{Table: "products", ID: id})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	exists, err := storagePositionsExistTx(ctx, tx)
	if err != nil {
		return err
	}
	if exists {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM warehouse_storage_positions WHERE closed_at IS NULL AND qty>0 AND product_id IN (SELECT product_id FROM order_items WHERE order_id=$1)`, orderID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			plan = append(plan, TransactionRowLock{Table: "warehouse_storage_positions", ID: id})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return LockTransactionRowsTx(bounded, tx, &TransactionLockSet{}, plan...)
}
