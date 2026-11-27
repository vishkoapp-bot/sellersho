package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrShipmentState = errors.New("shipment is not ready for shipping")

var ErrShipmentNotPending = errors.New("shipment is not pending")
var ErrTrackingNumberInUse = errors.New("tracking number is already used by another shipment")

// SubmitShipping updates only the caller's shipment, and advances the whole
// order only when every direct shipment is sent and there are no warehouse items.
func (r *SupplierShipmentRepository) SubmitShipping(ctx context.Context, supplierID, id, carrier, tracking, actor string) error {
	return r.submitShipping(ctx, supplierID, id, carrier, tracking, actor, nil)
}

type TapinRegistration struct {
	Service string
	BoxID   string
	Status  int
}

func (r *SupplierShipmentRepository) SubmitTapinShipping(ctx context.Context, supplierID, shipmentID, tracking, actor string, registration TapinRegistration) error {
	return r.submitShipping(ctx, supplierID, shipmentID, "tapin-"+registration.Service, tracking, actor, &registration)
}

func (r *SupplierShipmentRepository) submitShipping(ctx context.Context, supplierID, id, carrier, tracking, actor string, tapinRegistration *TapinRegistration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var orderID string
	if err := tx.QueryRowContext(ctx, `SELECT order_id FROM supplier_shipments WHERE (id = $1 OR order_id = $1) AND supplier_id = $2 LIMIT 1`, id, supplierID).Scan(&orderID); err != nil {
		return err
	}
	if err := assertOrderDispatchAllowedTx(ctx, tx, orderID); err != nil {
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
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.Constraint == "idx_supplier_shipments_tracking" {
			return ErrTrackingNumberInUse
		}
		return err
	}
	if tapinRegistration != nil {
		if err := recordTapinRegistration(ctx, tx, shipmentID, tapinRegistration.Service, tapinRegistration.BoxID, tapinRegistration.Status); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_shipment_audit_logs (id, shipment_id, actor_user_id, action, timestamp) VALUES ($1, $2, $3, 'marked_shipped', NOW())`, uuid.NewString(), shipmentID, actor); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status = 'shipped', tracking_code = $1, shipped_at = COALESCE(shipped_at, NOW()), updated_at = NOW() WHERE id = $2 AND fulfillment_method = 'supplier_direct' AND status NOT IN ('delivered','delivered_confirmed','cancelled') AND NOT EXISTS (SELECT 1 FROM supplier_shipments WHERE order_id = $2 AND status NOT IN ('shipped','in_transit','delivered'))`, tracking, orderID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// P0-4 (best-effort): direct-ship — the customer is told the order
	// shipped, with tracking, like the warehouse ShipOrder row. Only
	// when the order actually flipped to shipped.
	if o, oerr := loadOrderNotifyCtx(ctx, r.db, orderID); oerr == nil && o.Status == "shipped" && o.CustomerID != "" {
		notifyBestEffort(ctx, r.db, o.CustomerID, NotifKindOrderStatus,
			fmt.Sprintf("سفارش %s ارسال شد", o.OrderNumber),
			fmt.Sprintf("سفارش %s ارسال شد و در راه است. کد رهگیری: %s", o.OrderNumber, tracking),
			o.payload("shipped"), "direct_ship_customer")
	}
	return nil
}

// AcceptShipment moves the caller's own direct shipment from 'pending' to
// 'processing': an explicit "I take this order" step in the supplier panel.
// It is NOT a gate for SubmitShipping, which keeps accepting both 'pending'
// and 'processing' exactly as before, so the ship flow is unchanged.
func (r *SupplierShipmentRepository) AcceptShipment(ctx context.Context, supplierID, id, actor string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var shipmentID, orderID string
	if err := tx.QueryRowContext(ctx, `SELECT id, order_id FROM supplier_shipments WHERE (id = $1 OR order_id = $1) AND supplier_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1`, id, supplierID).Scan(&shipmentID, &orderID); err != nil {
		return err
	}
	// Cancellation and dispatch acquire the order guard before any shipment.
	// Check payment and cancellation while holding that same guard.
	if err := lockFulfillmentOrderTx(ctx, tx, orderID); err != nil {
		return err
	}
	var orderStatus, paymentStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status, payment_status FROM orders WHERE id = $1`, orderID).Scan(&orderStatus, &paymentStatus); err != nil {
		return err
	}
	if paymentStatus != "paid" || orderStatus == "cancelled" {
		return ErrShipmentState
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM supplier_shipments WHERE id = $1 AND supplier_id = $2 FOR UPDATE`, shipmentID, supplierID).Scan(&status); err != nil {
		return err
	}
	if status != "pending" {
		return ErrShipmentNotPending
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_shipments SET status = 'processing', updated_at = NOW() WHERE id = $1 AND supplier_id = $2`, shipmentID, supplierID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_shipment_audit_logs (id, shipment_id, actor_user_id, action, timestamp) VALUES ($1, $2, $3, 'accepted', NOW())`, uuid.NewString(), shipmentID, actor); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// P1-5 (best-effort): supplier accepted the order - customer.
	notifySupplierAccepted(ctx, r.db, orderID, "")
	return nil
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
