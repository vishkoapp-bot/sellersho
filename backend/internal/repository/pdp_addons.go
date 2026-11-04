package repository

import (
	"context"
	"database/sql"
)

// PDPAddon is one fee add-on card on a product page, exactly as live's
// PDP mapper reads it: id + title + precomputed feePerUnit (live's cart
// fee helper sums product.addons feePerUnit over the selected ids; it
// never recomputes from price_mode client-side).
type PDPAddon struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	FeePerUnit  int64  `json:"feePerUnit"`
	OwnerType   string `json:"ownerType"`
}

// GetPDPAddons resolves the fee add-ons a product page shows for the
// viewing store, plus the store's addon section title. Rules (FIX 14b,
// from migrations 202610230005 + 202610250403 and live's visible
// behaviour -- live serves no addon keys for any of the 40 current
// products, so the resolver must stay silent for current data):
//
//   - Dropshipper store: ONLY rows the dropshipper explicitly selected
//     for (store, product) in store_product_addons with addon_id set.
//     Direct picks (linked_product_id rows) are complementary products,
//     not fee cards, and are not returned here.
//   - Central view: the SUPPLIER's defined list for the product --
//     attachments at product/category/global scope whose addon is owned
//     by the product's supplier -- minus the central store's exclusion
//     rows. Platform-owned addons are not the supplier's defined list
//     and do not display on central (this is why the staging platform
//     fee addon attached to prod-coffee-maker stays hidden, like live).
//   - feePerUnit mirrors the order handler exactly: fixed -> price_value;
//     percent -> unitPrice * price_value / 100, capped by cap_toman.
//     Addons linked to a catalog product are skipped here (their fee is
//     the linked product's resolved price, a separate complementary
//     display, not a fee card).
//   - Section title: the store's addon_section_title, defaulting to
//     «محصولات مکمل» (migration 202610250403) when empty/unset.
//
// A nil/empty result with an empty title is the normal case today: no
// store has selected fee addons and no supplier-defined fee addons
// exist, so the PDP renders no addon section, like live.
func (r *CatalogRepository) GetPDPAddons(ctx context.Context, storeID string, isCentral bool, productID, categorySlug, supplierID string, unitPrice int64) ([]PDPAddon, string, error) {
	title := "محصولات مکمل"
	if storeID != "" {
		var t sql.NullString
		if err := r.db.QueryRowContext(ctx, `SELECT addon_section_title FROM stores WHERE id = $1`, storeID).Scan(&t); err == nil && t.Valid && t.String != "" {
			title = t.String
		}
	}

	var rows *sql.Rows
	var err error
	if isCentral {
		rows, err = r.db.QueryContext(ctx, `
			SELECT pa.id, pa.title, COALESCE(pa.description, ''), pa.owner_type,
			       pa.price_mode, pa.price_value, pa.cap_toman, pa.linked_product_id
			FROM product_addon_attachments att
			JOIN product_addons pa ON pa.id = att.addon_id
			WHERE pa.is_active AND pa.approval_status = 'approved'
			  AND pa.owner_supplier_id = $1
			  AND ((att.scope_type = 'product' AND att.product_id = $2)
			    OR (att.scope_type = 'category' AND att.category_slug = $3)
			    OR att.scope_type = 'global')
			  AND NOT EXISTS (
				SELECT 1 FROM store_product_addon_exclusions ex
				WHERE ex.store_id = $4 AND ex.product_id = $2 AND ex.addon_id = pa.id
			  )
			ORDER BY pa.sort_order, pa.title`,
			supplierID, productID, categorySlug, storeID)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT pa.id, pa.title, COALESCE(pa.description, ''), pa.owner_type,
			       pa.price_mode, pa.price_value, pa.cap_toman, pa.linked_product_id
			FROM store_product_addons spa
			JOIN product_addons pa ON pa.id = spa.addon_id
			WHERE spa.store_id = $1 AND spa.product_id = $2 AND spa.addon_id IS NOT NULL
			  AND pa.is_active AND pa.approval_status = 'approved'
			ORDER BY spa.sort_order, pa.title`,
			storeID, productID)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	out := []PDPAddon{}
	for rows.Next() {
		var a PDPAddon
		var mode string
		var priceVal float64
		var cap int64
		var linked sql.NullString
		if err := rows.Scan(&a.ID, &a.Title, &a.Description, &a.OwnerType, &mode, &priceVal, &cap, &linked); err != nil {
			return nil, "", err
		}
		if linked.Valid && linked.String != "" {
			continue
		}
		if mode == "percent" {
			a.FeePerUnit = int64(float64(unitPrice) * priceVal / 100.0)
		} else {
			a.FeePerUnit = int64(priceVal)
		}
		if cap > 0 && a.FeePerUnit > cap {
			a.FeePerUnit = cap
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return out, title, nil
}
