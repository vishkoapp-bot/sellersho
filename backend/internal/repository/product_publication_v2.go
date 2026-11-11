package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

func insertProductV2VariantsTx(ctx context.Context, tx *sql.Tx, productID, supplierID string, data map[string]interface{}) error {
	for _, entry := range data["variants"].([]interface{}) {
		v := entry.(map[string]interface{})
		spnc, err := uniqueSPNCTx(ctx, tx)
		if err != nil {
			return err
		}
		attrs, _ := json.Marshal(v["attributes"])
		selection, _ := json.Marshal(v["selection"])
		var cost, maxQty interface{}
		if c := v["supplier_base_cost"].(float64); c > 0 {
			cost = int64(c)
		}
		minQty := 1
		if n, ok := v["min_cart_qty"].(float64); ok {
			minQty = int(n)
		}
		if n, ok := v["max_cart_qty"].(float64); ok {
			maxQty = int(n)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO product_variants(id,product_id,spnc,variant_name,attributes,is_default,stock,supplier_base_cost,selection,combination_key,inventory_mode,is_active,min_cart_qty,max_cart_qty)
		 VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb,$10,'tracked',$11,$12,$13)`, v["id"], productID, spnc, v["variant_name"], string(attrs), data["productType"] == "simple", int(v["stock"].(float64)), cost, string(selection), v["combination_key"], v["is_active"], minQty, maxQty); err != nil {
			return fmt.Errorf("failed to publish stable SKU: %w", err)
		}
	}
	return SeedTrackedSKUInventoryTx(ctx, tx, productID, supplierID)
}
