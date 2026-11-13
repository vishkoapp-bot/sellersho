package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

func (r *SupplierRepository) CreateProductVariantV2(ctx context.Context, supplierID, productID string, variant SupplierProductVariant) (SupplierProductVariant, error) {
	if _, err := r.createProductVariantsV2(ctx, supplierID, productID, []SupplierProductVariant{variant}); err != nil {
		return SupplierProductVariant{}, err
	}
	variants, err := r.ListProductVariants(ctx, supplierID, productID)
	if err != nil {
		return SupplierProductVariant{}, err
	}
	for _, v := range variants {
		if v.ID == variant.ID {
			return v, nil
		}
	}
	return SupplierProductVariant{}, sql.ErrNoRows
}

func (r *SupplierRepository) createProductVariantsV2(ctx context.Context, supplierID, productID string, variants []SupplierProductVariant) (int, error) {
	if len(variants) == 0 || len(variants) > MaxProductSKUs {
		return 0, fmt.Errorf("invalid SKU count")
	}
	// B2 (2026-11-13): the product default is fixed at publication.
	// A post-publication add that names a default is a request to
	// change it, and is refused outright — not silently ignored
	// (there is no API to change the product default afterwards).
	for _, v := range variants {
		if v.IsDefault {
			return 0, fmt.Errorf("product default is fixed at publication and cannot be changed by adding variants")
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var schemaRaw []byte
	var kind, category string
	var version int
	var fallback int64
	err = tx.QueryRowContext(ctx, `SELECT product_type,schema_version,attribute_schema,category_slug,supplier_base_cost FROM products WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, productID, supplierID).Scan(&kind, &version, &schemaRaw, &category, &fallback)
	if err != nil {
		return 0, err
	}
	if version != 2 {
		return 0, fmt.Errorf("canonical SKU mutation requires V2 product")
	}
	if kind == "simple" {
		return 0, fmt.Errorf("simple product already has its single SKU")
	}
	var schema interface{}
	if err = json.Unmarshal(schemaRaw, &schema); err != nil {
		return 0, err
	}
	var existingRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(v) ORDER BY v.id),'[]'::jsonb) FROM product_variants v WHERE product_id=$1`, productID).Scan(&existingRaw); err != nil {
		return 0, err
	}
	var existing []interface{}
	if err = json.Unmarshal(existingRaw, &existing); err != nil {
		return 0, err
	}
	for _, entry := range existing {
		v := entry.(map[string]interface{})
		if v["supplier_base_cost"] == nil {
			v["supplier_base_cost"] = float64(fallback)
		}
	}
	newRaw, err := json.Marshal(variants)
	if err != nil {
		return 0, err
	}
	var incoming []interface{}
	if err = json.Unmarshal(newRaw, &incoming); err != nil {
		return 0, err
	}
	for _, entry := range incoming {
		v := entry.(map[string]interface{})
		if v["supplier_base_cost"] == nil {
			v["supplier_base_cost"] = float64(0)
		}
	}
	all := append(append([]interface{}{}, existing...), incoming...)
	data := map[string]interface{}{"schemaVersion": float64(2), "productType": kind, "productSchema": schema, "categorySlug": category, "variants": all}
	data["product_id"] = productID
	if err = ValidateProductProposalTx(ctx, tx, data); err != nil {
		return 0, err
	}
	// No registry requests can be introduced by a SKU-only mutation: the stored
	// schema is fixed and every option was already reviewed when published.
	data["variants"] = incoming
	// Post-publication add (assignDefault=false): a later-added
	// variant never becomes the product default (B2 decision — the
	// default is fixed at publication; see insertProductV2VariantsTx).
	if err = insertProductV2VariantsTx(ctx, tx, productID, supplierID, data, false); err != nil {
		return 0, err
	}
	// Extend canonical facet union for the newly activated combinations.
	if err = syncProductCanonicalFacetsV2Tx(ctx, tx, productID); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(incoming), nil
}

func syncProductCanonicalFacetsV2Tx(ctx context.Context, tx *sql.Tx, productID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO product_canonical_attribute_values(product_id,attribute_id,option_id)
	 SELECT p.id,a->>'id',v->>'id' FROM products p CROSS JOIN LATERAL jsonb_array_elements(p.attribute_schema->'attributes') a CROSS JOIN LATERAL jsonb_array_elements(a->'values') v WHERE p.id=$1 AND a->>'role'='fixed'
	 UNION SELECT pv.product_id,s.key,s.value FROM product_variants pv CROSS JOIN LATERAL jsonb_each_text(pv.selection) s WHERE pv.product_id=$1 AND pv.is_active AND pv.inventory_mode='tracked'
	 ON CONFLICT DO NOTHING`, productID)
	return err
}
