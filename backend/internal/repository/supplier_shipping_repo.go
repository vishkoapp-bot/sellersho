package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var ErrShipmentState = errors.New("shipment is not ready for shipping")

// SubmitShipping updates only the caller's shipment, and advances the whole
// order only when every direct shipment is sent and there are no warehouse items.
func (r *SupplierShipmentRepository) SubmitShipping(ctx context.Context, supplierID, id, carrier, tracking, actor string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM supplier_shipments WHERE (id = $1 OR order_id = $1) AND supplier_id = $2 LIMIT 1`, id, supplierID).Scan(&orderID); err != nil {
		return err
	}
	var orderStatus, paymentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status, payment_status FROM orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&orderStatus, &paymentStatus); err != nil {
		return err
	}
	if paymentStatus != "paid" || orderStatus == "cancelled" {
		return ErrShipmentState
	}
	var shipmentID, status string
	if err := tx.QueryRowContext(ctx, `SELECT id, status FROM supplier_shipments WHERE (id = $1 OR order_id = $1) AND supplier_id = $2 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, id, supplierID).Scan(&shipmentID, &status); err != nil {
		return err
	}
	if status != "pending" && status != "processing" {
		return ErrShipmentState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_shipments SET status = 'shipped', carrier = $1, tracking_number = $2, shipped_at = NOW(), updated_at = NOW() WHERE id = $3 AND supplier_id = $4`, carrier, tracking, shipmentID, supplierID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_shipment_audit_logs (id, shipment_id, actor_user_id, action, timestamp) VALUES ($1, $2, $3, 'marked_shipped', NOW())`, uuid.NewString(), shipmentID, actor); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'shipped', tracking_code = $1, shipped_at = COALESCE(shipped_at, NOW()), updated_at = NOW() WHERE id = $2 AND fulfillment_method = 'supplier_direct' AND status NOT IN ('delivered','delivered_confirmed','cancelled') AND NOT EXISTS (SELECT 1 FROM supplier_shipments WHERE order_id = $2 AND status NOT IN ('shipped','in_transit','delivered'))`, tracking, orderID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SupplierShipmentRepository) SupplierItemCount(ctx context.Context, orderID, supplierID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(oi.quantity), 0) FROM order_items oi JOIN products p ON p.id = oi.product_id WHERE oi.order_id = $1 AND p.supplier_id = $2`, orderID, supplierID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count supplier items: %w", err)
	}
	return count, nil
}

func IsMissingShipment(err error) bool { return errors.Is(err, sql.ErrNoRows) }
