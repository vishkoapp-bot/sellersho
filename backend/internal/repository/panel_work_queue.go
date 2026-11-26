package repository

import "context"

const supplierOrderReadySQL = "s.status IN ('pending','processing') AND s.shipped_at IS NULL AND o.payment_status = 'paid' AND o.status <> 'cancelled'"
const sellerOrderFollowUpSQL = "payment_status = 'paid' AND status IN ('delivery_disputed','delivered_unconfirmed')"

// Previous shipments keep the existing SLA calculation as an estimate. New
// shipments carry the commitment captured when the fulfillment leg was made.
func supplierDispatchDeadlineSQL(alias string) string {
	return `COALESCE(` + alias + `.dispatch_due_at, ` + alias + `.created_at + make_interval(days =>
		GREATEST(1, LEAST(30, COALESCE((SELECT MAX(p.preparation_days) FROM order_items oi
		JOIN products p ON p.id=oi.product_id WHERE oi.order_id=` + alias + `.order_id AND p.supplier_id=` + alias + `.supplier_id), 1)))))`
}

type SupplierWorkCounts struct {
	Ready         int `json:"ready"`
	Overdue       int `json:"overdue"`
	OpenCartons   int `json:"openCartons"`
	Discrepancies int `json:"discrepancies"`
}

func (r *SupplierRepository) GetSupplierWorkCounts(ctx context.Context, supplierID string) (SupplierWorkCounts, error) {
	var out SupplierWorkCounts
	err := r.db.QueryRowContext(ctx, `SELECT
		COUNT(*), COUNT(*) FILTER (WHERE `+supplierDispatchDeadlineSQL("s")+` < now()),
		(SELECT COUNT(*) FROM shipments s`+cartonStatusReadJoin+` WHERE s.supplier_id=$1
			AND carton_state.status IN ('draft','created','sent','in_transit','partially_received')),
		(SELECT COUNT(*) FROM qc_discrepancies WHERE supplier_id=$1 AND status='open' AND responded_at IS NULL AND escalated_at IS NULL AND response_due_at > now())
		FROM supplier_shipments s JOIN orders o ON o.id=s.order_id WHERE s.supplier_id=$1 AND `+supplierOrderReadySQL, supplierID).
		Scan(&out.Ready, &out.Overdue, &out.OpenCartons, &out.Discrepancies)
	return out, err
}

func (r *OrderRepository) CountStoreOrderFollowUp(ctx context.Context, storeID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM orders WHERE store_id=$1 AND "+sellerOrderFollowUpSQL, storeID).Scan(&count)
	return count, err
}
