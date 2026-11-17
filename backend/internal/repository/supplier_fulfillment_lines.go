package repository

import "context"

// SupplierFulfillmentLine exposes only the caller's shipping lines. Names and
// SPNC are the purchased snapshots, not today's catalog labels or retail prices.
type SupplierFulfillmentLine struct {
	OrderItemID  string `json:"orderItemId"`
	ProductID    string `json:"productId"`
	ProductTitle string `json:"productTitle"`
	VariantID    string `json:"variantId,omitempty"`
	VariantName  string `json:"variantName,omitempty"`
	VariantSPNC  string `json:"variantSpnc,omitempty"`
	Quantity     int    `json:"quantity"`
}

func (r *SupplierShipmentRepository) ListSupplierFulfillmentLines(ctx context.Context, supplierID, orderID string) ([]SupplierFulfillmentLine, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT oi.id::text,oi.product_id,p.title,COALESCE(oi.variant_id::text,''),COALESCE(oi.variant_name,''),COALESCE(oi.variant_spnc,''),COALESCE((SELECT SUM(x.quantity) FROM order_item_origins x WHERE x.order_item_id=oi.id AND x.origin_type='supplier' AND x.supplier_id=$2),oi.quantity)
	 FROM order_items oi JOIN products p ON p.id=oi.product_id
	 WHERE oi.order_id=$1 AND p.supplier_id=$2
	 AND (EXISTS(SELECT 1 FROM order_item_origins x WHERE x.order_item_id=oi.id AND x.origin_type='supplier' AND x.supplier_id=$2) OR (NOT EXISTS(SELECT 1 FROM order_item_origins x WHERE x.order_item_id=oi.id) AND p.fulfillment_method='supplier_direct'))
	 ORDER BY oi.id`, orderID, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierFulfillmentLine{}
	for rows.Next() {
		var l SupplierFulfillmentLine
		if err := rows.Scan(&l.OrderItemID, &l.ProductID, &l.ProductTitle, &l.VariantID, &l.VariantName, &l.VariantSPNC, &l.Quantity); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
