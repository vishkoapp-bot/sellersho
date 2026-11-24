package repository

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// Coded sellable capacity is bounded by both the physical lot and live
// units of the exact product/SKU. Do not expose or alter physical stock here.
const codedStockLots = ` FROM product_units u ` + codedStockJoins

const codedStockJoins = `
 JOIN product_variants v ON v.id=u.variant_id AND v.product_id=u.product_id AND v.is_active
 JOIN inventory_balances b ON b.id=u.balance_id AND b.product_id=u.product_id AND b.status='sellable' AND b.quantity>0
 LEFT JOIN warehouses w ON w.id=b.warehouse_id AND w.is_active
 LEFT JOIN supplier_warehouses sw ON sw.id=b.supplier_warehouse_id AND sw.supplier_id=p.supplier_id AND sw.is_active
 WHERE p.is_active AND u.product_id=p.id AND u.status='in_stock'
 AND (p.schema_version<2 OR v.inventory_mode='tracked')
 AND ((v.inventory_mode='tracked' AND b.variant_id=v.id) OR (v.inventory_mode<>'tracked' AND b.variant_id IS NULL))
 AND ((w.id IS NOT NULL AND b.supplier_warehouse_id IS NULL AND b.location_type IN ('warehouse','bin'))
 OR (sw.id IS NOT NULL AND b.warehouse_id IS NULL AND b.supplier_id=p.supplier_id AND b.location_type='supplier'))
`

// SQL equivalent of routing.ResolveItemRouting for catalog filtering. Direct
// must be enabled; if warehouse is also enabled, the product flag decides.
const codedStockRouted = `
 AND (sw.id IS NOT NULL) = EXISTS (
    SELECT 1 FROM suppliers supplier
    JOIN supplier_shipping_methods direct ON direct.owner_user_id=supplier.owner_user_id::text
      AND direct.method='direct' AND direct.status='enabled'
    WHERE supplier.id=p.supplier_id AND (
      p.fulfillment_method='supplier_direct' OR NOT EXISTS (
        SELECT 1 FROM supplier_shipping_methods warehouse
        WHERE warehouse.owner_user_id=direct.owner_user_id
          AND warehouse.method='warehouse' AND warehouse.status='enabled'
      )
    )
 )
`

func codedProductInStock(storeIDExpr string) string {
	visible := ""
	if storeIDExpr != "" {
		visible = " AND NOT EXISTS (SELECT 1 FROM store_variant_visibility svv WHERE svv.variant_id=v.id AND svv.store_id=" + storeIDExpr + " AND NOT svv.is_visible)"
	}
	return "EXISTS (SELECT 1 " + codedStockLots + codedStockRouted + visible + ")"
}

// CodedStockByVariant is a batched public availability projection, not a
// reservation. Quote and payment capture enforce the selected origin.
func (r *CatalogRepository) CodedStockByVariant(ctx context.Context, productIDs []string) (map[string]int, error) {
	return queryCodedStockByVariant(ctx, r.db, productIDs)
}

func (r *CatalogRepository) CodedStockByVariantTx(ctx context.Context, tx *sql.Tx, productIDs []string) (map[string]int, error) {
	return queryCodedStockByVariant(ctx, tx, productIDs)
}

// A set-based projection shared by the DTO, refresh batch and fingerprint.
// Aggregate actual unit rows by lot first so duplicate origin rows or an
// oversized set of units cannot inflate the lot's sellable capacity.
func codedVariantQuantitiesSQL(productFilter string) string {
	return `SELECT lot.product_id,lot.variant_id,SUM(lot.quantity) AS quantity FROM (
 SELECT p.id AS product_id,v.id::text AS variant_id,LEAST(b.quantity,count(*)) AS quantity
 FROM product_units u JOIN products p ON p.id=u.product_id` + codedStockJoins + codedStockRouted + productFilter + `
 GROUP BY p.id,v.id,b.id,b.quantity
 ) lot GROUP BY lot.product_id,lot.variant_id`
}

func queryCodedStockByVariant(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, productIDs []string) (map[string]int, error) {
	out := make(map[string]int)
	if len(productIDs) == 0 {
		return out, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT stock.variant_id,stock.quantity FROM (`+
		codedVariantQuantitiesSQL(" AND p.id=ANY($1::text[])")+`) stock`, pq.Array(productIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
