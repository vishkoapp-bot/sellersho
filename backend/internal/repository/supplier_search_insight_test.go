package repository

// Supplier search-insight integration tests (D12). They run ONLY when
// TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres (never
// the live database); otherwise they skip. The schema below is a
// minimal subset of the real migrations, sufficient for
// GetSupplierSearchInsight: products/categories footprint, the
// search_queries log (WITH catalog_result_count), and paid-order trend
// tables.

import (
	"context"
	"database/sql"
	"testing"
)

func searchInsightSeed(t *testing.T, db *sql.DB) {
	t.Helper()
	ddl := []string{
		`DROP TABLE IF EXISTS order_items, orders, search_queries, products, categories CASCADE`,
		`CREATE TABLE categories (id VARCHAR(50) PRIMARY KEY, title TEXT NOT NULL, slug VARCHAR(100) NOT NULL,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			depth smallint NOT NULL DEFAULT 0,
			gradient character varying(100),
			icon_name character varying(50),
			image_url text,
			is_active boolean NOT NULL DEFAULT true,
			parent_id character varying(50),
			sort_order integer NOT NULL DEFAULT 0)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), image_url TEXT, brand TEXT NOT NULL DEFAULT '', is_active BOOLEAN NOT NULL DEFAULT TRUE,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_version INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			created_at timestamp with time zone NOT NULL DEFAULT now(),
			description text,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			height_mm integer,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			is_featured boolean DEFAULT false,
			is_original boolean NOT NULL DEFAULT true,
			is_special_deal boolean DEFAULT false,
			length_mm integer,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
			measurement_source character varying(20),
			origin_supplier_warehouse_id text,
			original_price bigint,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			platform_margin bigint,
			preparation_days integer DEFAULT 1,
			product_type text NOT NULL DEFAULT 'legacy'::text,
			rating numeric(3,2) NOT NULL DEFAULT 0,
			reviews_count integer NOT NULL DEFAULT 0,
			schema_version integer NOT NULL DEFAULT 0,
			shipping_class text NOT NULL DEFAULT 'standard'::text,
			specs jsonb DEFAULT '{}'::jsonb,
			spn character varying(20),
			spn_number bigint,
			stock integer NOT NULL DEFAULT 0,
			supplier_base_cost bigint,
			supplier_warranty character varying(100),
			supply_option character varying(20) NOT NULL DEFAULT '1day'::character varying,
			tax_class text NOT NULL DEFAULT 'standard'::text,
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warehouse_name character varying(100) DEFAULT 'انبار مرکزی تهران'::character varying,
			warranty_property_id character varying(60),
			weight_grams integer,
			width_mm integer)`,
		`CREATE TABLE search_queries (id BIGSERIAL PRIMARY KEY, term_normalized TEXT NOT NULL, store_slug TEXT NOT NULL, result_count INTEGER NOT NULL, catalog_result_count INTEGER, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE orders (id VARCHAR(50) PRIMARY KEY, payment_status VARCHAR(20) NOT NULL DEFAULT 'pending', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			order_number VARCHAR(50) NOT NULL DEFAULT '',
			payable_amount BIGINT NOT NULL DEFAULT 0,
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			customer_user_id UUID,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			box_size character varying(80),
			carrier character varying(100),
			confirmed_at timestamp with time zone,
			coupon_discount_amount bigint,
			customer_address text,
			customer_city character varying(50),
			customer_full_name character varying(100),
			customer_phone character varying(20),
			customer_postal_code character varying(20),
			customer_province character varying(50),
			discount_amount bigint NOT NULL DEFAULT 0,
			discount_code text,
			dispute_reason text,
			disputed_at timestamp with time zone,
			dropshipper_profit bigint NOT NULL DEFAULT 0,
			fulfillment_method character varying(20) DEFAULT 'warehouse'::character varying,
			gateway_amount bigint NOT NULL DEFAULT 0,
			idempotency_key text,
			inventory_captured boolean NOT NULL DEFAULT false,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			packed_at timestamp with time zone,
			payment_expires_at timestamp with time zone DEFAULT (now() + '00:15:00'::interval),
			payment_method character varying(20) NOT NULL DEFAULT 'cash'::character varying,
			platform_addon_amount bigint NOT NULL DEFAULT 0,
			platform_profit bigint NOT NULL DEFAULT 0,
			profit_released boolean DEFAULT false,
			profit_released_at timestamp with time zone,
			referral_commission bigint DEFAULT 0,
			referral_commission_released boolean DEFAULT false,
			referrer_seller_id character varying(50),
			return_window_ends_at timestamp with time zone,
			seller_discount_amount bigint NOT NULL DEFAULT 0,
			shipped_at timestamp with time zone,
			shipping_cost bigint NOT NULL DEFAULT 0,
			shipping_label text,
			shipping_quote_id text,
			status character varying(30) NOT NULL DEFAULT 'processing'::character varying,
			store_id character varying(50),
			store_name character varying(100),
			supplier_addon_amount bigint NOT NULL DEFAULT 0,
			total_amount bigint,
			tracking_code character varying(50),
			tracking_number character varying(100),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			wallet_amount bigint NOT NULL DEFAULT 0,
			weight_grams integer)`,
		`CREATE TABLE order_items (id VARCHAR(50)  DEFAULT gen_random_uuid() PRIMARY KEY, order_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, quantity INT NOT NULL DEFAULT 0, wholesale_cost BIGINT NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			discount_amount bigint NOT NULL DEFAULT 0,
			final_unit_price bigint,
			platform_fee bigint,
			pricing_policy_version text NOT NULL DEFAULT 'legacy-unversioned'::text,
			product_title_snapshot text,
			seller_margin bigint,
			variant_id uuid,
			variant_name text,
			variant_spnc character varying(12))`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed (%q): %v", q[:60], err)
		}
	}
	seed := []string{
		`INSERT INTO categories VALUES ('cat-sport','پوشاک ورزشی','sportswear')`,
		`INSERT INTO categories VALUES ('cat-hat','کلاه ورزشی','sport-hats')`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,image_url,brand,is_active) VALUES ('pa1','کفش رانینگ الف','pa1','sportswear','sup-a','img-a1','برند الف',TRUE)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,image_url,brand,is_active) VALUES ('pa2','لباس تمرین الف','pa2','sportswear','sup-a','','برند الف',TRUE)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,image_url,brand,is_active) VALUES ('pb1','کلاه ورزشی ب','pb1','sport-hats','sup-b','img-b1','برند ب',TRUE)`,
		// Trend: pa1 sells 10 in-window vs 5 in the previous window
		// (days=30) on PAID orders; a pending order of 99 must be
		// ignored. pa2 is new (no previous window). pb1 is B's.
		`INSERT INTO orders (id,payment_status,created_at) VALUES ('o1','paid', NOW() - INTERVAL '1 day')`,
		`INSERT INTO orders (id,payment_status,created_at) VALUES ('o2','paid', NOW() - INTERVAL '40 days')`,
		`INSERT INTO orders (id,payment_status,created_at) VALUES ('o3','pending', NOW() - INTERVAL '1 day')`,
		`INSERT INTO orders (id,payment_status,created_at) VALUES ('o4','paid', NOW() - INTERVAL '2 days')`,
		`INSERT INTO order_items (id,order_id,product_id,quantity) VALUES ('oi1','o1','pa1',10)`,
		`INSERT INTO order_items (id,order_id,product_id,quantity) VALUES ('oi2','o2','pa1',5)`,
		`INSERT INTO order_items (id,order_id,product_id,quantity) VALUES ('oi3','o3','pa1',99)`,
		`INSERT INTO order_items (id,order_id,product_id,quantity) VALUES ('oi4','o4','pa2',4)`,
		`INSERT INTO order_items (id,order_id,product_id,quantity) VALUES ('oi5','o1','pb1',7)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed (%q): %v", q[:60], err)
		}
	}
	// Search-log rows: n copies of a term with the given result/catalog
	// counts, ageDays old, spread over two store slugs (the insight is
	// platform-wide; slugs must never matter or leak).
	logSearch := func(term string, n, resultCount int, catalogCount *int, ageDays int) {
		t.Helper()
		for i := 0; i < n; i++ {
			slug := "central"
			if i%2 == 1 {
				slug = "mone"
			}
			if _, err := db.Exec(`INSERT INTO search_queries (term_normalized, store_slug, result_count, catalog_result_count, created_at)
				VALUES ($1, $2, $3, $4, NOW() - ($5 || ' days')::interval)`, term, slug, resultCount, catalogCount, ageDays); err != nil {
				t.Fatalf("search seed (%q): %v", term, err)
			}
		}
	}
	intPtr := func(v int) *int { return &v }
	// A's own product-title demand (one zero-result row of three).
	logSearch("کفش رانینگ", 1, 5, intPtr(5), 1)
	logSearch("کفش رانینگ", 1, 0, intPtr(5), 1)
	logSearch("کفش رانینگ", 1, 3, intPtr(5), 1)
	// B's brand-only demand.
	logSearch("برند ب", 3, 2, intPtr(2), 1)
	// Below the minimum-volume threshold (2 < 3): suppressed.
	logSearch("رانینگ", 2, 1, intPtr(1), 1)
	// Unmet for A: category footprint, proven catalog-absent.
	logSearch("پوشاک ورزشی", 3, 0, intPtr(0), 1)
	// Brand term for A with a NULL catalog row: NULL means "unknown",
	// so it must never be certified unmet.
	logSearch("برند الف", 2, 0, intPtr(0), 1)
	logSearch("برند الف", 1, 0, nil, 1)
	// Positive catalog rows disqualify unmet even with zero store
	// results on every row.
	logSearch("لباس تمرین", 3, 0, intPtr(3), 1)
	// Writer-caveat drop: logged as catalog-absent, but a live title
	// match exists (B's own product) — a discoverability gap, not
	// supply, so it must drop out of unmet.
	logSearch("کلاه ورزشی", 3, 0, intPtr(0), 1)
	// Old demand: outside a 30-day window, inside a 90-day one.
	logSearch("الف", 3, 1, intPtr(1), 40)
}

func demandTerms(in *SupplierSearchInsight) map[string]SupplierSearchInsightDemandTerm {
	out := map[string]SupplierSearchInsightDemandTerm{}
	for _, d := range in.TopDemand {
		out[d.Term] = d
	}
	return out
}

func unmetTerms(in *SupplierSearchInsight) map[string]SupplierSearchInsightUnmetTerm {
	out := map[string]SupplierSearchInsightUnmetTerm{}
	for _, u := range in.UnmetInScope {
		out[u.Term] = u
	}
	return out
}

func TestSupplierSearchInsightFootprintScoping(t *testing.T) {
	db := team04TestDB(t)
	searchInsightSeed(t, db)
	repo := NewSupplierRepository(db)
	ctx := context.Background()

	a, err := repo.GetSupplierSearchInsight(ctx, "sup-a", 30)
	if err != nil {
		t.Fatalf("GetSupplierSearchInsight(sup-a): %v", err)
	}
	if a.WindowDays != 30 {
		t.Fatalf("windowDays = %d, want 30", a.WindowDays)
	}
	aDemand := demandTerms(a)
	aUnmet := unmetTerms(a)

	// Own product-title demand, with the matched product and counts.
	d, ok := aDemand["کفش رانینگ"]
	if !ok {
		t.Fatalf("sup-a topDemand missing «کفش رانینگ»: %+v", a.TopDemand)
	}
	if d.Searches != 3 || d.ZeroResultSearches != 1 {
		t.Fatalf("«کفش رانینگ» counts = %d/%d, want 3/1", d.Searches, d.ZeroResultSearches)
	}
	if len(d.MatchedProducts) != 1 || d.MatchedProducts[0].ProductID != "pa1" {
		t.Fatalf("«کفش رانینگ» matchedProducts = %+v, want [pa1]", d.MatchedProducts)
	}
	// Below-threshold term suppressed although it matches a title.
	if _, ok := aDemand["رانینگ"]; ok {
		t.Fatalf("term with 2 searches must be suppressed (threshold 3)")
	}
	// B's brand demand must not leak into A's insight.
	if _, ok := aDemand["برند ب"]; ok {
		t.Fatalf("sup-a topDemand leaked B's brand term")
	}
	if _, ok := aUnmet["برند ب"]; ok {
		t.Fatalf("sup-a unmet leaked B's brand term")
	}
	// Brand-only match for A: shown in topDemand with no products.
	d, ok = aDemand["برند الف"]
	if !ok {
		t.Fatalf("sup-a topDemand missing brand term «برند الف»")
	}
	if len(d.MatchedProducts) != 0 {
		t.Fatalf("«برند الف» matchedProducts = %+v, want empty (brand-level match)", d.MatchedProducts)
	}
	// ... but its NULL catalog row disqualifies it from unmet.
	if _, ok := aUnmet["برند الف"]; ok {
		t.Fatalf("term with a NULL catalog_result_count row must not be unmet")
	}
	// Positive catalog rows disqualify unmet; the term is demand with
	// a live title match instead.
	if _, ok := aUnmet["لباس تمرین"]; ok {
		t.Fatalf("term with catalog_result_count > 0 rows must not be unmet")
	}
	if d, ok := aDemand["لباس تمرین"]; !ok || len(d.MatchedProducts) != 1 || d.MatchedProducts[0].ProductID != "pa2" {
		t.Fatalf("«لباس تمرین» demand = %+v (ok=%v), want matched [pa2]", d, ok)
	}
	// Unmet with a category scope match.
	u, ok := aUnmet["پوشاک ورزشی"]
	if !ok {
		t.Fatalf("sup-a unmet missing «پوشاک ورزشی»: %+v", a.UnmetInScope)
	}
	if len(u.ScopeMatches) != 1 || u.ScopeMatches[0].Type != "category" || u.ScopeMatches[0].Name != "پوشاک ورزشی" {
		t.Fatalf("«پوشاک ورزشی» scopeMatches = %+v, want one category match", u.ScopeMatches)
	}
	// The 40-day-old term is outside the 30-day window.
	if _, ok := aDemand["الف"]; ok {
		t.Fatalf("40-day-old term must be absent at days=30")
	}

	// Trend: paid only, previous-window comparison, null delta for new.
	if len(a.OwnProductTrend) != 2 {
		t.Fatalf("sup-a trend = %+v, want 2 items", a.OwnProductTrend)
	}
	tr := a.OwnProductTrend[0]
	if tr.ProductID != "pa1" || tr.Units != 10 || tr.PrevUnits != 5 || tr.DeltaPct == nil || *tr.DeltaPct != 100 {
		t.Fatalf("pa1 trend = %+v, want units 10 prev 5 delta 100", tr)
	}
	tr = a.OwnProductTrend[1]
	if tr.ProductID != "pa2" || tr.Units != 4 || tr.PrevUnits != 0 || tr.DeltaPct != nil {
		t.Fatalf("pa2 trend = %+v, want units 4 prev 0 delta null", tr)
	}

	b, err := repo.GetSupplierSearchInsight(ctx, "sup-b", 30)
	if err != nil {
		t.Fatalf("GetSupplierSearchInsight(sup-b): %v", err)
	}
	bDemand := demandTerms(b)
	bUnmet := unmetTerms(b)
	// A's title term must not leak into B's insight.
	if _, ok := bDemand["کفش رانینگ"]; ok {
		t.Fatalf("sup-b topDemand leaked A's product term")
	}
	// Brand-only match for B (absent for A, asserted above).
	d, ok = bDemand["برند ب"]
	if !ok {
		t.Fatalf("sup-b topDemand missing brand term «برند ب»")
	}
	if len(d.MatchedProducts) != 0 {
		t.Fatalf("«برند ب» matchedProducts = %+v, want empty", d.MatchedProducts)
	}
	if _, ok := bDemand["برند الف"]; ok {
		t.Fatalf("sup-b topDemand leaked A's brand term")
	}
	// Writer-caveat drop: the live catalog match keeps «کلاه ورزشی»
	// out of unmet, and it surfaces as demand with B's own product.
	if _, ok := bUnmet["کلاه ورزشی"]; ok {
		t.Fatalf("term with a live catalog title match must drop out of unmet")
	}
	if d, ok := bDemand["کلاه ورزشی"]; !ok || len(d.MatchedProducts) != 1 || d.MatchedProducts[0].ProductID != "pb1" {
		t.Fatalf("«کلاه ورزشی» demand for B = %+v (ok=%v), want matched [pb1]", d, ok)
	}
	// B's trend holds only B's product.
	if len(b.OwnProductTrend) != 1 || b.OwnProductTrend[0].ProductID != "pb1" || b.OwnProductTrend[0].Units != 7 {
		t.Fatalf("sup-b trend = %+v, want [pb1 x7]", b.OwnProductTrend)
	}

	// Supplier with no products: same shape, every section empty.
	c, err := repo.GetSupplierSearchInsight(ctx, "sup-c", 30)
	if err != nil {
		t.Fatalf("GetSupplierSearchInsight(sup-c): %v", err)
	}
	if c.TopDemand == nil || c.UnmetInScope == nil || c.OwnProductTrend == nil {
		t.Fatalf("empty insight sections must be non-nil: %+v", c)
	}
	if len(c.TopDemand) != 0 || len(c.UnmetInScope) != 0 || len(c.OwnProductTrend) != 0 {
		t.Fatalf("sup-c insight = %+v, want all sections empty", c)
	}
}

func TestSupplierSearchInsightWindow(t *testing.T) {
	db := team04TestDB(t)
	searchInsightSeed(t, db)
	repo := NewSupplierRepository(db)
	ctx := context.Background()

	a90, err := repo.GetSupplierSearchInsight(ctx, "sup-a", 90)
	if err != nil {
		t.Fatalf("GetSupplierSearchInsight(sup-a, 90): %v", err)
	}
	if _, ok := demandTerms(a90)["الف"]; !ok {
		t.Fatalf("40-day-old term must appear at days=90: %+v", a90.TopDemand)
	}
	// An out-of-set window falls back to 30.
	aBad, err := repo.GetSupplierSearchInsight(ctx, "sup-a", 14)
	if err != nil {
		t.Fatalf("GetSupplierSearchInsight(sup-a, 14): %v", err)
	}
	if aBad.WindowDays != 30 {
		t.Fatalf("windowDays for days=14 = %d, want fallback 30", aBad.WindowDays)
	}
	if _, ok := demandTerms(aBad)["الف"]; ok {
		t.Fatalf("40-day-old term must be absent under the 30-day fallback window")
	}
}

func TestSupplierSearchInsightMissingSearchTable(t *testing.T) {
	db := team04TestDB(t)
	searchInsightSeed(t, db)
	if _, err := db.Exec(`DROP TABLE search_queries`); err != nil {
		t.Fatalf("drop search_queries: %v", err)
	}
	repo := NewSupplierRepository(db)
	a, err := repo.GetSupplierSearchInsight(context.Background(), "sup-a", 30)
	if err != nil {
		t.Fatalf("GetSupplierSearchInsight without search_queries: %v", err)
	}
	if len(a.TopDemand) != 0 || len(a.UnmetInScope) != 0 {
		t.Fatalf("search sections must be empty without the table: %+v", a)
	}
	if len(a.OwnProductTrend) != 2 {
		t.Fatalf("trend must still be computed without the table: %+v", a.OwnProductTrend)
	}
}
