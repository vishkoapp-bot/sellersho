package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// inventoryOrigin is an immutable checkout split. Labels and current product
// routing never authorize consuming a physical lot.
type inventoryOrigin struct {
	ID, Kind, SupplierID, WarehouseID, SupplierWarehouseID string
	Quantity                                               int
}

func consumeInventoryForOriginTx(ctx context.Context, tx *sql.Tx, it inventoryCaptureItem, mode string) ([]skuAllocation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,origin_type,COALESCE(supplier_id,''),COALESCE(warehouse_id,''),COALESCE(supplier_warehouse_id,''),quantity FROM order_item_origins WHERE order_item_id=$1 ORDER BY leg_index,id`, it.ID)
	if err != nil {
		return nil, err
	}
	var origins []inventoryOrigin
	total := 0
	for rows.Next() {
		var o inventoryOrigin
		if err := rows.Scan(&o.ID, &o.Kind, &o.SupplierID, &o.WarehouseID, &o.SupplierWarehouseID, &o.Quantity); err != nil {
			rows.Close()
			return nil, err
		}
		if o.Quantity <= 0 || o.Quantity > it.Quantity-total {
			rows.Close()
			return nil, fmt.Errorf("%w: invalid origin split", ErrInsufficientInventory)
		}
		total += o.Quantity
		origins = append(origins, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if total != it.Quantity {
		return nil, fmt.Errorf("%w: origin reconciliation required", ErrInsufficientInventory)
	}
	result := make([]skuAllocation, 0)
	for _, o := range origins {
		var valid bool
		switch o.Kind {
		case "warehouse":
			if o.WarehouseID == "" || o.SupplierWarehouseID != "" {
				return nil, ErrInsufficientInventory
			}
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM warehouses w WHERE w.id=$1 AND w.is_active AND ($2='' OR EXISTS(SELECT 1 FROM products p WHERE p.id=$3 AND p.supplier_id=$2)))`, o.WarehouseID, o.SupplierID, it.ProductID).Scan(&valid)
		case "supplier":
			if o.SupplierID == "" || o.SupplierWarehouseID == "" || o.WarehouseID != "" {
				return nil, ErrInsufficientInventory
			}
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier_warehouses w JOIN products p ON p.supplier_id=w.supplier_id WHERE w.id=$1 AND w.supplier_id=$2 AND w.is_active AND p.id=$3)`, o.SupplierWarehouseID, o.SupplierID, it.ProductID).Scan(&valid)
		default:
			return nil, ErrInsufficientInventory
		}
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, ErrInsufficientInventory
		}
		split := it
		split.Quantity = o.Quantity
		allocations, err := consumeInventoryBalancesAtOriginTx(ctx, tx, split, mode, o)
		if err != nil {
			return nil, err
		}
		for i := range allocations {
			allocations[i].OriginID = o.ID
		}
		result = append(result, allocations...)
	}
	return result, nil
}

// All product parents precede every SKU, including foreign-key/stock-trigger
// parents. Callers prehold their order/payment/account prefix.
func lockInventoryParentsTx(ctx context.Context, tx *sql.Tx, items []inventoryCaptureItem, resolve ...bool) error {
	products := map[string]bool{}
	variants := map[string]bool{}
	for _, it := range items {
		products[it.ProductID] = true
		if it.VariantID != "" {
			variants[it.VariantID] = true
		}
	}
	for _, group := range []struct {
		table string
		ids   map[string]bool
	}{{"products", products}, {"product_variants", variants}} {
		ids := []string{}
		for id := range group.ids {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if group.table == "product_variants" && len(resolve) > 0 && resolve[0] {
			for i := range items {
				it := &items[i]
				var err error
				it.VariantID, err = ResolveLegacyPurchaseVariant(ctx, tx, it.ProductID, it.VariantID)
				if err != nil {
					return err
				}
				if it.VariantID != "" {
					variants[it.VariantID] = true
				}
			}
			ids = ids[:0]
			for id := range variants {
				ids = append(ids, id)
			}
			sort.Strings(ids)
		}
		for _, id := range ids {
			var got string
			if err := tx.QueryRowContext(ctx, `SELECT id::text FROM `+group.table+` WHERE id=$1 FOR UPDATE`, id).Scan(&got); err != nil {
				return err
			}
		}
		if group.table == "product_variants" {
			for _, id := range ids {
				if err := lockExistingUnitCounterTx(ctx, tx, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func supplierDeclaredLocationTx(ctx context.Context, tx *sql.Tx, productID, supplierID string) (string, error) {
	var warehouse string
	err := tx.QueryRowContext(ctx, `SELECT w.id FROM products p JOIN supplier_warehouses w ON w.id=p.origin_supplier_warehouse_id AND w.supplier_id=p.supplier_id AND w.is_active WHERE p.id=$1 AND p.supplier_id=$2`, productID, supplierID).Scan(&warehouse)
	if err != nil {
		return "", fmt.Errorf("supplier stock declaration requires an active owned origin: %w", err)
	}
	return "supplier-warehouse:" + warehouse, nil
}

// FulfillmentOrigin is chosen from eligible attributed stock, never names or a
// current routing flag. Payment revalidates this same identity under row locks.
type FulfillmentOrigin struct {
	WarehouseID, SupplierWarehouseID, Label string
	Quantity                                int
}

func ResolveInventoryOrigins(ctx context.Context, db *sql.DB, productID, variantID, routing string, quantity int) ([]FulfillmentOrigin, error) {
	var o FulfillmentOrigin
	if quantity <= 0 {
		return nil, ErrInsufficientInventory
	}
	kind := "warehouse"
	if routing == "supplier_direct" {
		kind = "supplier"
	}
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(b.warehouse_id,''),COALESCE(b.supplier_warehouse_id,''),COALESCE(w.name,sw.name,''),SUM(b.quantity)
 FROM inventory_balances b JOIN products p ON p.id=b.product_id
 LEFT JOIN product_variants pv ON pv.id=NULLIF($2,'')::uuid AND pv.product_id=p.id
 LEFT JOIN warehouses w ON w.id=b.warehouse_id AND w.is_active
 LEFT JOIN supplier_warehouses sw ON sw.id=b.supplier_warehouse_id AND sw.supplier_id=p.supplier_id AND sw.is_active
 WHERE b.product_id=$1 AND b.status='sellable' AND ($2='' OR pv.id IS NOT NULL) AND
 ((pv.inventory_mode='tracked' AND b.variant_id=pv.id) OR (COALESCE(pv.inventory_mode,'legacy')<>'tracked' AND b.variant_id IS NULL))
 AND (($3='warehouse' AND w.id IS NOT NULL AND b.supplier_warehouse_id IS NULL AND b.location_type IN ('warehouse','bin')) OR
 ($3='supplier' AND sw.id IS NOT NULL AND b.warehouse_id IS NULL AND b.supplier_id=p.supplier_id AND b.location_type='supplier'))
 GROUP BY b.warehouse_id,b.supplier_warehouse_id,w.name,sw.name HAVING SUM(b.quantity)>0
 ORDER BY b.warehouse_id COLLATE "C",b.supplier_warehouse_id COLLATE "C"`, productID, variantID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var origins []FulfillmentOrigin
	remaining := quantity
	for rows.Next() {
		if err := rows.Scan(&o.WarehouseID, &o.SupplierWarehouseID, &o.Label, &o.Quantity); err != nil {
			return nil, err
		}
		if remaining > 0 {
			if o.Quantity > remaining {
				o.Quantity = remaining
			}
			remaining -= o.Quantity
			origins = append(origins, o)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if remaining > 0 {
		return nil, ErrInsufficientInventory
	}
	return origins, nil
}

// SingleSupplierPickupOrigin prevents a one-parcel provider API from silently
// substituting one pickup warehouse for a persisted multi-origin purchase.
// Multi-pickup parcels require a separate shipment/provider event per origin.
func SingleSupplierPickupOrigin(lines []SupplierOrderLine) (string, error) {
	origin := ""
	for _, line := range lines {
		if line.Quantity <= 0 || line.OriginWarehouseID == "" {
			return "", fmt.Errorf("fulfillment origin reconciliation required")
		}
		if origin != "" && origin != line.OriginWarehouseID {
			return "", fmt.Errorf("multiple supplier pickup origins require separate shipments")
		}
		origin = line.OriginWarehouseID
	}
	if origin == "" {
		return "", fmt.Errorf("fulfillment origin reconciliation required")
	}
	return origin, nil
}
