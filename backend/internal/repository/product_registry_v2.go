package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/lib/pq"
)

type productRegistryQuery interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

// The same category-scoped definitions power the editor and transaction-time
// validation. A global DK value is insufficient: its category link must exist.
func loadCategoryProductAttributes(ctx context.Context, q productRegistryQuery, slug string) ([]domain.ProductAttributeDefinition, error) {
	rows, err := q.QueryContext(ctx, `
	 WITH category AS (SELECT id FROM categories WHERE slug=$1 OR id=$1)
	 SELECT 'color', 'رنگ', 'color', NULL::BIGINT,
	        jsonb_agg(jsonb_build_object('id','color:'||cc.dk_color_id,'label',cc.title,'hexCode',COALESCE(cc.hex_code,'')) ORDER BY cc.dk_color_id)
	 FROM dk_category_colors cc JOIN category c ON c.id=cc.category_id HAVING COUNT(*)>0
	 UNION ALL
	 SELECT 'dk:'||a.dk_attr_id,a.title,'dk',a.dk_attr_id::BIGINT,
	        COALESCE((SELECT jsonb_agg(jsonb_build_object('id','dkv:'||a.dk_attr_id||':'||v.dk_value_id,'label',v.title_fa) ORDER BY v.dk_value_id)
	         FROM dk_category_attribute_values cav JOIN dk_attribute_values v ON v.dk_attr_id=cav.dk_attr_id AND v.dk_value_id=cav.dk_value_id
	         WHERE cav.category_id=ca.category_id AND cav.dk_attr_id=a.dk_attr_id),'[]'::jsonb)
	 FROM dk_category_attributes ca JOIN dk_attributes a ON a.dk_attr_id=ca.dk_attr_id JOIN category c ON c.id=ca.category_id
	 UNION ALL
	 SELECT a.id,a.name,'local',NULL::BIGINT,
	        COALESCE((SELECT jsonb_agg(jsonb_build_object('id',v.id,'label',v.label) ORDER BY v.label,v.id)
	         FROM category_local_product_attribute_values cav JOIN local_product_attribute_values v ON v.attribute_id=cav.attribute_id AND v.id=cav.value_id
	         WHERE cav.category_id=ca.category_id AND cav.attribute_id=a.id),'[]'::jsonb)
	 FROM category_local_product_attributes ca JOIN local_product_attributes a ON a.id=ca.attribute_id JOIN category c ON c.id=ca.category_id
	 ORDER BY 2,1`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ProductAttributeDefinition{}
	for rows.Next() {
		var a domain.ProductAttributeDefinition
		var registry sql.NullInt64
		var values []byte
		if err = rows.Scan(&a.ID, &a.Name, &a.Source, &registry, &values); err != nil {
			return nil, err
		}
		if registry.Valid {
			a.RegistryID = &registry.Int64
		}
		if err = json.Unmarshal(values, &a.Values); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	extensions, err := q.QueryContext(ctx, `SELECT e.attribute_id,e.id,e.label FROM category_product_attribute_option_extensions ce JOIN product_attribute_option_extensions e ON e.attribute_id=ce.attribute_id AND e.id=ce.value_id JOIN categories c ON c.id=ce.category_id WHERE c.slug=$1 OR c.id=$1 ORDER BY e.label,e.id`, slug)
	if err != nil {
		return nil, err
	}
	defer extensions.Close()
	index := map[string]int{}
	for i, a := range out {
		index[a.ID] = i
	}
	for extensions.Next() {
		var attr string
		var v domain.ProductAttributeOption
		if err = extensions.Scan(&attr, &v.ID, &v.Label); err != nil {
			return nil, err
		}
		if i, ok := index[attr]; ok {
			out[i].Values = append(out[i].Values, v)
		}
	}
	return out, extensions.Err()
}

func (r *CatalogRepository) GetCategoryProductAttributes(ctx context.Context, slug string) ([]domain.ProductAttributeDefinition, error) {
	return loadCategoryProductAttributes(ctx, r.db, slug)
}

func validateV2Registry(schema *domain.ProductSchema, available []domain.ProductAttributeDefinition) error {
	byID := map[string]domain.ProductAttributeDefinition{}
	byName := map[string]string{}
	optionOwners := map[string]string{}
	for _, a := range available {
		byID[a.ID] = a
		byName[productIdentityText(a.Name)] = a.ID
		for _, v := range a.Values {
			optionOwners[v.ID] = a.ID
		}
	}
	for _, a := range schema.Attributes {
		registered, exists := byID[a.ID]
		if !exists && a.Source == "requested" {
			if other := byName[productIdentityText(a.Name)]; other != "" && other != a.ID {
				return fmt.Errorf("requested attribute already exists in category: %s", a.Name)
			}
			continue
		}
		if !exists {
			return fmt.Errorf("attribute does not belong to selected category: %s", a.ID)
		}
		if a.Name != registered.Name {
			return fmt.Errorf("attribute label differs from category registry: %s", a.ID)
		}
		if a.Source != "requested" && a.Source != registered.Source {
			return fmt.Errorf("attribute source differs from registry: %s", a.ID)
		}
		options := map[string]string{}
		for _, v := range registered.Values {
			options[v.ID] = v.Label
		}
		for _, v := range a.Values {
			if owner := optionOwners[v.ID]; owner != "" && owner != a.ID {
				return fmt.Errorf("option belongs to another attribute: %s", v.ID)
			}
			label, ok := options[v.ID]
			if !ok && requestedOptionID(v.ID) {
				for _, known := range options {
					if productIdentityText(known) == productIdentityText(v.Label) {
						return fmt.Errorf("requested option already exists in category: %s", v.Label)
					}
				}
				continue
			}
			if !ok || label != v.Label {
				return fmt.Errorf("option does not belong to selected category/attribute: %s", v.ID)
			}
		}
	}
	return nil
}

// ValidateProductProposalTx must run inside each submission/correction/approval
// transaction. It never publishes requests while a proposal is pending.
func ValidateProductProposalTx(ctx context.Context, tx *sql.Tx, data map[string]interface{}, proposalIDs ...string) error {
	if err := ValidateProductVariationProposal(data); err != nil {
		return err
	}
	if !IsProductV2(data) {
		return nil
	}
	proposalID := ""
	if len(proposalIDs) > 0 {
		proposalID = proposalIDs[0]
	}
	if err := validateProductSKUOwnershipTx(ctx, tx, data, proposalID); err != nil {
		return err
	}
	schema, err := productSchemaFromData(data)
	if err != nil {
		return err
	}
	slug, _ := data["categorySlug"].(string)
	if slug == "" {
		slug, _ = data["category"].(string)
	}
	var category, categorySlug string
	if err = tx.QueryRowContext(ctx, `SELECT id, slug FROM categories WHERE slug=$1 OR id=$1 LIMIT 1 FOR SHARE`, slug).Scan(&category, &categorySlug); err != nil {
		return fmt.Errorf("invalid product category: %w", err)
	}
	definitions, err := loadCategoryProductAttributes(ctx, tx, category)
	if err != nil {
		return err
	}
	if err = validateLocalProductOptionOwnershipTx(ctx, tx, schema); err != nil {
		return err
	}
	if err = validateV2Registry(schema, definitions); err != nil {
		return err
	}
	// M1 (2026-11-13): sale R/S floor at proposal/review time —
	// runs last, after validateProductV2 has normalised the sale
	// fields (rules 1/4/5 + expired-sale normalisation).
	return validateVariantSaleFloorsTx(ctx, tx, data, categorySlug)
}

// loadPlatformPricingSettingsTx loads the platform pricing settings
// inside a validation transaction (M1 sale-floor check). It mirrors
// PricingRepository.GetPlatformPricingSettings, including its
// degrade-to-defaults behaviour: a missing settings row, missing
// ladder/ceiling tables, or any other load error yields the code
// defaults rather than failing validation — the same fallback the
// live sale endpoint uses via loadPricingSettings. It reads settings
// only; no price formula is changed here.
func loadPlatformPricingSettingsTx(ctx context.Context, tx *sql.Tx) *pricing.PlatformPricingSettings {
	s := &pricing.PlatformPricingSettings{
		ProductPlatformPcts:  map[string]float64{},
		CategoryPlatformPcts: map[string]float64{},
		ProductMarginCeilings: map[string]float64{},
	}
	var earlyWithdrawalFeePct sql.NullFloat64
	err := tx.QueryRowContext(ctx, `
		SELECT platform_pct, central_profit_pct, increase_cap_pct, increase_locked,
		       max_profit_margin, min_profit_margin, default_margin, central_store_markup_percent,
		       referral_is_enabled, referral_commission_percent,
		       settlement_day, early_withdrawal_fee_pct, wallet_min_withdrawal,
		       wallet_spend_enabled, wallet_withdraw_enabled
		FROM platform_settings WHERE id = 1
	`).Scan(
		&s.PlatformPct, &s.CentralProfitPct, &s.IncreaseCapPct, &s.IncreaseLocked,
		&s.MaxProfitMargin, &s.MinProfitMargin, &s.DefaultMargin, &s.CentralStoreMarkupPercent,
		&s.ReferralIsEnabled, &s.ReferralCommissionPercent,
		&s.SettlementDay, &earlyWithdrawalFeePct, &s.WalletMinWithdrawal,
		&s.WalletSpendEnabled, &s.WalletWithdrawEnabled,
	)
	if err != nil {
		return pricing.DefaultPlatformPricingSettings()
	}
	s.EarlyWithdrawalFeePct = earlyWithdrawalFeePct.Float64
	// The ladder tables may be absent on a database that has not run
	// their migration; the engine then degrades to the global
	// platform_pct instead of failing (same as GetPlatformPricingSettings).
	if rows, lerr := tx.QueryContext(ctx, `SELECT product_id, pct FROM platform_pct_products`); lerr == nil {
		for rows.Next() {
			var id string
			var pct float64
			if rows.Scan(&id, &pct) == nil {
				s.ProductPlatformPcts[id] = pct
			}
		}
		rows.Close()
	}
	if rows, lerr := tx.QueryContext(ctx, `SELECT category_slug, pct FROM platform_pct_categories`); lerr == nil {
		for rows.Next() {
			var slug string
			var pct float64
			if rows.Scan(&slug, &pct) == nil {
				s.CategoryPlatformPcts[slug] = pct
			}
		}
		rows.Close()
	}
	if rows, lerr := tx.QueryContext(ctx, `SELECT product_id, max_margin_pct FROM product_margin_ceilings`); lerr == nil {
		for rows.Next() {
			var id string
			var pct float64
			if rows.Scan(&id, &pct) == nil {
				s.ProductMarginCeilings[id] = pct
			}
		}
		rows.Close()
	}
	return s
}

// validateVariantSaleFloorsTx enforces the P1(c) sale rules 2/3
// (S > R, S < P_central) at proposal/review time (M1), using the
// same engine as the live sale endpoint: R and P_central come from
// pricing.ResolveReferencePricingForStoreProductVariantWithCost
// resolved as the central store, from the variant's proposed cost C
// and the platform-pct ladder. It runs inside
// ValidateProductProposalTx, so it covers propose, correction, and
// approval/publication re-validation — and bulk-add, where only the
// incoming variants are checked (see the stored-row skip below). A
// bad sale is therefore rejected before it can publish; it can no
// longer sail through review and suspend silently at display/charge
// time. The documented deviation stands: an EXPIRED sale never
// reaches here — validateProductV2 normalises it away first — so it
// is still dropped, not rejected.
//
// Stored-row skip: variants that already exist as stored rows for
// this product are NOT re-checked. Their sales were validated at
// write time (live endpoint) and a later governed cost rise only
// suspends them at runtime (inert). Re-checking them here would
// block an unrelated bulk-add for a sale that has simply gone
// stale — the same philosophy as the expired-sale normalisation
// (D1). data carries product_id only on the bulk-add /
// re-publication paths; propose-time payloads have none, so every
// row is checked there.
//
// Costless variants: a variant with no positive own cost (inactive,
// NULL) inherits the product cost at read time, so the check uses
// that inherited cost — the stored product cost on the add path,
// else the proposal's aggregate product cost
// (data["supplierBaseCost"], set by validateProductV2 as the min
// active submitted cost and stored as the product cost at publish).
// The runtime suspension remains the backstop for any residual
// mismatch.
func validateVariantSaleFloorsTx(ctx context.Context, tx *sql.Tx, data map[string]interface{}, categorySlug string) error {
	raw, ok := data["variants"].([]interface{})
	if !ok || len(raw) == 0 {
		return nil
	}
	skip := map[string]bool{}
	productID, _ := data["product_id"].(string)
	if productID != "" {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM product_variants WHERE product_id=$1`, productID)
		if err == nil {
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					skip[id] = true
				}
			}
			rows.Close()
		}
	}
	var settings *pricing.PlatformPricingSettings
	var storedProductCost *int64
	central := domain.Store{ID: "store-central", Slug: "central", OwnerType: "central"}
	for _, entry := range raw {
		v, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := v["id"].(string)
		if skip[id] {
			continue
		}
		sale, hasSale := jsonNumber(v["sale_price"])
		if !hasSale || sale <= 0 {
			continue
		}
		cost, _ := jsonNumber(v["supplier_base_cost"])
		if cost <= 0 {
			// No own cost: inherit the product cost (see doc above).
			if productID != "" {
				if storedProductCost == nil {
					var c int64
					if err := tx.QueryRowContext(ctx, `SELECT supplier_base_cost FROM products WHERE id=$1`, productID).Scan(&c); err == nil {
						storedProductCost = &c
					} else {
						zero := int64(0)
						storedProductCost = &zero
					}
				}
				cost = float64(*storedProductCost)
			} else {
				aggregate, _ := jsonNumber(data["supplierBaseCost"])
				cost = aggregate
			}
		}
		if cost <= 0 {
			// Unresolvable cost (no product cost anywhere): the sale
			// cannot be floor-checked here; runtime suspension is the
			// backstop (a sale at/below R is inert, never money-losing).
			continue
		}
		if settings == nil {
			settings = loadPlatformPricingSettingsTx(ctx, tx)
		}
		prod := domain.Product{ID: productID, CategorySlug: categorySlug, SupplierBaseCost: int64(cost)}
		res := pricing.ResolveReferencePricingForStoreProductVariantWithCost(prod, int64(cost), id, central, settings, nil, nil)
		s := int64(sale)
		if s <= res.BaseCost {
			return fmt.Errorf("salePrice must be above the reference cost %d", res.BaseCost)
		}
		if s >= res.FinalPrice {
			return fmt.Errorf("salePrice must be below the central price %d", res.FinalPrice)
		}
	}
	return nil
}

func validateLocalProductOptionOwnershipTx(ctx context.Context, tx *sql.Tx, schema *domain.ProductSchema) error {
	type choice struct{ attribute, label string }
	wanted := map[string]choice{}
	ids := []string{}
	for _, a := range schema.Attributes {
		for _, v := range a.Values {
			if !uuidBackedID(v.ID) {
				continue
			}
			if previous, ok := wanted[v.ID]; ok && previous.attribute != a.ID {
				return fmt.Errorf("local option identity used by two attributes")
			}
			wanted[v.ID] = choice{a.ID, v.Label}
			ids = append(ids, v.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,attribute_id,label FROM local_product_attribute_values WHERE id=ANY($1) UNION ALL SELECT id,attribute_id,label FROM product_attribute_option_extensions WHERE id=ANY($1)`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, attr, label string
		if err = rows.Scan(&id, &attr, &label); err != nil {
			return err
		}
		if expected := wanted[id]; expected.attribute != attr || expected.label != label {
			return fmt.Errorf("local option identity belongs to another attribute or label")
		}
	}
	return rows.Err()
}

func publishProductModelV2Tx(ctx context.Context, tx *sql.Tx, productID, categorySlug, proposalID string, data map[string]interface{}) error {
	schema, err := productSchemaFromData(data)
	if err != nil {
		return err
	}
	// Serialize requested registry publication, including duplicate-name races.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(20261111,2)`); err != nil {
		return err
	}
	if err = ValidateProductProposalTx(ctx, tx, data, proposalID); err != nil {
		return err
	}
	var category string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE slug=$1 OR id=$1 LIMIT 1`, categorySlug).Scan(&category); err != nil {
		return err
	}
	for _, a := range schema.Attributes {
		if a.Source != "requested" {
			continue
		}
		// Existing stable IDs are immutable. A reused draft cannot rename registry
		// data or overwrite definitions already referenced by another product.
		var actualName string
		err = tx.QueryRowContext(ctx, `INSERT INTO local_product_attributes(id,name,normalized_name) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET id=EXCLUDED.id RETURNING name`, a.ID, a.Name, productIdentityText(a.Name)).Scan(&actualName)
		if err != nil {
			return err
		}
		if actualName != a.Name {
			return fmt.Errorf("requested attribute identity already registered")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO category_local_product_attributes(category_id,attribute_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, category, a.ID); err != nil {
			return err
		}
		for _, v := range a.Values {
			var label string
			err = tx.QueryRowContext(ctx, `INSERT INTO local_product_attribute_values(attribute_id,id,label,normalized_label) VALUES($1,$2,$3,$4) ON CONFLICT(attribute_id,id) DO UPDATE SET id=EXCLUDED.id RETURNING label`, a.ID, v.ID, v.Label, productIdentityText(v.Label)).Scan(&label)
			if err != nil {
				return err
			}
			if label != v.Label {
				return fmt.Errorf("requested option identity already registered")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO category_local_product_attribute_values(category_id,attribute_id,value_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, category, a.ID, v.ID); err != nil {
				return err
			}
		}
	}
	available, err := loadCategoryProductAttributes(ctx, tx, category)
	if err != nil {
		return err
	}
	registeredOptions := map[string]map[string]bool{}
	for _, a := range available {
		registeredOptions[a.ID] = map[string]bool{}
		for _, v := range a.Values {
			registeredOptions[a.ID][v.ID] = true
		}
	}
	for _, a := range schema.Attributes {
		for _, v := range a.Values {
			if !requestedOptionID(v.ID) || registeredOptions[a.ID][v.ID] {
				continue
			}
			var label string
			err = tx.QueryRowContext(ctx, `INSERT INTO product_attribute_option_extensions(attribute_id,id,label,normalized_label) VALUES($1,$2,$3,$4) ON CONFLICT(attribute_id,id) DO UPDATE SET id=EXCLUDED.id RETURNING label`, a.ID, v.ID, v.Label, productIdentityText(v.Label)).Scan(&label)
			if err != nil {
				return err
			}
			if label != v.Label {
				return fmt.Errorf("requested option identity already registered")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO category_product_attribute_option_extensions(category_id,attribute_id,value_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, category, a.ID, v.ID); err != nil {
				return err
			}
		}
	}
	b, _ := json.Marshal(schema)
	if _, err = tx.ExecContext(ctx, `UPDATE products SET product_type=$2,schema_version=2,attribute_schema=$3::jsonb WHERE id=$1`, productID, schema.Type, string(b)); err != nil {
		return err
	}
	// Materialize product facets from fixed options and the union of actual active
	// SKU selections. Unused axis options are not claims about the product.
	for _, a := range schema.Attributes {
		values := map[string]bool{}
		if a.Role == "fixed" {
			values[a.Values[0].ID] = true
		} else {
			for _, raw := range data["variants"].([]interface{}) {
				v := raw.(map[string]interface{})
				if v["is_active"] == true {
					selection := v["selection"].(map[string]interface{})
					values[selection[a.ID].(string)] = true
				}
			}
		}
		for option := range values {
			if _, err = tx.ExecContext(ctx, `INSERT INTO product_canonical_attribute_values(product_id,attribute_id,option_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, productID, a.ID, option); err != nil {
				return err
			}
			if a.Source != "dk" || requestedOptionID(option) {
				continue
			}
			valueID, e := signedRegistryID(option, "dkv:"+strconv.FormatInt(*a.RegistryID, 10)+":")
			if e != nil {
				return e
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO product_attribute_values(product_id,dk_attribute_id,dk_value_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, productID, *a.RegistryID, valueID); err != nil {
				return err
			}
		}
	}
	return nil
}

func productDraftSKUIds(data map[string]interface{}) []string {
	ids := []string{}
	for _, raw := range data["variants"].([]interface{}) {
		ids = append(ids, raw.(map[string]interface{})["id"].(string))
	}
	return ids
}

func validateProductSKUOwnershipTx(ctx context.Context, tx *sql.Tx, data map[string]interface{}, proposalID string) error {
	ids := productDraftSKUIds(data)
	productID, _ := data["product_id"].(string)
	var foreign bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM product_variants WHERE id=ANY($1::uuid[]) AND product_id<>$2) OR EXISTS(SELECT 1 FROM product_draft_sku_reservations d LEFT JOIN product_variants v ON v.id=d.sku_id WHERE d.sku_id=ANY($1::uuid[]) AND d.proposal_id<>$3 AND (v.id IS NULL OR v.product_id<>$2))`, pq.Array(ids), productID, proposalID).Scan(&foreign)
	if err != nil {
		return err
	}
	if foreign {
		return fmt.Errorf("SKU UUID is already used by another product or proposal")
	}
	return nil
}

// SyncProductDraftSKUsTx closes the concurrent-submission race using a UUID PK.
// Call after inserting the pending proposal and after accepted structure edits.
func SyncProductDraftSKUsTx(ctx context.Context, tx *sql.Tx, proposalID string, data map[string]interface{}) error {
	if !IsProductV2(data) {
		return nil
	}
	ids := productDraftSKUIds(data)
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_draft_sku_reservations WHERE proposal_id=$1 AND NOT(sku_id=ANY($2::uuid[]))`, proposalID, pq.Array(ids)); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO product_draft_sku_reservations(sku_id,proposal_id) SELECT unnest($1::uuid[]),$2 ON CONFLICT(sku_id) DO UPDATE SET proposal_id=EXCLUDED.proposal_id WHERE product_draft_sku_reservations.proposal_id=EXCLUDED.proposal_id`, pq.Array(ids), proposalID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != int64(len(ids)) {
		return fmt.Errorf("SKU UUID is reserved by another product proposal")
	}
	return nil
}
