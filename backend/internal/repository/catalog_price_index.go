// Package repository — PERF-01 public catalog price index.
//
// The index (migration 202611190002) is a cache of the authoritative Go
// pricing engine, never a second pricing implementation: one row per
// (store, product) with displayed public prices only (main price, original
// price, discount percent, active-variant price range). No supplier cost
// is ever stored. The ONLY writer is RefreshCatalogPricesTx, which prices
// with catalogpricing.ResolveMainPrice — the same engine the list handler
// uses for the served page. Deleting index rows only costs a rebuild.
//
// Freshness: input_fingerprint is a versioned md5 over the raw price
// inputs, computed by ONE SQL expression (priceFingerprintExpr) used both
// by the staleness probe and at refresh time, so a probe and a stored row
// can never disagree about what "fresh" means. A row is stale when its
// fingerprint differs, when it is missing, or when its valid_until
// boundary (campaign/widget/sale window) has passed. The scope revision
// (catalog_scope_revisions) is bumped only when a refresh actually changes
// a displayed price, so cursor bindings survive input edits that do not
// move prices.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/drup/platform/internal/catalogpage"
	"github.com/drup/platform/internal/catalogpricing"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/lib/pq"
)

// FingerprintVersion versions the fingerprint expression. Bump it whenever
// the priced inputs change shape: every stored row then reads as stale and
// rebuilds exactly once.
const FingerprintVersion = "v3-raw"

// catalogPriceRefreshBatch bounds one refresh round-trip: products are
// priced and upserted in batches of this size, never the whole stale set
// in one statement.
const catalogPriceRefreshBatch = 200

// CatalogPriceRow is one stored public-price row (public data only).
type CatalogPriceRow struct {
	StoreID       string
	ProductID     string
	MainPrice     int64
	OriginalPrice int64
	DiscountPct   int
	PriceMin      *int64
	PriceMax      *int64
	Fingerprint   string
	ValidUntil    *time.Time
}

// CatalogPricedPage is one selected page plus the filtered total. Keys
// are catalogpage.Key values carrying exactly the active sort's key
// fields (Price for cheapest/expensive, Discount for discount,
// Featured/CreatedAt for featured/newest, Position for store position),
// so the handler can encode the next cursor directly.
type CatalogPricedPage struct {
	Keys    []catalogpage.Key
	HasMore bool
	Total   int
}

// CatalogPriceBounds is the index-aggregate bounds result.
type CatalogPriceBounds struct {
	MinPrice int64
	MaxPrice int64
	Count    int
}

// BeginCatalogTx opens a snapshot transaction for one paged catalog read:
// probe, refresh and selection must see one consistent snapshot, so the
// isolation is REPEATABLE READ (the default ReadCommitted would let a
// concurrent edit slip between the probe and the selection).
func (r *CatalogRepository) BeginCatalogTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}

// GetCatalogScopeRevisionTx returns the store's cursor-binding revision,
// creating the row at revision 1 when absent. A revision below 1 can only
// come from a hand-written row; it is repaired to 1 in the same tx.
func (r *CatalogRepository) GetCatalogScopeRevisionTx(ctx context.Context, tx *sql.Tx, storeID string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_scope_revisions(store_id, revision) VALUES ($1, 1) ON CONFLICT (store_id) DO NOTHING`, storeID); err != nil {
		return 0, err
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM catalog_scope_revisions WHERE store_id = $1`, storeID).Scan(&revision); err != nil {
		return 0, err
	}
	if revision < 1 {
		if _, err := tx.ExecContext(ctx, `UPDATE catalog_scope_revisions SET revision = 1, updated_at = NOW() WHERE store_id = $1`, storeID); err != nil {
			return 0, err
		}
		return 1, nil
	}
	return revision, nil
}

// priceFingerprintJoins returns the LEFT JOIN clauses that feed
// priceFingerprintExpr. They reference the products alias "p" (all three
// call sites use it) and the store placeholder. v2 (Nov 19, 2026) replaced
// v1's per-product correlated scalar subqueries with pre-aggregated joins:
// v1 ran ~10 indexed probes per candidate row (~280ms at 10k); the joins
// collapse each input to one set-based pass (~one hash join per input).
// Every joined source is provably at most one row per product:
//   - platform_pct_products / platform_pct_categories /
//     product_margin_ceilings / store_product_overrides /
//     store_variant_defaults all carry a PRIMARY KEY on the join key
//     (verified in the migrations), so the LEFT JOIN cannot multiply rows.
//   - the variant / variant-override / lock / campaign sources are
//     grouped subqueries (GROUP BY product_id), one row per product.
//
// The campaign part is split into a store-scope scalar (one initplan, the
// same string for every product) and a grouped product/niche part, joined
// with '#' — a character no campaign field can contain — so the split is
// unambiguous. The lock part keeps v1's string_agg ORDER BY scope,
// scope_id over the same row set (product / niche / variant scopes).
func priceFingerprintJoins(storePlaceholder string, scoped bool) string {
	sp := storePlaceholder
	variantExpr := `jsonb_build_array(v.id,v.supplier_base_cost,v.is_default,to_jsonb(v)->'is_active',
		to_jsonb(v)->'inventory_mode',v.sale_price,v.sale_from,v.sale_to,v.variant_name,v.attributes,v.created_at,
		to_jsonb(v)->'min_cart_qty',to_jsonb(v)->'max_cart_qty',v.stock)::text`
	variantSet := `SELECT v.product_id, string_agg(` + variantExpr + `, ',' ORDER BY v.id) AS s FROM product_variants v GROUP BY v.product_id`
	if scoped {
		variantSet = `SELECT v.product_id, string_agg(` + variantExpr + `, ',' ORDER BY v.id) AS s FROM product_variants v LEFT JOIN store_variant_visibility svv ON svv.variant_id = v.id AND svv.store_id=` + sp + ` WHERE svv.is_visible IS DISTINCT FROM FALSE GROUP BY v.product_id`
	}
	campExpr := `c.id || ':' || c.scope || ':' || c.target_id || ':' || c.mode || ':' || c.value::text || ':' || c.starts_at::text || ':' || c.ends_at::text || ':' || c.is_active::text || ':' || c.created_at::text`
	joins := `
	LEFT JOIN platform_pct_products fp_ppp ON fp_ppp.product_id = p.id
	LEFT JOIN platform_pct_categories fp_ppc ON fp_ppc.category_slug = p.category_slug
	LEFT JOIN product_margin_ceilings fp_ceil ON fp_ceil.product_id = p.id
	LEFT JOIN store_product_overrides fp_po ON fp_po.store_id = ` + sp + ` AND fp_po.product_id = p.id
	LEFT JOIN store_variant_defaults fp_def ON fp_def.store_id = ` + sp + ` AND fp_def.product_id = p.id
	LEFT JOIN (` + variantSet + `) fp_var ON fp_var.product_id = p.id
	LEFT JOIN (SELECT product_id, string_agg(variant_id::text || ':' || override_value::text || ':' || mode, ',' ORDER BY variant_id) AS s FROM store_variant_overrides WHERE store_id = ` + sp + ` GROUP BY product_id) fp_vo ON fp_vo.product_id = p.id
	LEFT JOIN (SELECT u.product_id, string_agg(u.scope || ':' || u.scope_id || ':' || u.lock_mode || ':' || COALESCE(u.lock_value::text,''), ',' ORDER BY u.scope, u.scope_id) AS s FROM (
		SELECT l.scope, l.scope_id, l.lock_mode, l.lock_value, l.scope_id AS product_id FROM store_margin_locks l WHERE l.store_id = ` + sp + ` AND l.scope = 'product'
		UNION ALL
		SELECT l.scope, l.scope_id, l.lock_mode, l.lock_value, pn.product_id FROM store_margin_locks l JOIN product_niches pn ON pn.niche_id = l.scope_id WHERE l.store_id = ` + sp + ` AND l.scope = 'niche'
		UNION ALL
		SELECT l.scope, l.scope_id, l.lock_mode, l.lock_value, v.product_id FROM store_margin_locks l JOIN product_variants v ON v.id::text = l.scope_id WHERE l.store_id = ` + sp + ` AND l.scope = 'variant'
	) u GROUP BY u.product_id) fp_locks ON fp_locks.product_id = p.id
	LEFT JOIN (SELECT u.product_id, string_agg(u.s, ',' ORDER BY u.cid) AS s FROM (
		SELECT c.target_id AS product_id, c.id AS cid, ` + campExpr + ` AS s FROM seller_discount_campaigns c WHERE c.store_id = ` + sp + ` AND c.scope = 'product'
		UNION ALL
		SELECT pn.product_id, c.id AS cid, ` + campExpr + ` AS s FROM seller_discount_campaigns c JOIN product_niches pn ON pn.niche_id = c.target_id WHERE c.store_id = ` + sp + ` AND c.scope = 'niche'
		UNION ALL
		SELECT v.product_id, c.id AS cid, ` + campExpr + ` AS s FROM seller_discount_campaigns c JOIN product_variants v ON v.id::text = c.target_id WHERE c.store_id = ` + sp + ` AND c.scope = 'variant'
	) u GROUP BY u.product_id) fp_camp ON fp_camp.product_id = p.id`
	return joins
}

// priceFingerprintExpr returns a SQL expression evaluating to the
// versioned fingerprint of one product's price inputs for one store.
// storePlaceholder is the $n placeholder of the store id argument;
// productAlias is the products alias ("p"). The expression mirrors
// exactly what the Go engine reads (see ResolveReferencePricing,
// selectDropshipperMarginForProduct, ResolveVariantsAt, PickCampaign,
// DealsWidgetConfig.DiscountFor): product cost/original/category/active;
// the platform-pct ladder (product, category, global) and margin
// ceilings; the store's margin lock, custom override and widget config;
// the store's product override for this product; the visibility-filtered
// variants (cost, default, active, sale window); the store default;
// variant overrides; margin locks covering this product, its niches or
// its variants; and campaigns that can reach this product (store scope,
// this product, or one of its niches). Anything outside this list cannot
// move a displayed price, so it must not invalidate the index.
// The per-product parts come from priceFingerprintJoins (same query);
// the store-scope campaign string is a scalar initplan (one execution),
// and the settings/store rows are scalar initplans. The caller MUST
// include priceFingerprintJoins in the same query's FROM clause.
func priceFingerprintExpr(storePlaceholder, productAlias string, scoped bool) string {
	p := productAlias
	sp := storePlaceholder
	campExpr := `c.id || ':' || c.scope || ':' || c.target_id || ':' || c.mode || ':' || c.value::text || ':' || c.starts_at::text || ':' || c.ends_at::text || ':' || c.is_active::text || ':' || c.created_at::text`
	storeCamp := `COALESCE((SELECT string_agg(` + campExpr + `, ',' ORDER BY id) FROM seller_discount_campaigns c WHERE store_id=` + sp + ` AND scope='store'),'')`
	parts := []string{
		`COALESCE(` + p + `.supplier_base_cost::text,'')`,
		`COALESCE(` + p + `.original_price::text,'')`,
		`COALESCE(` + p + `.category_slug,'')`,
		`jsonb_build_array(` + p + `.is_active,` + p + `.stock,
			CASE WHEN to_jsonb(` + p + `)->>'product_type'='variable' THEN regexp_replace(` + p + `.title,'[[:space:]‌‍]','','g')='پیشفرض' ELSE FALSE END,
			to_jsonb(` + p + `)->'product_type',to_jsonb(` + p + `)->'schema_version')::text`,
		`COALESCE((SELECT platform_pct::text || '/' || central_profit_pct::text || '/' || max_profit_margin::text FROM platform_settings WHERE id = 1),'')`,
		`COALESCE(fp_ppp.pct::text,'')`,
		`COALESCE(fp_ppc.pct::text,'')`,
		`COALESCE(fp_ceil.max_margin_pct::text,'')`,
		`COALESCE((SELECT is_margin_locked_by_admin::text || '/' || COALESCE(custom_store_margin_override::text,'') || '/' || COALESCE(seller_deals_widget_config::text,'') FROM stores WHERE id=` + sp + `),'')`,
		`COALESCE(fp_po.override_value::text || '/' || fp_po.mode,'')`,
		`COALESCE(fp_var.s,'')`,
		`COALESCE(fp_def.variant_id::text,'')`,
		`COALESCE(fp_vo.s,'')`,
		`COALESCE(fp_locks.s,'')`,
		storeCamp + ` || '#' || COALESCE(fp_camp.s,'')`,
	}
	return `('` + FingerprintVersion + `:' || md5(` + strings.Join(parts, ` || '|' || `) + `))`
}

// priceFilterParts builds the FROM/WHERE of the filtered, scope-checked
// product set for one store. withIndex joins the price index (needed for
// price predicates, price ordering and bounds); the suspended-supplier
// clause is guarded inline with to_regclass so partial fixture databases
// without a suppliers table keep the pre-suspension shape, exactly as
// GetProducts degrades in Go. hasCategories gates the category-title
// search match (see buildSearchCondition). It also returns the search
// rank expression ("" when the filter has no search). The search
// predicate carries the pre-filter inline (see buildSearchCondition);
// there is no CTE: the MATERIALIZED-CTE fence was measured slower
// than the flat shape (see catalog_search.go) and was removed.
func priceFilterParts(scope catalogpage.Scope, filter catalogpage.Filter, withIndex bool, hasCategories bool) (from string, where []string, args []any, rankExpr string, err error) {
	if err := catalogpage.Validate(scope, filter, nil); err != nil {
		return "", nil, nil, "", err
	}
	arg := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	scoped := scope.OwnerType == "dropshipper"
	from = ` FROM products p JOIN stores scope_store ON scope_store.id=` + arg(scope.ID) + ` AND scope_store.owner_type=` + arg(scope.OwnerType)
	if scoped {
		from += ` JOIN store_products sp ON sp.store_id=scope_store.id AND sp.product_id=p.id`
	}
	if withIndex {
		from += ` JOIN catalog_public_prices idx ON idx.store_id=` + arg(scope.ID) + ` AND idx.product_id=p.id`
	}
	where = []string{`(to_regclass('public.suppliers') IS NULL OR ` + notSuspendedSupplierClause + `)`}
	// B1(a) D42 reversal: the is_active gate applies to every scope,
	// dropshipper assortments included — assortment membership no
	// longer resurrects a deactivated product on the public list.
	// Only the owner-gated IncludeInactive carve-out lifts it.
	if !filter.IncludeInactive {
		where = append(where, `p.is_active=TRUE`)
	}
	if filter.Category != "" && filter.Category != "all" {
		where = append(where, `p.category_slug=`+arg(filter.Category))
	}
	if filter.Niche != "" && filter.Niche != "all" {
		niche := arg(filter.Niche)
		where = append(where, `EXISTS(SELECT 1 FROM product_niches pn JOIN niches n ON n.id=pn.niche_id WHERE pn.product_id=p.id AND (n.slug=`+niche+` OR n.id=`+niche+`))`)
	}
	if len(filter.Brands) > 0 {
		where = append(where, `p.brand=ANY(`+arg(pq.Array(filter.Brands))+`::text[])`)
	}
	if filter.IsOriginal != nil {
		where = append(where, `p.is_original=`+arg(*filter.IsOriginal))
	}
	if filter.VisibleStockOnly {
		where = append(where, storefrontStockPredicate)
	} else if filter.InStockOnly {
		where = append(where, `p.stock>0`)
	}
	if filter.Search != "" {
		searchWhere, searchRank := buildSearchCondition(filter.Search, arg, hasCategories)
		where = append(where, searchWhere)
		rankExpr = searchRank
	}
	attributeIDs := make([]int, 0, len(filter.Attributes))
	for id := range filter.Attributes {
		attributeIDs = append(attributeIDs, id)
	}
	sort.Ints(attributeIDs)
	for _, id := range attributeIDs {
		values := filter.Attributes[id]
		if len(values) == 0 {
			continue
		}
		ids := make([]int64, len(values))
		for i, value := range values {
			ids[i] = int64(value)
		}
		where = append(where, `EXISTS(SELECT 1 FROM product_attribute_values av WHERE av.product_id=p.id AND av.dk_attribute_id=`+arg(id)+` AND av.dk_value_id=ANY(`+arg(pq.Array(ids))+`::bigint[]))`)
	}
	if withIndex {
		if filter.MinPrice != nil {
			where = append(where, `idx.main_price>=`+arg(*filter.MinPrice))
		}
		if filter.MaxPrice != nil {
			where = append(where, `idx.main_price<=`+arg(*filter.MaxPrice))
		}
	}
	return from, where, args, rankExpr, nil
}

// ProbeCatalogPricesTx returns the filtered total and the IDs whose index
// row is missing, fingerprint-stale, or past its time boundary — exactly
// the rows a refresh must rebuild before a price-sorted page or bounds
// answer can be trusted. One query; the stale list (not the catalog) is
// what comes back.
func (r *CatalogRepository) ProbeCatalogPricesTx(ctx context.Context, tx *sql.Tx, scope catalogpage.Scope, filter catalogpage.Filter) (total int, stale []string, err error) {
	if filter.Sort == "" {
		filter.Sort = "featured"
	}
	if filter.Search != "" {
		setSearchJITOff(ctx, tx)
	}
	from, where, args, _, err := priceFilterParts(scope, filter, false, categoriesTableExistsTx(ctx, tx))
	if err != nil {
		return 0, nil, err
	}
	args = append(args, scope.ID)
	fpStore := fmt.Sprintf("$%d", len(args))
	fp := priceFingerprintExpr(fpStore, "p", scope.OwnerType == "dropshipper")
	fpJoins := priceFingerprintJoins(fpStore, scope.OwnerType == "dropshipper")
	query := `WITH cand AS (SELECT p.id, ` + fp + ` AS fp` + from + fpJoins + ` WHERE ` + strings.Join(where, ` AND `) + `)
		SELECT (SELECT COUNT(*) FROM cand), c.id FROM cand c
		LEFT JOIN catalog_public_prices idx ON idx.store_id=` + fpStore + ` AND idx.product_id=c.id
		WHERE idx.product_id IS NULL OR idx.input_fingerprint <> c.fp OR (idx.valid_until IS NOT NULL AND idx.valid_until <= NOW())
		ORDER BY c.id`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	stale = []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&total, &id); err != nil {
			return 0, nil, err
		}
		stale = append(stale, id)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	if len(stale) == 0 {
		// No stale rows: the COUNT above never ran a row through the
		// scan loop, so read the total separately (one aggregate).
		countQuery := `SELECT COUNT(*) ` + from + ` WHERE ` + strings.Join(where, ` AND `)
		if err := tx.QueryRowContext(ctx, countQuery, args[:len(args)-1]...).Scan(&total); err != nil {
			return 0, nil, err
		}
	}
	return total, stale, nil
}

// ProbeCatalogPricesForIDsTx returns the subset of the given IDs whose
// index row is missing, fingerprint-stale, or past its time boundary.
// Used to warm the index for one non-price-sorted page (bounded by page
// size) without probing the whole filtered set.
func (r *CatalogRepository) ProbeCatalogPricesForIDsTx(ctx context.Context, tx *sql.Tx, storeID string, scoped bool, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}
	fp := priceFingerprintExpr("$2", "p", scoped)
	fpJoins := priceFingerprintJoins("$2", scoped)
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM products p
		LEFT JOIN catalog_public_prices idx ON idx.store_id=$2 AND idx.product_id=p.id`+fpJoins+`
		WHERE p.id = ANY($1) AND (idx.product_id IS NULL OR idx.input_fingerprint <> `+fp+` OR (idx.valid_until IS NOT NULL AND idx.valid_until <= NOW()))
		ORDER BY p.id`, pq.Array(ids), storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stale := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		stale = append(stale, id)
	}
	return stale, rows.Err()
}

// RefreshCatalogPricesTx rebuilds the index rows for the given product IDs
// in one snapshot transaction (the caller's tx): it loads the products,
// their store-filtered variants and defaults, prices each product with
// the Go engine via catalogpricing.ResolveMainPrice, and upserts the rows
// with freshly computed fingerprints. Products whose pricing errors lose
// their index row (the list handler skips them too). All price inputs
// come from inputs (CatalogScopeInputs, loaded in the same tx by
// LoadCatalogScopeInputsTx) — never from outside the snapshot, so a
// stored price can never disagree with its stored fingerprint. Returns
// whether any displayed price changed — only then does the scope revision
// bump, so cursor bindings survive price-neutral refreshes.
func (r *CatalogRepository) RefreshCatalogPricesTx(ctx context.Context, tx *sql.Tx, inputs *CatalogScopeInputs, productIDs []string, asOf time.Time) (changed bool, err error) {
	if inputs == nil || len(productIDs) == 0 || asOf.IsZero() {
		return false, nil
	}
	// PERF-03 G7 concurrency contract: this refresh runs inside the
	// caller's RepeatableRead snapshot tx, so two transactions
	// refreshing the same stale rows concurrently collide with 40001
	// "could not serialize access due to concurrent update" (the first
	// catalog burst on a cold index produced exactly 19x500, every one
	// a 40001, captured by the sql_error code log). The callers handle
	// that with ONE bounded retry of the whole snapshot tx (see
	// retryCatalogTxOnSerialization in cmd/server): the retry re-probes
	// against the winner's committed refresh and finds no stale rows.
	// An advisory lock was tried and REJECTED here: it serializes the
	// refresh execution but cannot refresh the loser's snapshot, which
	// was taken at the tx's first read — before the lock — so the
	// loser still fails with 40001 after waiting. Do not re-add it.
	store := inputs.Store
	settings := pricing.OrDefault(inputs.Settings)
	locks := inputs.Locks
	overrides := inputs.Overrides
	campaigns := inputs.Campaigns
	scoped := store.OwnerType == "dropshipper"
	if !scoped || store.Slug == "central" {
		campaigns = nil
	}
	widget := pricing.ParseDealsWidgetConfig(store.SellerDealsWidgetConfig)
	for start := 0; start < len(productIDs); start += catalogPriceRefreshBatch {
		end := start + catalogPriceRefreshBatch
		if end > len(productIDs) {
			end = len(productIDs)
		}
		batchChanged, err := r.refreshCatalogPriceBatchTx(ctx, tx, store, settings, locks, overrides, widget, campaigns, scoped, productIDs[start:end], asOf)
		if err != nil {
			return false, err
		}
		changed = changed || batchChanged
	}
	if changed {
		if _, err := tx.ExecContext(ctx, `UPDATE catalog_scope_revisions SET revision = revision + 1, updated_at = NOW() WHERE store_id = $1`, store.ID); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func (r *CatalogRepository) refreshCatalogPriceBatchTx(ctx context.Context, tx *sql.Tx, store domain.Store, settings *pricing.PlatformPricingSettings, locks []pricing.MarginLock, overrides map[string]pricing.StoreVariantOverride, widget *pricing.DealsWidgetConfig, campaigns []domain.SellerDiscountCampaign, scoped bool, batch []string, asOf time.Time) (bool, error) {
	products, err := r.catalogProductsByIDsTx(ctx, tx, batch)
	if err != nil {
		return false, err
	}
	variantsByProduct, err := r.catalogVariantsByIDsTx(ctx, tx, store.ID, scoped, batch)
	if err != nil {
		return false, err
	}
	defaults, err := r.catalogStoreDefaultsTx(ctx, tx, store.ID, batch)
	if err != nil {
		return false, err
	}
	fingerprints, err := r.catalogFingerprintsTx(ctx, tx, store.ID, scoped, batch)
	if err != nil {
		return false, err
	}
	existing, err := r.catalogExistingPricesTx(ctx, tx, store.ID, batch)
	if err != nil {
		return false, err
	}
	changed := false
	errorIDs := []string{}
	for _, p := range products {
		relevant := make([]domain.SellerDiscountCampaign, 0, len(campaigns))
		active := make([]domain.SellerDiscountCampaign, 0, len(campaigns))
		visibleVariants := make(map[string]bool, len(variantsByProduct[p.ID]))
		for _, v := range variantsByProduct[p.ID] {
			visibleVariants[v.ID] = true
		}
		for _, c := range campaigns {
			if c.Scope == "store" || (c.Scope == "variant" && visibleVariants[c.TargetID]) || (c.Scope == "product" && c.TargetID == p.ID) || (c.Scope == "niche" && containsString(p.NicheIDs, c.TargetID)) {
				relevant = append(relevant, c)
				if c.IsActive && !c.StartsAt.After(asOf) && c.EndsAt.After(asOf) {
					active = append(active, c)
				}
			}
		}
		main, err := catalogpricing.ResolveMainPrice(catalogpricing.MainPriceInput{
			Product: p, Variants: variantsByProduct[p.ID], Store: store,
			Settings: settings, Locks: locks, Overrides: overrides,
			ActiveCampaigns: active, AllCampaigns: relevant, Widget: widget,
			StoreDefaultVariantID: defaults[p.ID], UsePublicStock: true, AsOf: asOf,
		})
		if err != nil {
			errorIDs = append(errorIDs, p.ID)
			continue
		}
		old, ok := existing[p.ID]
		if !ok || old.MainPrice != main.Price || old.OriginalPrice != main.OriginalPrice || old.DiscountPct != main.DiscountPercent || !equalPricePtr(old.PriceMin, main.PriceMin) || !equalPricePtr(old.PriceMax, main.PriceMax) {
			changed = true
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_public_prices
			(store_id, product_id, main_price, original_price, discount_pct, price_min, price_max, input_fingerprint, valid_until, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (store_id, product_id) DO UPDATE SET
			main_price = EXCLUDED.main_price, original_price = EXCLUDED.original_price,
			discount_pct = EXCLUDED.discount_pct, price_min = EXCLUDED.price_min,
			price_max = EXCLUDED.price_max, input_fingerprint = EXCLUDED.input_fingerprint,
			valid_until = EXCLUDED.valid_until, updated_at = NOW()`,
			store.ID, p.ID, main.Price, main.OriginalPrice, main.DiscountPercent,
			nullablePrice(main.PriceMin), nullablePrice(main.PriceMax),
			fingerprints[p.ID], nullableTime(main.ValidUntil)); err != nil {
			return false, err
		}
	}
	// IDs in the batch with no product row (deleted) or a pricing error
	// must not keep a row: price sorts and bounds would otherwise serve
	// a product the list handler skips. Their absence is a change only
	// when a row actually existed.
	if len(errorIDs) > 0 {
		if err := r.deleteCatalogPricesTx(ctx, tx, store.ID, errorIDs); err != nil {
			return false, err
		}
		for _, id := range errorIDs {
			if _, ok := existing[id]; ok {
				changed = true
			}
		}
	}
	return changed, nil
}

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func equalPricePtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func nullablePrice(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return *v
}

func (r *CatalogRepository) deleteCatalogPricesTx(ctx context.Context, tx *sql.Tx, storeID string, ids []string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM catalog_public_prices WHERE store_id = $1 AND product_id = ANY($2)`, storeID, pq.Array(ids))
	return err
}

func (r *CatalogRepository) catalogExistingPricesTx(ctx context.Context, tx *sql.Tx, storeID string, ids []string) (map[string]CatalogPriceRow, error) {
	rows, err := tx.QueryContext(ctx, `SELECT product_id, main_price, original_price, discount_pct, price_min, price_max
		FROM catalog_public_prices WHERE store_id = $1 AND product_id = ANY($2)`, storeID, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]CatalogPriceRow{}
	for rows.Next() {
		var row CatalogPriceRow
		var priceMin, priceMax sql.NullInt64
		if err := rows.Scan(&row.ProductID, &row.MainPrice, &row.OriginalPrice, &row.DiscountPct, &priceMin, &priceMax); err != nil {
			return nil, err
		}
		if priceMin.Valid {
			v := priceMin.Int64
			row.PriceMin = &v
		}
		if priceMax.Valid {
			v := priceMax.Int64
			row.PriceMax = &v
		}
		out[row.ProductID] = row
	}
	return out, rows.Err()
}

func (r *CatalogRepository) catalogFingerprintsTx(ctx context.Context, tx *sql.Tx, storeID string, scoped bool, ids []string) (map[string]string, error) {
	fp := priceFingerprintExpr("$2", "p", scoped)
	fpJoins := priceFingerprintJoins("$2", scoped)
	rows, err := tx.QueryContext(ctx, `SELECT p.id, `+fp+` FROM products p`+fpJoins+` WHERE p.id = ANY($1)`, pq.Array(ids), storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, fingerprint string
		if err := rows.Scan(&id, &fingerprint); err != nil {
			return nil, err
		}
		out[id] = fingerprint
	}
	return out, rows.Err()
}

func (r *CatalogRepository) listStoreCampaignsTx(ctx context.Context, tx *sql.Tx, storeID string) ([]domain.SellerDiscountCampaign, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+team05CampaignColumns+` FROM seller_discount_campaigns WHERE store_id=$1 ORDER BY created_at ASC, id ASC`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SellerDiscountCampaign{}
	for rows.Next() {
		c, err := team05ScanCampaignRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// catalogProductsByIDsTx loads full product rows (same column list and
// scanner as GetProducts) for the refresh batch, in the caller's tx.
func (r *CatalogRepository) catalogProductsByIDsTx(ctx context.Context, tx *sql.Tx, ids []string) ([]domain.Product, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.title, p.slug, COALESCE(p.category_slug, ''), p.supplier_id, p.original_price,
		       p.supplier_base_cost, p.platform_margin, p.is_original, p.stock,
		       COALESCE(p.image_url, ''), COALESCE(p.description, ''), p.warehouse_name,
		       p.preparation_days, COALESCE(p.supplier_warranty, ''), p.is_featured, p.is_special_deal,
		       COALESCE(p.fulfillment_method, 'warehouse'),
		       COALESCE(p.supply_option, '1day'),
		       p.is_active, COALESCE(p.brand, ''), p.spn_number, p.specs,
		       p.length_mm, p.width_mm, p.height_mm, p.weight_grams,
		       COALESCE(to_jsonb(p)->>'product_type','legacy'), COALESCE((to_jsonb(p)->>'schema_version')::int,0), to_jsonb(p)->'attribute_schema',
		       COALESCE(ARRAY_AGG(pn.niche_id) FILTER (WHERE pn.niche_id IS NOT NULL), '{}') AS niche_ids,
		       COALESCE(p.shipping_class, 'standard'), COALESCE(p.tax_class, 'standard')
		FROM products p
		LEFT JOIN product_niches pn ON p.id = pn.product_id
		WHERE p.id = ANY($1)
		GROUP BY p.id
	`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var products []domain.Product
	for rows.Next() {
		var p domain.Product
		var nicheIDs pq.StringArray
		var specsRaw []byte
		if err := scanProductRow(rows, &p, &nicheIDs, &specsRaw); err != nil {
			return nil, fmt.Errorf("failed to scan product: %w", err)
		}
		p.NicheIDs = []string(nicheIDs)
		p.Specs = parseProductSpecs(specsRaw)
		products = append(products, p)
	}
	return products, rows.Err()
}

// catalogVariantsByIDsTx loads the price-relevant variant fields in repo
// order (default first, then created_at, id — the card pick order), with
// the same store-visibility filter the list handler applies. Images and
// physicals do not affect prices and are not loaded.
func (r *CatalogRepository) catalogVariantsByIDsTx(ctx context.Context, tx *sql.Tx, storeID string, scoped bool, ids []string) (map[string][]domain.ProductVariant, error) {
	query := `
		SELECT v.id, v.product_id, v.supplier_base_cost, v.is_default,
		       COALESCE((to_jsonb(v)->>'is_active')::boolean, TRUE),
		       COALESCE((to_jsonb(v)->>'inventory_mode')::text, 'legacy'),
		       v.sale_price, v.sale_from, v.sale_to, COALESCE(v.variant_name,''), COALESCE(v.attributes,'{}'::jsonb),
		       COALESCE((to_jsonb(v)->>'min_cart_qty')::int,1), (to_jsonb(v)->>'max_cart_qty')::int, v.stock
		FROM product_variants v
	`
	args := []any{pq.Array(ids)}
	if scoped {
		query += ` LEFT JOIN store_variant_visibility svv ON svv.variant_id = v.id AND svv.store_id = $2`
		args = append(args, storeID)
	}
	query += ` WHERE v.product_id = ANY($1)`
	if scoped {
		query += ` AND svv.is_visible IS DISTINCT FROM FALSE`
	}
	query += ` ORDER BY v.product_id, v.is_default DESC, v.created_at ASC, v.id ASC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]domain.ProductVariant{}
	for rows.Next() {
		var v domain.ProductVariant
		var cost sql.NullInt64
		var salePrice sql.NullInt64
		var saleFrom, saleTo sql.NullTime
		var attrs []byte
		var maxQty sql.NullInt32
		if err := rows.Scan(&v.ID, &v.ProductID, &cost, &v.IsDefault, &v.IsActive, &v.InventoryMode, &salePrice, &saleFrom, &saleTo, &v.Name, &attrs, &v.MinCartQty, &maxQty, &v.Stock); err != nil {
			return nil, err
		}
		v.Attributes = parseProductSpecs(attrs)
		if maxQty.Valid {
			n := int(maxQty.Int32)
			v.MaxCartQty = &n
		}
		if cost.Valid {
			c := cost.Int64
			v.SupplierBaseCost = &c
		}
		if salePrice.Valid {
			s := salePrice.Int64
			v.SalePrice = &s
		}
		if saleFrom.Valid {
			f := saleFrom.Time
			v.SaleFrom = &f
		}
		if saleTo.Valid {
			t := saleTo.Time
			v.SaleTo = &t
		}
		out[v.ProductID] = append(out[v.ProductID], v)
	}
	return out, rows.Err()
}

func (r *CatalogRepository) catalogStoreDefaultsTx(ctx context.Context, tx *sql.Tx, storeID string, ids []string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT product_id, variant_id FROM store_variant_defaults WHERE store_id = $1 AND product_id = ANY($2)`, storeID, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var productID, variantID string
		if err := rows.Scan(&productID, &variantID); err != nil {
			return nil, err
		}
		out[productID] = variantID
	}
	return out, rows.Err()
}

// GetPricedProductPageTx is the unified page selection for the public
// catalog. It selects at most N+1 keys in SQL and returns at most N,
// with the filtered total (stable across pages). Price sorts
// (cheapest/expensive/discount) order by the DISPLAYED main price from
// the fresh index — never by supplier cost — and price filters
// (MinPrice/MaxPrice) always join the index, whatever the sort. Callers
// must probe + refresh first (ProbeCatalogPricesTx +
// RefreshCatalogPricesTx); this method trusts the index rows it joins.
// Non-price sorts (featured/newest/store-position) mirror
// GetProductCandidatePageTx's ordering and cursor predicates exactly;
// the candidate method remains the tested primitive for that SQL, and
// this method is covered by the enumeration tests for every sort.
// StorePosition is derived by the caller (dropshipper + featured sort),
// never a client parameter.
func (r *CatalogRepository) GetPricedProductPageTx(ctx context.Context, tx *sql.Tx, scope catalogpage.Scope, filter catalogpage.Filter, size int, after *catalogpage.Key) (CatalogPricedPage, error) {
	page := CatalogPricedPage{Keys: []catalogpage.Key{}}
	if tx == nil || size < 1 || size > catalogpage.MaxSize {
		return page, catalogpage.ErrInvalid
	}
	if err := catalogpage.Validate(scope, filter, after); err != nil {
		return page, err
	}
	if filter.Sort == "" {
		filter.Sort = "featured"
	}
	switch filter.Sort {
	case "featured", "newest", "cheapest", "expensive", "discount":
	default:
		return page, catalogpage.ErrInvalid
	}
	scoped := scope.OwnerType == "dropshipper"
	if filter.StorePosition != (scoped && filter.Sort == "featured") {
		return page, catalogpage.ErrInvalid
	}
	pricedSort := filter.Sort == "cheapest" || filter.Sort == "expensive" || filter.Sort == "discount"
	withIndex := pricedSort || filter.MinPrice != nil || filter.MaxPrice != nil
	searching := filter.Search != ""
	if searching {
		setSearchJITOff(ctx, tx)
	}
	from, where, args, rankExpr, err := priceFilterParts(scope, filter, withIndex, categoriesTableExistsTx(ctx, tx))
	if err != nil {
		return page, err
	}
	position := `0`
	if scoped {
		// priceFilterParts already joins store_products for a
		// dropshipper scope; the position column is sp.position.
		position = `sp.position`
	}
	var order, selectCols string
	switch {
	case filter.StorePosition:
		order = position + ` ASC,p.id ASC`
		selectCols = `p.id,` + position
	case filter.Sort == "newest":
		order = `p.created_at DESC,p.id DESC`
		selectCols = `p.id,p.created_at`
	case filter.Sort == "cheapest":
		order, selectCols = `idx.main_price ASC, p.id ASC`, `p.id,idx.main_price,idx.discount_pct`
	case filter.Sort == "expensive":
		order, selectCols = `idx.main_price DESC, p.id ASC`, `p.id,idx.main_price,idx.discount_pct`
	case filter.Sort == "discount":
		order, selectCols = `idx.discount_pct DESC, p.id ASC`, `p.id,idx.main_price,idx.discount_pct`
	default: // featured
		order = `COALESCE(p.is_featured,FALSE) DESC,p.created_at DESC,p.id DESC`
		selectCols = `p.id,COALESCE(p.is_featured,FALSE),p.created_at`
	}
	// Batch 2, A1: on a search page the match rank leads the order
	// (title above brand/category above description-only), then the
	// existing order decides ties. Without a search the rank term is
	// omitted entirely, keeping non-search paging byte-identical.
	// Search pages run single-statement (total via COUNT(*) OVER()
	// on the pre-cursor set, after-key on the projected columns
	// outside the window) so the pre-filtered CTE + predicate run
	// once per page call instead of twice (COUNT + SELECT).
	if searching {
		return r.getPricedProductPageSearchTx(ctx, tx, scope, filter, size, after, from, where, args, rankExpr, position, pricedSort)
	}
	// The total is the FULL filtered count (stable across pages), so it
	// is computed BEFORE the after-key predicate is appended below.
	// Products whose pricing errors have no index row are not listable,
	// exactly as the handler skips them when rendering.
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) `+from+` WHERE `+strings.Join(where, ` AND `), args...).Scan(&page.Total); err != nil {
		return page, err
	}
	if after != nil {
		priceArg := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
		idArg := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
		var afterPred string
		switch {
		case filter.StorePosition:
			afterPred = `(` + position + `,p.id)>(` + priceArg(after.Position) + `,` + idArg(after.ID) + `)`
		case filter.Sort == "newest":
			afterPred = `(p.created_at,p.id)<(` + priceArg(after.CreatedAt) + `,` + idArg(after.ID) + `)`
		case filter.Sort == "cheapest":
			col := `idx.main_price`
			afterPred = `(` + col + ` > ` + priceArg(after.Price) + ` OR (` + col + ` = ` + priceArg(after.Price) + ` AND p.id > ` + idArg(after.ID) + `))`
		case filter.Sort == "expensive":
			col := `idx.main_price`
			afterPred = `(` + col + ` < ` + priceArg(after.Price) + ` OR (` + col + ` = ` + priceArg(after.Price) + ` AND p.id > ` + idArg(after.ID) + `))`
		case filter.Sort == "discount":
			col := `idx.discount_pct`
			afterPred = `(` + col + ` < ` + priceArg(after.Discount) + ` OR (` + col + ` = ` + priceArg(after.Discount) + ` AND p.id > ` + idArg(after.ID) + `))`
		default: // featured
			afterPred = `(COALESCE(p.is_featured,FALSE),p.created_at,p.id)<(` + priceArg(after.Featured) + `,` + priceArg(after.CreatedAt) + `,` + idArg(after.ID) + `)`
		}
		if searching {
			// Rank leads ASC while the rest of the tuple keeps its
			// own directions, so a single tuple comparison cannot
			// express the after-key: rank strictly greater, or equal
			// rank with the existing tuple predicate. Without this,
			// page 2+ of a search could repeat or skip rows across
			// a rank boundary.
			rankArg := priceArg(after.Rank)
			where = append(where, `(`+rankExpr+` > `+rankArg+` OR (`+rankExpr+` = `+rankArg+` AND `+afterPred+`))`)
		} else {
			where = append(where, afterPred)
		}
	}
	args = append(args, size+1)
	limitArg := fmt.Sprintf("$%d", len(args))
	rows, err := tx.QueryContext(ctx, `SELECT `+selectCols+from+` WHERE `+strings.Join(where, ` AND `)+` ORDER BY `+order+` LIMIT `+limitArg, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var key catalogpage.Key
		var rank int
		switch {
		case filter.StorePosition:
			var pos int64
			if err := scanRow(rows, searching, &key.ID, &pos, &rank); err != nil {
				return page, err
			}
			key.Position = pos
		case filter.Sort == "newest":
			var created time.Time
			if err := scanRow(rows, searching, &key.ID, &created, &rank); err != nil {
				return page, err
			}
			key.CreatedAt = created.UTC()
		case pricedSort:
			if err := scanRow(rows, searching, &key.ID, &key.Price, &key.Discount, &rank); err != nil {
				return page, err
			}
			if filter.Sort == "discount" {
				key.Price = 0
			} else {
				key.Discount = 0
			}
		default: // featured
			var featured bool
			var created time.Time
			if err := scanRow(rows, searching, &key.ID, &featured, &created, &rank); err != nil {
				return page, err
			}
			key.Featured = featured
			key.CreatedAt = created.UTC()
		}
		key.Rank = rank
		page.Keys = append(page.Keys, key)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	page.HasMore = len(page.Keys) > size
	if page.HasMore {
		page.Keys = page.Keys[:size]
	}
	return page, nil
}

// getPricedProductPageSearchTx is the single-statement search page for
// GetPricedProductPageTx (prefilter-soundness.md §3.2). The inner query
// (MATERIALIZED filtered CTE) selects the page keys, the rank
// (projected as a column — NOT computed once: rankExpr re-evaluates
// the title/brand/category chains, so WHERE + rank CASE = 2× per
// surviving row; there is no _n match-boolean CTE in this tree —
// see buildSearchCondition) and COUNT(*) OVER() as the total,
// over the filtered set BEFORE any after-key predicate. The outer query
// then applies the after-key predicate to the PROJECTED rank + sort
// columns and orders by rank + the existing sort. Because the window is
// evaluated inside the CTE and the after-key lives outside it, the total
// is the full filtered count, stable across pages (page 2 does not
// report a shrinking total). Empty result: the outer query returns zero
// rows and page.Total stays 0 (the window has no row to carry a total).
func (r *CatalogRepository) getPricedProductPageSearchTx(ctx context.Context, tx *sql.Tx, scope catalogpage.Scope, filter catalogpage.Filter, size int, after *catalogpage.Key, from string, where []string, args []any, rankExpr, position string, pricedSort bool) (CatalogPricedPage, error) {
	page := CatalogPricedPage{Keys: []catalogpage.Key{}}
	var aliasedSelect, aliasedOrder, outerCols string
	switch {
	case filter.StorePosition:
		aliasedSelect = `p.id AS id, ` + position + ` AS position`
		aliasedOrder = `position ASC, id ASC`
		outerCols = `id, position`
	case filter.Sort == "newest":
		aliasedSelect = `p.id AS id, p.created_at AS created_at`
		aliasedOrder = `created_at DESC, id DESC`
		outerCols = `id, created_at`
	case pricedSort:
		aliasedSelect = `p.id AS id, idx.main_price AS main_price, idx.discount_pct AS discount_pct`
		switch filter.Sort {
		case "cheapest":
			aliasedOrder = `main_price ASC, id ASC`
		case "expensive":
			aliasedOrder = `main_price DESC, id ASC`
		default: // discount
			aliasedOrder = `discount_pct DESC, id ASC`
		}
		outerCols = `id, main_price, discount_pct`
	default: // featured
		aliasedSelect = `p.id AS id, COALESCE(p.is_featured,FALSE) AS featured, p.created_at AS created_at`
		aliasedOrder = `featured DESC, created_at DESC, id DESC`
		outerCols = `id, featured, created_at`
	}
	inner := `WITH filtered AS MATERIALIZED (SELECT ` + aliasedSelect + `, ` + rankExpr + ` AS rank, COUNT(*) OVER() AS total` + from + ` WHERE ` + strings.Join(where, ` AND `) + `)`
	outerWhere := ""
	if after != nil {
		arg := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
		var tuplePred string
		switch {
		case filter.StorePosition:
			tuplePred = `(position,id)>(` + arg(after.Position) + `,` + arg(after.ID) + `)`
		case filter.Sort == "newest":
			tuplePred = `(created_at,id)<(` + arg(after.CreatedAt) + `,` + arg(after.ID) + `)`
		case filter.Sort == "cheapest":
			tuplePred = `(main_price > ` + arg(after.Price) + ` OR (main_price = ` + arg(after.Price) + ` AND id > ` + arg(after.ID) + `))`
		case filter.Sort == "expensive":
			tuplePred = `(main_price < ` + arg(after.Price) + ` OR (main_price = ` + arg(after.Price) + ` AND id > ` + arg(after.ID) + `))`
		case filter.Sort == "discount":
			tuplePred = `(discount_pct < ` + arg(after.Discount) + ` OR (discount_pct = ` + arg(after.Discount) + ` AND id > ` + arg(after.ID) + `))`
		default: // featured
			tuplePred = `(featured,created_at,id)<(` + arg(after.Featured) + `,` + arg(after.CreatedAt) + `,` + arg(after.ID) + `)`
		}
		// Rank leads ASC while the rest of the tuple keeps its own
		// directions: rank strictly greater, or equal rank with the
		// existing tuple predicate.
		outerWhere = ` WHERE (rank > ` + arg(after.Rank) + ` OR (rank = ` + arg(after.Rank) + ` AND ` + tuplePred + `))`
	}
	args = append(args, size+1)
	limitArg := fmt.Sprintf("$%d", len(args))
	rows, err := tx.QueryContext(ctx, inner+` SELECT `+outerCols+`, rank, total FROM filtered`+outerWhere+` ORDER BY rank ASC, `+aliasedOrder+` LIMIT `+limitArg, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var key catalogpage.Key
		var rank, total int
		switch {
		case filter.StorePosition:
			var pos int64
			if err := rows.Scan(&key.ID, &pos, &rank, &total); err != nil {
				return page, err
			}
			key.Position = pos
		case filter.Sort == "newest":
			var created time.Time
			if err := rows.Scan(&key.ID, &created, &rank, &total); err != nil {
				return page, err
			}
			key.CreatedAt = created.UTC()
		case pricedSort:
			if err := rows.Scan(&key.ID, &key.Price, &key.Discount, &rank, &total); err != nil {
				return page, err
			}
			if filter.Sort == "discount" {
				key.Price = 0
			} else {
				key.Discount = 0
			}
		default: // featured
			var featured bool
			var created time.Time
			if err := rows.Scan(&key.ID, &featured, &created, &rank, &total); err != nil {
				return page, err
			}
			key.Featured = featured
			key.CreatedAt = created.UTC()
		}
		key.Rank = rank
		page.Total = total
		page.Keys = append(page.Keys, key)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	page.HasMore = len(page.Keys) > size
	if page.HasMore {
		page.Keys = page.Keys[:size]
	}
	return page, nil
}

// CountCatalogCandidatesTx counts the filtered set for non-price sorts
// (same predicates as GetProductCandidatePageTx, no fingerprint, no
// index). It has NO live callers (test-only): the live envelope total
// comes from the window COUNT(*) OVER() in the single-statement page
// (getPricedProductPageSearchTx), not from this function.
func (r *CatalogRepository) CountCatalogCandidatesTx(ctx context.Context, tx *sql.Tx, scope catalogpage.Scope, filter catalogpage.Filter) (int, error) {
	if filter.Search != "" {
		setSearchJITOff(ctx, tx)
	}
	from, where, args, _, err := priceFilterParts(scope, filter, false, categoriesTableExistsTx(ctx, tx))
	if err != nil {
		return 0, err
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) `+from+` WHERE `+strings.Join(where, ` AND `), args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

// GetCatalogPriceBoundsTx aggregates MIN/MAX/COUNT of displayed main
// prices over the filtered set from the fresh index. Callers must probe +
// refresh first. Products with no index row (pricing errors) are excluded
// from all three numbers, exactly as computePriceBounds excludes them.
func (r *CatalogRepository) GetCatalogPriceBoundsTx(ctx context.Context, tx *sql.Tx, scope catalogpage.Scope, filter catalogpage.Filter) (CatalogPriceBounds, error) {
	var bounds CatalogPriceBounds
	if filter.Search != "" {
		setSearchJITOff(ctx, tx)
	}
	from, where, args, _, err := priceFilterParts(scope, filter, true, categoriesTableExistsTx(ctx, tx))
	if err != nil {
		return bounds, err
	}
	var minPrice, maxPrice sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MIN(idx.main_price), MAX(idx.main_price), COUNT(*) `+from+` WHERE `+strings.Join(where, ` AND `), args...).Scan(&minPrice, &maxPrice, &bounds.Count); err != nil {
		return bounds, err
	}
	if minPrice.Valid {
		bounds.MinPrice = minPrice.Int64
	}
	if maxPrice.Valid {
		bounds.MaxPrice = maxPrice.Int64
	}
	return bounds, nil
}

// ListStoreCategorySlugs returns the distinct category slugs of the
// products one store scope can list (assortment for a dropshipper,
// active catalog for central), for the storefront category cache. One
// query; it replaces the previous whole-catalog fetch in StoreContext.
func (r *CatalogRepository) ListStoreCategorySlugs(ctx context.Context, storeID, ownerType string) ([]string, error) {
	return r.listStoreCategorySlugsQ(ctx, r.db, storeID, ownerType)
}

// ListStoreCategorySlugsTx is the same query inside the caller's tx
// (used by tests running against a fixture schema).
func (r *CatalogRepository) ListStoreCategorySlugsTx(ctx context.Context, tx *sql.Tx, storeID, ownerType string) ([]string, error) {
	return r.listStoreCategorySlugsQ(ctx, tx, storeID, ownerType)
}

type catalogQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (r *CatalogRepository) listStoreCategorySlugsQ(ctx context.Context, q catalogQuerier, storeID, ownerType string) ([]string, error) {
	scoped := ownerType == "dropshipper"
	from := ` FROM products p`
	if scoped {
		from += ` JOIN store_products sp ON sp.store_id=$1 AND sp.product_id=p.id`
	}
	where := []string{`(to_regclass('public.suppliers') IS NULL OR ` + notSuspendedSupplierClause + `)`}
	// The unscoped query has no placeholder: passing the store ID
	// anyway fails in pq ("got 1 parameters but the statement
	// requires 0").
	var args []any
	if scoped {
		args = []any{storeID}
	}
	// B1(a) D42 reversal: the category slugs of a scope are the
	// categories of its listable products — deactivated assortment
	// products contribute no category. (This query has no filter, so
	// there is no IncludeInactive carve-out here; seller management
	// reads categories through the list endpoint.)
	where = append(where, `p.is_active=TRUE`)
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT p.category_slug`+from+` WHERE `+strings.Join(where, ` AND `)+` AND p.category_slug <> '' ORDER BY p.category_slug`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}
