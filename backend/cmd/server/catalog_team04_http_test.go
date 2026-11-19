package main

// Team 04 live parity HTTP integration tests (handlers only). These run
// ONLY when TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local
// Postgres (never the live database); otherwise they skip. The
// repository integration test seeds the schema; this file reuses the
// same seed through a shared helper in the repository test package is
// not accessible here, so it seeds its own minimal copy.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func team04HTTPRepos(t *testing.T) (*repository.CatalogRepository, *repository.StoreRepository) {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB HTTP test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`DROP TABLE IF EXISTS search_queries, store_products, product_images, product_variant_images, product_variants, product_niches, pending_approvals, products CASCADE`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100) DEFAULT 'انبار مرکزی تهران'::character varying , preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50) DEFAULT 'warehouse'::character varying , supply_option VARCHAR(50) DEFAULT '1day'::character varying , is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_status VARCHAR(255) NOT NULL DEFAULT 'unverified',
			-- TEST-02: shape-compatible columns (shared-DB pollution repair)
			measurement_version INTEGER NOT NULL DEFAULT 0,
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			attribute_schema jsonb,
			barcode character varying(64),
			base_price_override bigint,
			implicit_agreement boolean NOT NULL DEFAULT false,
			implicit_agreement_until timestamp with time zone,
			markup_type character varying(10),
			markup_value numeric(10,2),
			measurement_revision_id text,
			measurement_source character varying(20),
			origin_supplier_warehouse_id text,
			packaging_height_mm integer,
			packaging_length_mm integer,
			packaging_width_mm integer,
			product_type text NOT NULL DEFAULT 'legacy'::text,
			rating numeric(3,2) NOT NULL DEFAULT 0,
			reviews_count integer NOT NULL DEFAULT 0,
			schema_version integer NOT NULL DEFAULT 0,
			spn character varying(20),
			updated_at timestamp with time zone NOT NULL DEFAULT now(),
			warranty_property_id character varying(60))`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'x', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, sale_price BIGINT, sale_from TIMESTAMPTZ, sale_to TIMESTAMPTZ, shipping_class TEXT, tax_class TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			combination_key text,
			inventory_mode text NOT NULL DEFAULT 'legacy'::text,
			is_active boolean NOT NULL DEFAULT true,
			max_cart_qty integer,
			measurement_revision_id text,
			measurement_source text,
			measurement_status text NOT NULL DEFAULT 'unverified'::text,
			measurement_version integer NOT NULL DEFAULT 0,
			min_cart_qty integer NOT NULL DEFAULT 1,
			selection jsonb NOT NULL DEFAULT '{}'::jsonb)`,
		`CREATE TABLE product_images (id UUID  DEFAULT gen_random_uuid() PRIMARY KEY, product_id VARCHAR(50) NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_variant_images (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), variant_id UUID NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE search_queries (id BIGSERIAL PRIMARY KEY, term_normalized TEXT NOT NULL, store_slug TEXT NOT NULL, result_count INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			-- TEST-02 round 4: full demanded set (shared-DB pollution repair)
			catalog_result_count integer)`,
		`CREATE TABLE pending_approvals (id VARCHAR(50) PRIMARY KEY, entity_type VARCHAR(50) NOT NULL, target_id VARCHAR(50) NOT NULL, title TEXT NOT NULL, requested_data JSONB NOT NULL DEFAULT '{}', requested_by VARCHAR(50) NOT NULL, status VARCHAR(50)  DEFAULT 'pending'::character varying NOT NULL, rejection_reason TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), reviewed_at TIMESTAMPTZ)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	seed := []string{
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-active-1','دستبند طلا','gold-bracelet','gold-silver','sup-1',72500000,58000000,0,TRUE,5,'img','{"power":"1450 وات"}','desc','انبار مرکزی تهران',1,'۱۸ ماه گارانتی شرکتی',FALSE,FALSE,'warehouse','1day',TRUE,'نشر دمو',10000002,120,80,40,500)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('prod-inactive-1','گوشی تست','test-phone','mobile','sup-1',1000000,800000,0,TRUE,0,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',FALSE,'نشر دمو',10000004)`,
		`INSERT INTO product_variants (id,product_id,spnc,spnc_number,variant_name,attributes,is_default,stock,supplier_base_cost,created_at) VALUES ('11111111-1111-1111-1111-111111111111','prod-active-1','FE16A2E1',20000001,'پیش‌فرض','{}',TRUE,5,NULL,NOW())`,
		`INSERT INTO product_variants (id,product_id,spnc,spnc_number,variant_name,attributes,is_default,stock,supplier_base_cost,created_at) VALUES ('22222222-2222-2222-2222-222222222222','prod-active-1','55F08797',20000002,'نگین‌دار','{"style":"نگین‌دار"}',FALSE,2,64000000,NOW())`,
		`INSERT INTO product_images VALUES ('33333333-3333-3333-3333-333333333333','prod-active-1','img1',0,NOW())`,
		`INSERT INTO store_products VALUES ('store-a','prod-active-1',0)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
	// No stores table: GetStoreBySlug errors -> nil store -> central
	// pricing via defaults (loadPricingSettings(nil) = live defaults).
	return repository.NewCatalogRepository(db), repository.NewStoreRepository(db)
}

func team04Get(t *testing.T, handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestTeam04HTTPListAndDetail(t *testing.T) {
	catalogRepo, storeRepo := team04HTTPRepos(t)
	ctx := context.Background()
	listHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, ctx, catalogRepo, storeRepo, nil)
	}

	// List: active only, 23 keys (21 always-present + priceMin/priceMax,
	// P1(a)), variant prices differ per variant.
	rec := team04Get(t, listHandler, "/api/v1/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	// PERF-01: the list endpoint returns the paged envelope
	// {items, nextCursor, hasMore, total}; the bare-array shape is retired.
	var listEnvelope struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	list := listEnvelope.Items
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1 active only", len(list))
	}
	if len(list[0]) != 23 {
		t.Fatalf("list item keys = %d, want exactly 23", len(list[0]))
	}
	for _, forbidden := range []string{"supplierId", "supplierName", "baseCostB", "profitToman", "marginPercent", "installmentPrice"} {
		if _, ok := list[0][forbidden]; ok {
			t.Fatalf("list item must not contain %q", forbidden)
		}
	}
	var vp map[string]map[string]int64
	if err := json.Unmarshal(list[0]["variantPrices"], &vp); err != nil {
		t.Fatalf("variantPrices decode: %v", err)
	}
	// FIX 4: live keys variantPrices by variant ID (UUID), not spnc.
	if vp["11111111-1111-1111-1111-111111111111"]["price"] != 63800000 || vp["22222222-2222-2222-2222-222222222222"]["price"] != 70400000 {
		t.Fatalf("variantPrices = %v, want 63800000/70400000 by variant id", vp)
	}
	if vp["11111111-1111-1111-1111-111111111111"]["originalPrice"] != 72500000 {
		t.Fatalf("variantPrices originalPrice = %v, want 72500000", vp)
	}
	// P1(a): price range = min/max of priced active variants; the
	// card price (Price) is unchanged by the range.
	var priceMin, priceMax int64
	if err := json.Unmarshal(list[0]["priceMin"], &priceMin); err != nil || priceMin != 63800000 {
		t.Fatalf("priceMin = %d, want 63800000", priceMin)
	}
	if err := json.Unmarshal(list[0]["priceMax"], &priceMax); err != nil || priceMax != 70400000 {
		t.Fatalf("priceMax = %d, want 70400000", priceMax)
	}
	if _, ok := list[0]["storeDefaultVariantId"]; ok {
		t.Fatalf("storeDefaultVariantId must be omitted with no default row")
	}

	// Brand exact match through HTTP.
	rec = team04Get(t, listHandler, "/api/v1/products?brand=%D9%86%D8%B4%D8%B1")
	if rec.Code != http.StatusOK {
		t.Fatalf("brand list status = %d", rec.Code)
	}
	var brandEnvelope struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &brandEnvelope)
	brandList := brandEnvelope.Items
	if len(brandList) != 0 {
		t.Fatalf("brand partial 'نشر' returned %d items, want 0 (exact match)", len(brandList))
	}

	// Search logs the event (D43) with the catalog result count.
	rec = team04Get(t, listHandler, "/api/v1/products?search=%D8%B7%D9%84%D8%A7")
	if rec.Code != http.StatusOK {
		t.Fatalf("search list status = %d", rec.Code)
	}
	var searchEnvelope struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &searchEnvelope)
	searchList := searchEnvelope.Items
	if len(searchList) != 1 {
		t.Fatalf("search 'طلا' returned %d items, want 1", len(searchList))
	}

	// Detail: inactive product returns 200 (D42 live-confirmed-200).
	detailHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductDetail(w, r, ctx, catalogRepo, storeRepo, nil, "prod-inactive-1")
	}
	rec = team04Get(t, detailHandler, "/api/v1/products/prod-inactive-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("inactive detail status = %d, want 200", rec.Code)
	}
	var detail map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail) != 25 {
		t.Fatalf("detail keys = %d, want 20 live + 5 extensions", len(detail))
	}
	// P1(a): zero-variant legacy product => range = product price.
	var dMin, dMax int64
	_ = json.Unmarshal(detail["priceMin"], &dMin)
	_ = json.Unmarshal(detail["priceMax"], &dMax)
	if dMin != 880000 || dMax != 880000 {
		t.Fatalf("zero-variant detail priceMin/priceMax = %d/%d, want 880000/880000", dMin, dMax)
	}
	var isActive bool
	_ = json.Unmarshal(detail["isActive"], &isActive)
	if isActive {
		t.Fatalf("inactive detail isActive = true, want false")
	}

	// Detail via spn- lookup (D46 decision: server lookup implemented).
	spnHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductDetail(w, r, ctx, catalogRepo, storeRepo, nil, "spn-10000002")
	}
	rec = team04Get(t, spnHandler, "/api/v1/products/spn-10000002")
	if rec.Code != http.StatusOK {
		t.Fatalf("spn detail status = %d, body %s", rec.Code, rec.Body.String())
	}
	var spnDetail map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &spnDetail)
	var spnTitle string
	_ = json.Unmarshal(spnDetail["title"], &spnTitle)
	if spnTitle != "دستبند طلا" {
		t.Fatalf("spn detail title = %q", spnTitle)
	}
	// The current public DTO deliberately omits specs/images (FIX 12 in
	// assembleProductDetail), while the repository keeps dossier data intact.
	if _, exists := spnDetail["specs"]; exists {
		t.Fatal("public detail unexpectedly exposes dossier specs")
	}
	if _, exists := spnDetail["images"]; exists {
		t.Fatal("public detail unexpectedly exposes dossier gallery")
	}
	stored, err := catalogRepo.GetProductByID(ctx, "prod-active-1")
	if err != nil || stored == nil || stored.Specs["power"] != "1450 وات" {
		t.Fatalf("stored dossier specs were lost: product=%v error=%v", stored, err)
	}
	var storeCount int
	_ = json.Unmarshal(spnDetail["storeCount"], &storeCount)
	if storeCount != 1 {
		t.Fatalf("spn detail storeCount = %d, want 1", storeCount)
	}

	// Unknown product still 404s.
	missHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductDetail(w, r, ctx, catalogRepo, storeRepo, nil, "no-such")
	}
	rec = team04Get(t, missHandler, "/api/v1/products/no-such")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown detail status = %d, want 404", rec.Code)
	}

	// Price bounds (D05): active only, engine prices.
	boundsHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductPriceBounds(w, r, ctx, catalogRepo, storeRepo, nil)
	}
	rec = team04Get(t, boundsHandler, "/api/v1/products/price-bounds")
	if rec.Code != http.StatusOK {
		t.Fatalf("price-bounds status = %d", rec.Code)
	}
	var bounds struct {
		MinPrice int64 `json:"minPrice"`
		MaxPrice int64 `json:"maxPrice"`
		Count    int   `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bounds); err != nil {
		t.Fatalf("price-bounds decode: %v", err)
	}
	if bounds.Count != 1 || bounds.MinPrice != 63800000 || bounds.MaxPrice != 63800000 {
		t.Fatalf("price-bounds = %+v, want 63800000/63800000/1", bounds)
	}
}
