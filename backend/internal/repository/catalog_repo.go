package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/lib/pq"
)

// CatalogRepository handles data queries for products, categories, and niches
type CatalogRepository struct {
	db *sql.DB
}

func NewCatalogRepository(db *sql.DB) *CatalogRepository {
	return &CatalogRepository{db: db}
}

// ProductFilter defines filtering parameters for catalog search and listing.
// Brand is an EXACT match (D41), never partial. List queries always
// restrict to active products (D42); detail queries do not.
type ProductFilter struct {
	CategorySlug string
	NicheSlug    string
	Brand        string
	IsOriginal   *bool
	InStockOnly  bool
	Search       string
	SortBy       string // 'featured' | 'cheapest' | 'expensive' | 'discount'
}

// GetProducts returns active reference products matching the filter with their assigned niche IDs
func (r *CatalogRepository) GetProducts(ctx context.Context, filter ProductFilter) ([]domain.Product, error) {
	var whereClauses []string
	var args []interface{}
	argIdx := 1

	if filter.CategorySlug != "" && filter.CategorySlug != "all" {
		whereClauses = append(whereClauses, fmt.Sprintf("p.category_slug = $%d", argIdx))
		args = append(args, filter.CategorySlug)
		argIdx++
	}

	if filter.IsOriginal != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("p.is_original = $%d", argIdx))
		args = append(args, *filter.IsOriginal)
		argIdx++
	}

	if filter.InStockOnly {
		whereClauses = append(whereClauses, "p.stock > 0")
	}

	// D42 list-only: the public list returns active products only.
	// Detail (GetProductByID/GetProductBySpn) deliberately has no
	// such condition: live returns 200 for inactive detail.
	whereClauses = append(whereClauses, "p.is_active = TRUE")

	// D41 brand filter: EXACT match, never partial.
	if filter.Brand != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("p.brand = $%d", argIdx))
		args = append(args, filter.Brand)
		argIdx++
	}

	if filter.Search != "" {
		searchTerm := "%" + strings.ToLower(filter.Search) + "%"
		whereClauses = append(whereClauses, fmt.Sprintf("(LOWER(p.title) LIKE $%d OR LOWER(p.description) LIKE $%d)", argIdx, argIdx))
		args = append(args, searchTerm)
		argIdx++
	}

	if filter.NicheSlug != "" && filter.NicheSlug != "all" {
		whereClauses = append(whereClauses, fmt.Sprintf("p.id IN (SELECT pn.product_id FROM product_niches pn JOIN niches n ON pn.niche_id = n.id WHERE n.slug = $%d OR n.id = $%d)", argIdx, argIdx))
		args = append(args, filter.NicheSlug)
		argIdx++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " WHERE " + strings.Join(whereClauses, " AND ")
	}

	orderBySQL := " ORDER BY p.is_featured DESC, p.created_at DESC"
	switch filter.SortBy {
	case "cheapest":
		orderBySQL = " ORDER BY p.supplier_base_cost ASC"
	case "expensive":
		orderBySQL = " ORDER BY p.supplier_base_cost DESC"
	case "discount":
		orderBySQL = " ORDER BY (p.original_price - p.supplier_base_cost) DESC"
	}

	query := fmt.Sprintf(`
		SELECT p.id, p.title, p.slug, p.category_slug, p.supplier_id, p.original_price,
		       p.supplier_base_cost, p.platform_margin, p.is_original, p.stock,
		       COALESCE(p.image_url, ''), COALESCE(p.description, ''), p.warehouse_name,
		       p.preparation_days, COALESCE(p.supplier_warranty, ''), p.is_featured, p.is_special_deal,
		       COALESCE(p.fulfillment_method, 'warehouse'),
		       COALESCE(p.supply_option, '1day'),
		       p.is_active, COALESCE(p.brand, ''), p.spn_number, p.specs,
		       p.length_mm, p.width_mm, p.height_mm, p.weight_grams,
		       COALESCE(ARRAY_AGG(pn.niche_id) FILTER (WHERE pn.niche_id IS NOT NULL), '{}') AS niche_ids
		FROM products p
		LEFT JOIN product_niches pn ON p.id = pn.product_id
		%s
		GROUP BY p.id
		%s
	`, whereSQL, orderBySQL)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query products: %w", err)
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

	return products, nil
}

// GetProductByID retrieves a single product by ID or Slug with associated niche IDs
func (r *CatalogRepository) GetProductByID(ctx context.Context, idOrSlug string) (*domain.Product, error) {
	query := `
		SELECT p.id, p.title, p.slug, p.category_slug, p.supplier_id, p.original_price,
		       p.supplier_base_cost, p.platform_margin, p.is_original, p.stock,
		       COALESCE(p.image_url, ''), COALESCE(p.description, ''), p.warehouse_name,
		       p.preparation_days, COALESCE(p.supplier_warranty, ''), p.is_featured, p.is_special_deal,
		       COALESCE(p.fulfillment_method, 'warehouse'),
		       COALESCE(p.supply_option, '1day'),
		       p.is_active, COALESCE(p.brand, ''), p.spn_number, p.specs,
		       p.length_mm, p.width_mm, p.height_mm, p.weight_grams,
		       COALESCE(ARRAY_AGG(pn.niche_id) FILTER (WHERE pn.niche_id IS NOT NULL), '{}') AS niche_ids
		FROM products p
		LEFT JOIN product_niches pn ON p.id = pn.product_id
		WHERE p.id = $1 OR p.slug = $1
		GROUP BY p.id
	`
	row := r.db.QueryRowContext(ctx, query, idOrSlug)

	var p domain.Product
	var nicheIDs pq.StringArray
	var specsRaw []byte
	if err := scanProductRow(row, &p, &nicheIDs, &specsRaw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query product by ID: %w", err)
	}
	p.NicheIDs = []string(nicheIDs)
	p.Specs = parseProductSpecs(specsRaw)
	return &p, nil
}

// GetCategoryProductCounts returns, for every category id, the count
// of ACTIVE products directly assigned to that category (products
// whose category_slug equals the category's own slug). FIX 12: live
// shows "موبایل" as ۲ کالا while the subtree count gave ۴ (mobile 2 +
// child "گوشی موبایل" 2); every other top-level count matched either
// way, so the direct count is the live definition.
func (r *CatalogRepository) GetCategoryProductCounts(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.id, COUNT(p.id) FROM categories c
		LEFT JOIN products p ON p.category_slug = c.slug AND p.is_active = TRUE
		GROUP BY c.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
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

// GetSupplierNameByID returns the public supplier name for a product's
// supplier id, or "" when unknown (FIX 4, live store-view parity).
func (r *CatalogRepository) GetSupplierNameByID(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	var name string
	err := r.db.QueryRowContext(ctx, `SELECT name FROM suppliers WHERE id = $1`, id).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// GetCategories returns ALL categories (D39): the live list has
// all 3,184 rows with exactly the 7 keys id/title/slug/iconName/
// gradient/imageUrl/parentId, INCLUDING the inactive cat-gold.
// TODO: whether the public list should eventually filter inactive
// categories is an open decision (live does not filter); this
// implementation follows confirmed live behaviour and does not
// silently filter. depth/sortOrder/children/isActive are NOT
// returned, matching live.
func (r *CatalogRepository) GetCategories(ctx context.Context) ([]domain.Category, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, title, slug, icon_name, COALESCE(gradient, ''), COALESCE(image_url, ''), parent_id FROM categories ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []domain.Category
	for rows.Next() {
		var c domain.Category
		var parentID sql.NullString
		if err := rows.Scan(&c.ID, &c.Title, &c.Slug, &c.IconName, &c.Gradient, &c.ImageURL, &parentID); err != nil {
			return nil, err
		}
		if parentID.Valid {
			p := parentID.String
			c.ParentID = &p
		}
		categories = append(categories, c)
	}
	if counts, err := r.GetCategoryProductCounts(ctx); err == nil {
		for i := range categories {
			categories[i].ProductCount = counts[categories[i].ID]
		}
	}
	return categories, nil
}

// GetNiches returns the niches in the DB, optionally filtered by
// categorySlug (D44: the DB is the source of truth, not any
// hardcoded official list). The niche-auto -> digital mapping
// suspicion is left as-is (TODO, see Team 04 report).
func (r *CatalogRepository) GetNiches(ctx context.Context, categorySlug string) ([]domain.Niche, error) {
	query := `SELECT id, title, slug, category_slug, COALESCE(badge, ''), COALESCE(description, '') FROM niches`
	var rows *sql.Rows
	var err error

	if categorySlug != "" && categorySlug != "all" {
		query += " WHERE category_slug = $1 ORDER BY id ASC"
		rows, err = r.db.QueryContext(ctx, query, categorySlug)
	} else {
		query += " ORDER BY id ASC"
		rows, err = r.db.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var niches []domain.Niche
	for rows.Next() {
		var n domain.Niche
		if err := rows.Scan(&n.ID, &n.Title, &n.Slug, &n.CategorySlug, &n.Badge, &n.Description); err != nil {
			return nil, err
		}
		niches = append(niches, n)
	}
	return niches, nil
}

// ---------------------------------------------------------------------------
// Team 04 live parity additions to CatalogRepository (D05/D28/D39/D40/
// D41/D42/D43/D46/D61). Appended by the Team 04 patch; the pre-existing
// methods above are edited only for the brand/is_active/columns changes
// noted in their own comments.
// ---------------------------------------------------------------------------

// productRowScanner is satisfied by both *sql.Row and *sql.Rows, so the
// list and detail queries share one scan order.
type productRowScanner interface {
	Scan(dest ...any) error
}

// scanProductRow scans one product row in the shared column order used
// by GetProducts and GetProductByID: the source columns, then
// is_active, brand, spn_number, specs, dimensions, then niche_ids.
// Specs arrive as raw JSONB and are decoded by the caller.
func scanProductRow(scanner productRowScanner, p *domain.Product, nicheIDs *pq.StringArray, specsRaw *[]byte) error {
	return scanner.Scan(
		&p.ID, &p.Title, &p.Slug, &p.CategorySlug, &p.SupplierID, &p.OriginalPrice,
		&p.SupplierBaseCost, &p.PlatformMargin, &p.IsOriginal, &p.Stock,
		&p.ImageURL, &p.Description, &p.WarehouseName,
		&p.PreparationDays, &p.SupplierWarranty, &p.IsFeatured, &p.IsSpecialDeal,
		&p.FulfillmentMethod, &p.SupplyOption,
		&p.IsActive, &p.Brand, &p.SpnNumber, specsRaw,
		&p.LengthMM, &p.WidthMM, &p.HeightMM, &p.WeightGrams,
		nicheIDs,
	)
}

// parseProductSpecs decodes a specs JSONB value; a NULL, empty, or
// invalid value yields an empty object, never a null in the response.
func parseProductSpecs(raw []byte) map[string]interface{} {
	out := map[string]interface{}{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]interface{}{}
	}
	return out
}

// GetVariantsByProductIDs returns variants for many products in one
// query (live binary symbol GetVariantsByProductIDs), ordered
// deterministically: defaults first, then oldest first, then id. The
// ordering is what DefaultVariantForResponse relies on (TODO U32 for
// the live fallback when no default exists).
func (r *CatalogRepository) GetVariantsByProductIDs(ctx context.Context, productIDs []string) (map[string][]domain.ProductVariant, error) {
	out := map[string][]domain.ProductVariant{}
	if len(productIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT v.id, v.product_id, v.spnc, v.spnc_number, v.variant_name,
		       v.attributes, v.is_default, v.stock, v.supplier_base_cost
		FROM product_variants v
		WHERE v.product_id = ANY($1)
		ORDER BY v.product_id, v.is_default DESC, v.created_at ASC, v.id ASC
	`, pq.Array(productIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.ProductVariant
		var attrsRaw []byte
		var spncNumber sql.NullInt32
		var cost sql.NullInt64
		if err := rows.Scan(&v.ID, &v.ProductID, &v.Spnc, &spncNumber, &v.Name, &attrsRaw, &v.IsDefault, &v.Stock, &cost); err != nil {
			return nil, err
		}
		if spncNumber.Valid {
			n := int(spncNumber.Int32)
			v.SpncNumber = &n
		}
		if cost.Valid {
			c := cost.Int64
			v.SupplierBaseCost = &c
		}
		v.Attributes = parseProductSpecs(attrsRaw)
		out[v.ProductID] = append(out[v.ProductID], v)
	}
	return out, rows.Err()
}

// GetProductVariants returns one product's variants in repo order.
func (r *CatalogRepository) GetProductVariants(ctx context.Context, productID string) ([]domain.ProductVariant, error) {
	byProduct, err := r.GetVariantsByProductIDs(ctx, []string{productID})
	if err != nil {
		return nil, err
	}
	variants := byProduct[productID]
	if variants == nil {
		variants = []domain.ProductVariant{}
	}
	return variants, nil
}

// GetProductBySpn resolves a product by its public spn_number (D46
// decision: implement the server lookup so shared spn- links work).
// It deliberately has NO is_active condition: detail of an inactive
// product returns 200 in live (D42, live-confirmed-200).
func (r *CatalogRepository) GetProductBySpn(ctx context.Context, spn int64) (*domain.Product, error) {
	query := `
		SELECT p.id, p.title, p.slug, p.category_slug, p.supplier_id, p.original_price,
		       p.supplier_base_cost, p.platform_margin, p.is_original, p.stock,
		       COALESCE(p.image_url, ''), COALESCE(p.description, ''), p.warehouse_name,
		       p.preparation_days, COALESCE(p.supplier_warranty, ''), p.is_featured, p.is_special_deal,
		       COALESCE(p.fulfillment_method, 'warehouse'),
		       COALESCE(p.supply_option, '1day'),
		       p.is_active, COALESCE(p.brand, ''), p.spn_number, p.specs,
		       p.length_mm, p.width_mm, p.height_mm, p.weight_grams,
		       COALESCE(ARRAY_AGG(pn.niche_id) FILTER (WHERE pn.niche_id IS NOT NULL), '{}') AS niche_ids
		FROM products p
		LEFT JOIN product_niches pn ON p.id = pn.product_id
		WHERE p.spn_number = $1
		GROUP BY p.id
	`
	row := r.db.QueryRowContext(ctx, query, spn)
	var p domain.Product
	var nicheIDs pq.StringArray
	var specsRaw []byte
	if err := scanProductRow(row, &p, &nicheIDs, &specsRaw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query product by spn: %w", err)
	}
	p.NicheIDs = []string(nicheIDs)
	p.Specs = parseProductSpecs(specsRaw)
	return &p, nil
}

// GetProductImages returns a product's gallery in sort_order order
// (specs/gallery shared gap: expose the DB gallery in detail).
func (r *CatalogRepository) GetProductImages(ctx context.Context, productID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT image_url FROM product_images
		WHERE product_id = $1
		ORDER BY sort_order ASC, created_at ASC, id ASC
	`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	images := []string{}
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, err
		}
		images = append(images, url)
	}
	return images, rows.Err()
}

// GetProductStoreCount counts distinct stores carrying a product in
// store_products (D21 detail storeCount). The live semantics of
// storeCount are unproven beyond the single observed value 0 for
// prod-1793551155965; this definition (distinct store_products rows)
// is the documented reading, not a proven live formula.
func (r *CatalogRepository) GetProductStoreCount(ctx context.Context, productID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT store_id) FROM store_products WHERE product_id = $1
	`, productID).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// EnsureSearchLogTable creates search_queries if missing and adds the
// catalog_result_count column if missing (D43; mirrors migration
// 202610310009, whose comment documents this runtime ADD COLUMN).
func (r *CatalogRepository) EnsureSearchLogTable(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS search_queries (
			id BIGSERIAL PRIMARY KEY,
			term_normalized TEXT NOT NULL,
			store_slug TEXT NOT NULL,
			result_count INTEGER NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `ALTER TABLE search_queries ADD COLUMN IF NOT EXISTS catalog_result_count INTEGER`)
	return err
}

// LogSearchEvent records one search (D43, live binary symbol
// logSearchEvent). An empty normalised term is not logged. Callers
// treat a logging error as best-effort: it must never fail a listing.
func (r *CatalogRepository) LogSearchEvent(ctx context.Context, termNormalized, storeSlug string, resultCount, catalogResultCount int) error {
	if termNormalized == "" {
		return nil
	}
	if err := r.EnsureSearchLogTable(ctx); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO search_queries (term_normalized, store_slug, result_count, catalog_result_count)
		VALUES ($1, $2, $3, $4)
	`, termNormalized, storeSlug, resultCount, catalogResultCount)
	return err
}

// AggregateSearchQueriesDaily aggregates search_queries by Asia/Tehran
// calendar day, following the bucketing rule in migration 202610220003.
// It only AGGREGATES: it writes nowhere (see RollupSearchQueries).
func (r *CatalogRepository) AggregateSearchQueriesDaily(ctx context.Context) ([]domain.SearchDailyAggregate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT term_normalized, store_slug,
		       (created_at AT TIME ZONE 'Asia/Tehran')::date AS day,
		       COUNT(*),
		       COALESCE(SUM(CASE WHEN result_count = 0 THEN 1 ELSE 0 END), 0)
		FROM search_queries
		GROUP BY term_normalized, store_slug, day
		ORDER BY day ASC, term_normalized ASC, store_slug ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SearchDailyAggregate{}
	for rows.Next() {
		var a domain.SearchDailyAggregate
		if err := rows.Scan(&a.TermNormalized, &a.StoreSlug, &a.Day, &a.Searches, &a.ZeroResultSearches); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RollupSearchQueries is the repository entry point named by migration
// 202610220003 and by the live binary symbol RollupSearchQueries.
// TODO U73: the rollup DESTINATION is unverified (live
// search_term_daily holds 0 rows and Team 09 left the hourly worker
// tick a documented no-op), so this function returns the aggregates
// WITHOUT persisting them. Wiring a write destination belongs to the
// supervisor once U73 is resolved; do not assume search_term_daily is
// the hourly destination.
func (r *CatalogRepository) RollupSearchQueries(ctx context.Context) ([]domain.SearchDailyAggregate, error) {
	return r.AggregateSearchQueriesDaily(ctx)
}

// GetBrands returns every distinct Digikala brand in dk_category_brands
// (D40, new API shape: live 404ed here). Brands are reference-only
// suggestion data, not merged into products.brand.
func (r *CatalogRepository) GetBrands(ctx context.Context) ([]domain.CategoryBrand, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT ON (dk_brand_id) dk_brand_id, COALESCE(code, ''), title_fa, COALESCE(title_en, '')
		FROM dk_category_brands
		ORDER BY dk_brand_id ASC, title_fa ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	brands := []domain.CategoryBrand{}
	for rows.Next() {
		var b domain.CategoryBrand
		if err := rows.Scan(&b.ID, &b.Code, &b.TitleFa, &b.TitleEn); err != nil {
			return nil, err
		}
		brands = append(brands, b)
	}
	return brands, rows.Err()
}

// GetCategoryBrandsBySlug returns a category's Digikala brands (D40),
// matched by slug or id. An unknown category yields an empty list, not
// an error: the route exists, the category simply has no facet data.
func (r *CatalogRepository) GetCategoryBrandsBySlug(ctx context.Context, slugOrID string) ([]domain.CategoryBrand, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT b.dk_brand_id, COALESCE(b.code, ''), b.title_fa, COALESCE(b.title_en, '')
		FROM dk_category_brands b
		JOIN categories c ON c.id = b.category_id
		WHERE c.slug = $1 OR c.id = $1
		ORDER BY b.title_fa ASC
	`, slugOrID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	brands := []domain.CategoryBrand{}
	for rows.Next() {
		var b domain.CategoryBrand
		if err := rows.Scan(&b.ID, &b.Code, &b.TitleFa, &b.TitleEn); err != nil {
			return nil, err
		}
		brands = append(brands, b)
	}
	return brands, rows.Err()
}

// GetCategoryAttributesBySlug returns a category's OWN Digikala
// attributes with their per-category values (D40). No inherited or
// descendant union is computed: the migration comment for
// 202610280011 describes such a union as a design idea, not a proven
// live behaviour, so it is not implemented here.
func (r *CatalogRepository) GetCategoryAttributesBySlug(ctx context.Context, slugOrID string) ([]domain.DkAttribute, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.dk_attr_id, a.title, a.type
		FROM dk_category_attributes ca
		JOIN dk_attributes a ON a.dk_attr_id = ca.dk_attr_id
		JOIN categories c ON c.id = ca.category_id
		WHERE c.slug = $1 OR c.id = $1
		ORDER BY a.dk_attr_id ASC
	`, slugOrID)
	if err != nil {
		return nil, err
	}
	attrs := []domain.DkAttribute{}
	byID := map[int]int{}
	for rows.Next() {
		var a domain.DkAttribute
		if err := rows.Scan(&a.ID, &a.Title, &a.Type); err != nil {
			rows.Close()
			return nil, err
		}
		a.Values = []domain.DkAttributeValue{}
		byID[a.ID] = len(attrs)
		attrs = append(attrs, a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(attrs) == 0 {
		return attrs, nil
	}
	valueRows, err := r.db.QueryContext(ctx, `
		SELECT cav.dk_attr_id, v.dk_value_id, v.title_fa, COALESCE(v.title_en, '')
		FROM dk_category_attribute_values cav
		JOIN dk_attribute_values v
		  ON v.dk_attr_id = cav.dk_attr_id AND v.dk_value_id = cav.dk_value_id
		JOIN categories c ON c.id = cav.category_id
		WHERE c.slug = $1 OR c.id = $1
		ORDER BY cav.dk_attr_id ASC, v.dk_value_id ASC
	`, slugOrID)
	if err != nil {
		return nil, err
	}
	defer valueRows.Close()
	for valueRows.Next() {
		var attrID int
		var v domain.DkAttributeValue
		if err := valueRows.Scan(&attrID, &v.ID, &v.TitleFa, &v.TitleEn); err != nil {
			return nil, err
		}
		if idx, ok := byID[attrID]; ok {
			attrs[idx].Values = append(attrs[idx].Values, v)
		}
	}
	return attrs, valueRows.Err()
}

// FIX 51 — live category-attributes editor (ql): colors and brands for a
// category, from the Digikala tables. JSON keys match live's ql shape.
type DkCategoryColor struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	HexCode string `json:"hexCode"`
}


func (r *CatalogRepository) GetCategoryColorsBySlug(ctx context.Context, slugOrID string) ([]DkCategoryColor, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT cc.dk_color_id, cc.title, COALESCE(cc.hex_code, '')
		FROM dk_category_colors cc
		JOIN categories c ON c.id = cc.category_id
		WHERE c.slug = $1 OR c.id = $1
		ORDER BY cc.dk_color_id ASC
	`, slugOrID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DkCategoryColor{}
	for rows.Next() {
		var col DkCategoryColor
		if err := rows.Scan(&col.ID, &col.Title, &col.HexCode); err != nil {
			return nil, err
		}
		out = append(out, col)
	}
	return out, rows.Err()
}

// GetCategoryAttributesInherited returns the attributes of a category's
// descendant categories (live's ql "inherited" branch: shown when the
// category itself has no attributes of its own).
func (r *CatalogRepository) GetCategoryAttributesInherited(ctx context.Context, slugOrID string) ([]domain.DkAttribute, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM categories WHERE slug = $1 OR id = $1
			UNION ALL
			SELECT c.id FROM categories c JOIN sub s ON c.parent_id = s.id
		)
		SELECT DISTINCT a.dk_attr_id, a.title, a.type
		FROM dk_category_attributes ca
		JOIN dk_attributes a ON a.dk_attr_id = ca.dk_attr_id
		WHERE ca.category_id IN (SELECT id FROM sub)
		  AND ca.category_id NOT IN (SELECT id FROM categories WHERE slug = $1 OR id = $1)
		ORDER BY a.dk_attr_id ASC
	`, slugOrID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attrs := []domain.DkAttribute{}
	byID := map[int]int{}
	for rows.Next() {
		var a domain.DkAttribute
		if err := rows.Scan(&a.ID, &a.Title, &a.Type); err != nil {
			return nil, err
		}
		a.Values = []domain.DkAttributeValue{}
		byID[a.ID] = len(attrs)
		attrs = append(attrs, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(attrs) == 0 {
		return attrs, nil
	}
	valueRows, err := r.db.QueryContext(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM categories WHERE slug = $1 OR id = $1
			UNION ALL
			SELECT c.id FROM categories c JOIN sub s ON c.parent_id = s.id
		)
		SELECT DISTINCT cav.dk_attr_id, v.dk_value_id, v.title_fa, COALESCE(v.title_en, '')
		FROM dk_category_attribute_values cav
		JOIN dk_attribute_values v
		  ON v.dk_attr_id = cav.dk_attr_id AND v.dk_value_id = cav.dk_value_id
		WHERE cav.category_id IN (SELECT id FROM sub)
		ORDER BY cav.dk_attr_id ASC, v.dk_value_id ASC
	`, slugOrID)
	if err != nil {
		return nil, err
	}
	defer valueRows.Close()
	for valueRows.Next() {
		var attrID int
		var v domain.DkAttributeValue
		if err := valueRows.Scan(&attrID, &v.ID, &v.TitleFa, &v.TitleEn); err != nil {
			return nil, err
		}
		if idx, ok := byID[attrID]; ok {
			attrs[idx].Values = append(attrs[idx].Values, v)
		}
	}
	return attrs, valueRows.Err()
}

// SearchCategories searches categories by slug or title (D40, new API
// shape: live 404ed here). An empty query returns an empty list, never
// the whole 3,184-row tree. The 50-row limit is a new-API practicality,
// not a live-proven value, and is documented as such.
func (r *CatalogRepository) SearchCategories(ctx context.Context, q string) ([]domain.Category, error) {
	out := []domain.Category{}
	if strings.TrimSpace(q) == "" {
		return out, nil
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, slug, icon_name, COALESCE(gradient, ''), COALESCE(image_url, ''), parent_id
		FROM categories
		WHERE slug ILIKE $1 OR title ILIKE $1
		ORDER BY id ASC
		LIMIT 50
	`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.Category
		var parentID sql.NullString
		if err := rows.Scan(&c.ID, &c.Title, &c.Slug, &c.IconName, &c.Gradient, &c.ImageURL, &parentID); err != nil {
			return nil, err
		}
		if parentID.Valid {
			p := parentID.String
			c.ParentID = &p
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if counts, err := r.GetCategoryProductCounts(ctx); err == nil {
		for i := range out {
			out[i].ProductCount = counts[out[i].ID]
		}
	}
	return out, nil
}

// catalogReviewApprovalNote is the message recorded in MASTER D61 for
// the review dossier ("on approval, product is created without
// origin"). It is served as data for the Team 08 component; this
// endpoint does not enforce any origin rule.
const catalogReviewApprovalNote = "در صورت تأیید، کالا بدون مبدأ ساخته می‌شود"

// GetCatalogReviewDossier serves the D61 backend data: package
// dimensions, product height, warranty, central-warehouse vs
// direct-supplier shipping, and any stored rejection reason. The id
// may be a product id (post-approval dossier) or a pending_approvals
// proposal id (pre-approval dossier). Package dimensions are served
// from the products dimension columns (length/width/height/weight):
// no separate packaging-dimension column exists on products, and none
// is invented here. A nil return with nil error means "not found".
func (r *CatalogRepository) GetCatalogReviewDossier(ctx context.Context, id string) (*domain.CatalogReviewDossier, error) {
	if p, err := r.GetProductByID(ctx, id); err != nil {
		return nil, err
	} else if p != nil {
		images, _ := r.GetProductImages(ctx, p.ID)
		if images == nil {
			images = []string{}
		}
		variants, _ := r.GetProductVariants(ctx, p.ID)
		var rejection *string
		var rr sql.NullString
		if err := r.db.QueryRowContext(ctx, `
			SELECT rejection_reason FROM pending_approvals
			WHERE target_id = $1 AND entity_type = 'proposed_product'
			ORDER BY created_at DESC LIMIT 1
		`, p.ID).Scan(&rr); err == nil && rr.Valid && rr.String != "" {
			s := rr.String
			rejection = &s
		}
		status := "inactive"
		if p.IsActive {
			status = "active"
		}
		specs := p.Specs
		if specs == nil {
			specs = map[string]interface{}{}
		}
		return &domain.CatalogReviewDossier{
			ProductID: p.ID,
			Title:     p.Title,
			Slug:      p.Slug,
			Brand:     p.Brand,
			Category:  p.CategorySlug,
			Status:    status,
			PackageDimensions: domain.DossierDimensions{
				LengthMm:    p.LengthMM,
				WidthMm:     p.WidthMM,
				HeightMm:    p.HeightMM,
				WeightGrams: p.WeightGrams,
			},
			ProductHeightMm: p.HeightMM,
			WeightGrams:     p.WeightGrams,
			Warranty:        p.SupplierWarranty,
			Fulfillment:     p.FulfillmentMethod,
			ShippingMode:    dossierShippingMode(p.FulfillmentMethod),
			Specs:           specs,
			Images:          images,
			VariantCount:    len(variants),
			RejectionReason: rejection,
			ApprovalNote:    catalogReviewApprovalNote,
		}, nil
	}

	var title, requestedBy, status string
	var rawData []byte
	var rr sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT title, requested_data, requested_by, status, rejection_reason
		FROM pending_approvals
		WHERE id = $1 AND entity_type = 'proposed_product'
	`, id).Scan(&title, &rawData, &requestedBy, &status, &rr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := map[string]interface{}{}
	_ = json.Unmarshal(rawData, &data)
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := data[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	num := func(keys ...string) *int {
		for _, k := range keys {
			if v, ok := data[k].(float64); ok && v > 0 {
				n := int(v)
				return &n
			}
		}
		return nil
	}
	fulfillment := str("fulfillment_method", "fulfillmentMethod")
	if fulfillment == "" {
		fulfillment = "warehouse"
	}
	var rejection *string
	if rr.Valid && rr.String != "" {
		s := rr.String
		rejection = &s
	}
	proposalID := id
	return &domain.CatalogReviewDossier{
		ProductID:  "",
		ProposalID: &proposalID,
		Title:      title,
		Slug:       "",
		Brand:      str("brand"),
		Category:   str("category", "categorySlug"),
		Status:     status,
		PackageDimensions: domain.DossierDimensions{
			LengthMm:    num("length_mm", "lengthMm"),
			WidthMm:     num("width_mm", "widthMm"),
			HeightMm:    num("height_mm", "heightMm"),
			WeightGrams: num("weight_grams", "weightGrams"),
		},
		ProductHeightMm: num("height_mm", "heightMm"),
		WeightGrams:     num("weight_grams", "weightGrams"),
		Warranty:        str("supplier_warranty", "warranty"),
		Fulfillment:     fulfillment,
		ShippingMode:    dossierShippingMode(fulfillment),
		Specs:           map[string]interface{}{},
		Images:          []string{},
		VariantCount:    0,
		RejectionReason: rejection,
		ApprovalNote:    catalogReviewApprovalNote,
	}, nil
}

// dossierShippingMode maps a fulfillment_method value to the dossier's
// central-warehouse vs direct-supplier wording (D61). Unknown values
// pass through unchanged rather than being forced into either bucket.
func dossierShippingMode(fulfillment string) string {
	switch fulfillment {
	case "warehouse":
		return "central-warehouse"
	case "supplier_direct":
		return "direct-supplier"
	default:
		return fulfillment
	}
}

// ProductReview is one published product review (same field shape as
// the /reviews endpoint items, used by /products/<id>/reviews).
type ProductReview struct {
	ID        string    `json:"id"`
	ProductID string    `json:"productId"`
	UserID    string    `json:"userId"`
	Rating    int       `json:"rating"`
	Comment   string    `json:"comment"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// GetProductReviews returns published reviews for a product, newest first.
func (r *CatalogRepository) GetProductReviews(ctx context.Context, productID string) ([]ProductReview, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, product_id, user_id, rating, comment, status, created_at
		FROM product_reviews WHERE product_id = $1 AND status = 'published' ORDER BY created_at DESC`,
		productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProductReview{}
	for rows.Next() {
		var rv ProductReview
		if err := rows.Scan(&rv.ID, &rv.ProductID, &rv.UserID, &rv.Rating, &rv.Comment, &rv.Status, &rv.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rv)
	}
	return out, rows.Err()
}

// FIX 48 — display name for one review author (live rv displayName).
func (r *CatalogRepository) GetUserDisplayName(ctx context.Context, userID string) (string, error) {
	var name sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT full_name FROM users WHERE id = $1`, userID).Scan(&name)
	if err != nil {
		return "", err
	}
	return name.String, nil
}
