package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// insertProductV2VariantsTx inserts exactly the submitted variants.
//
// B2 (2026-11-13): real product-default assignment (OQ-1).
//   - At publication (assignDefault=true) the proposal's is_default
//     flag is honored for variable products. When no variant is
//     named, the FIRST ACTIVE submitted variant is the default
//     (documented fallback: the submitted order is the supplier's
//     order, and the repo loader orders is_default first, so the DB
//     tells the truth about the effective default instead of leaving
//     every variable product with is_default=false). An explicitly
//     flagged variant is honored even when inactive. `simple` keeps
//     its existing behaviour (its single SKU is always the default).
//   - Post-publication adds (assignDefault=false) NEVER set
//     is_default: the product default is fixed at publication and
//     there is no API to change it afterwards, so a later-added
//     variant must not silently become (or displace) the default.
//     (Validation also rejects an incoming is_default=true when a
//     default already exists, because the add path validates
//     existing + incoming together; the flag is ignored here as the
//     second line of defence for legacy all-false products.)
func insertProductV2VariantsTx(ctx context.Context, tx *sql.Tx, productID, supplierID string, data map[string]interface{}, assignDefault bool, approvalID ...string) error {
	rawVariants, _ := data["variants"].([]interface{})
	// The default for this insert: the flagged variant, else the
	// first active submitted variant (publication only).
	defaultID := ""
	if assignDefault {
		for _, entry := range rawVariants {
			v, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			if b, _ := v["is_default"].(bool); b {
				defaultID, _ = v["id"].(string)
				break
			}
		}
		if defaultID == "" {
			for _, entry := range rawVariants {
				v, ok := entry.(map[string]interface{})
				if !ok {
					continue
				}
				if active, _ := v["is_active"].(bool); active {
					defaultID, _ = v["id"].(string)
					break
				}
			}
		}
		if defaultID == "" && len(rawVariants) > 0 {
			if first, ok := rawVariants[0].(map[string]interface{}); ok {
				defaultID, _ = first["id"].(string)
			}
		}
	}
	for _, entry := range rawVariants {
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
		// P0 physicals: validated + normalised by validateVariantPhysical
		// (float64 or nil); nil = inherit from the product.
		var weight, length, width, height interface{}
		if n, ok := v["weight_grams"].(float64); ok {
			weight = int(n)
		}
		if n, ok := v["length_mm"].(float64); ok {
			length = int(n)
		}
		if n, ok := v["width_mm"].(float64); ok {
			width = int(n)
		}
		if n, ok := v["height_mm"].(float64); ok {
			height = int(n)
		}
		// P1(c)/P2(b): validated + normalised by validateVariantSale /
		// validateVariantClasses (sale_price float64, sale_from/to
		// RFC3339 strings, classes strings; nil = absent).
		var salePrice, saleFrom, saleTo, shipClass, taxClass interface{}
		if n, ok := v["sale_price"].(float64); ok && n > 0 {
			salePrice = int64(n)
		}
		if s, ok := v["sale_from"].(string); ok && s != "" {
			saleFrom = s
		}
		if s, ok := v["sale_to"].(string); ok && s != "" {
			saleTo = s
		}
		if s, ok := v["shipping_class"].(string); ok && s != "" {
			shipClass = s
		}
		if s, ok := v["tax_class"].(string); ok && s != "" {
			taxClass = s
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO product_variants(id,product_id,spnc,variant_name,attributes,is_default,stock,supplier_base_cost,selection,combination_key,inventory_mode,is_active,min_cart_qty,max_cart_qty,weight_grams,length_mm,width_mm,height_mm,sale_price,sale_from,sale_to,shipping_class,tax_class)
		 VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb,$10,'tracked',$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`, v["id"], productID, spnc, v["variant_name"], string(attrs), data["productType"] == "simple" || (assignDefault && defaultID != "" && v["id"] == defaultID), int(v["stock"].(float64)), cost, string(selection), v["combination_key"], v["is_active"], minQty, maxQty, weight, length, width, height, salePrice, saleFrom, saleTo, shipClass, taxClass); err != nil {
			return fmt.Errorf("failed to publish stable SKU: %w", err)
		}
		// P0 variant images (Fix 9 pattern): proposal-carried URLs,
		// written in the same tx as the variant row.
		if raw, ok := v["images"]; ok && raw != nil {
			var imgs []string
			switch t := raw.(type) {
			case []interface{}:
				for _, e := range t {
					if str, ok := e.(string); ok {
						imgs = append(imgs, str)
					}
				}
			case []string:
				imgs = t
			}
			for k, u := range imgs {
				if _, err = tx.ExecContext(ctx, `INSERT INTO product_variant_images (variant_id, image_url, sort_order) VALUES ($1, $2, $3)`, v["id"], u, k); err != nil {
					return fmt.Errorf("failed to publish variant image: %w", err)
				}
			}
		}
	}
	return SeedTrackedSKUInventoryTx(ctx, tx, productID, supplierID, approvalID...)
}
