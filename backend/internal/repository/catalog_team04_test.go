package repository

// Team 04 live parity integration tests. These run ONLY when
// TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres (never
// the live database); otherwise they skip. The schema below is a
// minimal subset of the real migrations, sufficient for the catalog
// queries under test.

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/drup/platform/internal/domain"
	_ "github.com/lib/pq"
)

func team04TestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func team04Seed(t *testing.T, db *sql.DB) {
	t.Helper()
	ddl := []string{
		`DROP TABLE IF EXISTS dk_category_attribute_values, dk_category_attributes, dk_attribute_values, dk_attributes, dk_category_brands, dk_category_colors, search_queries, store_products, product_images, product_variants, product_niches, pending_approvals, products, categories CASCADE`,
		`CREATE TABLE categories (id VARCHAR(50) PRIMARY KEY, title TEXT NOT NULL, slug VARCHAR(100) NOT NULL, icon_name TEXT, gradient TEXT, image_url TEXT, parent_id VARCHAR(50), is_active BOOLEAN NOT NULL DEFAULT TRUE)`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100), preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50), supply_option VARCHAR(50), is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'x', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_images (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		// search_queries WITHOUT catalog_result_count: LogSearchEvent's
		// ensure step must add it (mirrors migration 202610310009).
		`CREATE TABLE search_queries (id BIGSERIAL PRIMARY KEY, term_normalized TEXT NOT NULL, store_slug TEXT NOT NULL, result_count INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE pending_approvals (id VARCHAR(50) PRIMARY KEY, entity_type VARCHAR(50) NOT NULL, target_id VARCHAR(50) NOT NULL, title TEXT NOT NULL, requested_data JSONB NOT NULL DEFAULT '{}', requested_by VARCHAR(50) NOT NULL, status VARCHAR(50) NOT NULL, rejection_reason TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), reviewed_at TIMESTAMPTZ)`,
		`CREATE TABLE dk_attributes (dk_attr_id INT PRIMARY KEY, title TEXT NOT NULL, type VARCHAR(50) NOT NULL DEFAULT 'checkbox')`,
		`CREATE TABLE dk_attribute_values (dk_attr_id INT NOT NULL, dk_value_id INT NOT NULL, title_fa TEXT NOT NULL, title_en TEXT, PRIMARY KEY (dk_attr_id, dk_value_id))`,
		`CREATE TABLE dk_category_attributes (category_id VARCHAR(50) NOT NULL, dk_attr_id INT NOT NULL, PRIMARY KEY (category_id, dk_attr_id))`,
		`CREATE TABLE dk_category_attribute_values (category_id VARCHAR(50) NOT NULL, dk_attr_id INT NOT NULL, dk_value_id INT NOT NULL, PRIMARY KEY (category_id, dk_attr_id, dk_value_id))`,
		`CREATE TABLE dk_category_brands (category_id VARCHAR(50) NOT NULL, dk_brand_id INT NOT NULL, code VARCHAR(100), title_fa TEXT NOT NULL, title_en TEXT, PRIMARY KEY (category_id, dk_brand_id))`,
		`CREATE TABLE dk_category_colors (category_id VARCHAR(50) NOT NULL, dk_color_id INT NOT NULL, title TEXT NOT NULL, hex_code VARCHAR(20), PRIMARY KEY (category_id, dk_color_id))`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed (%q): %v", q[:60], err)
		}
	}
	seed := []string{
		`INSERT INTO categories VALUES ('cat-gold','طلا و زیورآلات','gold','Coins','g','u',NULL,FALSE)`,
		`INSERT INTO categories VALUES ('cat-digital','کالای دیجیتال','digital','Smartphone','g','u',NULL,TRUE)`,
		`INSERT INTO categories VALUES ('cat-dk-1','گوشی موبایل','mobile-phone','Smartphone','g','u','cat-digital',TRUE)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-active-1','دستبند طلا','gold-bracelet','gold-silver','sup-1',72500000,58000000,0,TRUE,5,'img','{"power":"1450 وات"}','desc','انبار مرکزی تهران',1,'۱۸ ماه گارانتی شرکتی',FALSE,FALSE,'warehouse','1day',TRUE,'نشر دمو',10000002,120,80,40,500)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('prod-active-2','کتاب نشر','book-nashr','books','sup-1',500000,400000,0,TRUE,10,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'نشر',10000003)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('prod-inactive-1','گوشی تست','test-phone','mobile','sup-1',1000000,800000,0,TRUE,0,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',FALSE,'نشر دمو',10000004)`,
		`INSERT INTO product_variants VALUES ('11111111-1111-1111-1111-111111111111','prod-active-1','FE16A2E1',20000001,'پیش‌فرض','{}',TRUE,5,NULL,NOW())`,
		`INSERT INTO product_variants VALUES ('22222222-2222-2222-2222-222222222222','prod-active-1','55F08797',20000002,'نگین‌دار','{"style":"نگین‌دار"}',FALSE,2,64000000,NOW())`,
		`INSERT INTO product_images VALUES ('33333333-3333-3333-3333-333333333333','prod-active-1','img2',1,NOW())`,
		`INSERT INTO product_images VALUES ('44444444-4444-4444-4444-444444444444','prod-active-1','img1',0,NOW())`,
		`INSERT INTO store_products VALUES ('store-a','prod-active-1',0)`,
		`INSERT INTO store_products VALUES ('store-b','prod-active-1',1)`,
		`INSERT INTO dk_attributes VALUES (2226,'سیستم عامل','checkbox')`,
		`INSERT INTO dk_attribute_values VALUES (2226,19239,'Android','Android')`,
		`INSERT INTO dk_category_attributes VALUES ('cat-dk-1',2226)`,
		`INSERT INTO dk_category_attribute_values VALUES ('cat-dk-1',2226,19239)`,
		`INSERT INTO dk_category_brands VALUES ('cat-dk-1',7,'samsung','سامسونگ','Samsung')`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed (%q): %v", q[:60], err)
		}
	}
}

func TestTeam04CatalogRepo(t *testing.T) {
	db := team04TestDB(t)
	team04Seed(t, db)
	repo := NewCatalogRepository(db)
	ctx := context.Background()

	// D42: list returns active only (2 of 3 seeded products).
	products, err := repo.GetProducts(ctx, ProductFilter{})
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("GetProducts returned %d products, want 2 active only", len(products))
	}
	// D41: brand EXACT match.
	byExact, err := repo.GetProducts(ctx, ProductFilter{Brand: "نشر دمو"})
	if err != nil || len(byExact) != 1 || byExact[0].ID != "prod-active-1" {
		t.Fatalf("brand exact 'نشر دمو' = %v (err %v), want only prod-active-1", idsOf(byExact), err)
	}
	byPartial, err := repo.GetProducts(ctx, ProductFilter{Brand: "نشر"})
	if err != nil || len(byPartial) != 1 || byPartial[0].ID != "prod-active-2" {
		t.Fatalf("brand exact 'نشر' = %v (err %v), want only prod-active-2 (no partial match)", idsOf(byPartial), err)
	}
	// D39: category filter is an EXACT category_slug match.
	byCat, err := repo.GetProducts(ctx, ProductFilter{CategorySlug: "gold"})
	if err != nil || len(byCat) != 0 {
		t.Fatalf("category 'gold' = %d, want 0 (no child expansion)", len(byCat))
	}
	// D42: detail of an inactive product still resolves (live 200).
	inactive, err := repo.GetProductByID(ctx, "prod-inactive-1")
	if err != nil || inactive == nil || inactive.IsActive {
		t.Fatalf("GetProductByID(inactive) = %+v (err %v), want the product with IsActive=false", inactive, err)
	}
	// D46: spn lookup resolves; specs/dims scan through.
	bySpn, err := repo.GetProductBySpn(ctx, 10000002)
	if err != nil || bySpn == nil || bySpn.ID != "prod-active-1" {
		t.Fatalf("GetProductBySpn(10000002) = %+v (err %v)", bySpn, err)
	}
	if bySpn.Specs["power"] != "1450 وات" {
		t.Fatalf("specs not scanned: %v", bySpn.Specs)
	}
	if bySpn.LengthMM == nil || *bySpn.LengthMM != 120 {
		t.Fatalf("lengthMm not scanned: %v", bySpn.LengthMM)
	}
	if _, err := repo.GetProductBySpn(ctx, 99999999); err != nil {
		t.Fatalf("GetProductBySpn unknown: %v", err)
	}
	// D28: variants in repo order, defaults first; gallery in sort_order.
	variants, err := repo.GetProductVariants(ctx, "prod-active-1")
	if err != nil || len(variants) != 2 || !variants[0].IsDefault || variants[0].Spnc != "FE16A2E1" {
		t.Fatalf("variants = %+v (err %v), want default first", variants, err)
	}
	if variants[1].SupplierBaseCost == nil || *variants[1].SupplierBaseCost != 64000000 {
		t.Fatalf("variant cost not scanned: %+v", variants[1])
	}
	images, err := repo.GetProductImages(ctx, "prod-active-1")
	if err != nil || len(images) != 2 || images[0] != "img1" || images[1] != "img2" {
		t.Fatalf("images = %v (err %v), want [img1 img2] in sort_order", images, err)
	}
	// D21 storeCount: two distinct stores carry prod-active-1.
	n, err := repo.GetProductStoreCount(ctx, "prod-active-1")
	if err != nil || n != 2 {
		t.Fatalf("storeCount = %d (err %v), want 2", n, err)
	}
	// D39: categories carry parentId and include the inactive cat-gold.
	cats, err := repo.GetCategories(ctx)
	if err != nil || len(cats) != 3 {
		t.Fatalf("GetCategories = %d (err %v), want 3 incl. inactive", len(cats), err)
	}
	for _, c := range cats {
		if c.ID == "cat-dk-1" && (c.ParentID == nil || *c.ParentID != "cat-digital") {
			t.Fatalf("cat-dk-1 parentId = %v, want cat-digital", c.ParentID)
		}
		if c.ID == "cat-gold" && c.ParentID != nil {
			t.Fatalf("cat-gold parentId = %v, want nil", c.ParentID)
		}
	}
	// Category JSON shape: exactly the 7 live keys.
	b, _ := json.Marshal(cats[0])
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	if len(m) != 7 {
		t.Fatalf("category JSON keys = %v, want exactly 7", m)
	}
	for _, k := range []string{"id", "title", "slug", "iconName", "gradient", "imageUrl", "parentId"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("category JSON missing key %q", k)
		}
	}
	// D43: search logging writes the normalised term + catalog count,
	// and the ensure step added catalog_result_count.
	if err := repo.LogSearchEvent(ctx, "طلا", "central", 1, 1); err != nil {
		t.Fatalf("LogSearchEvent: %v", err)
	}
	var term string
	var rc, crc int
	if err := db.QueryRow(`SELECT term_normalized, result_count, catalog_result_count FROM search_queries ORDER BY id DESC LIMIT 1`).Scan(&term, &rc, &crc); err != nil || term != "طلا" || rc != 1 || crc != 1 {
		t.Fatalf("search_queries row = (%q, %d, %d) err %v", term, rc, crc, err)
	}
	// D43 rollup aggregates without writing anywhere (TODO U73).
	aggs, err := repo.RollupSearchQueries(ctx)
	if err != nil || len(aggs) != 1 || aggs[0].Searches != 1 {
		t.Fatalf("RollupSearchQueries = %+v (err %v), want 1 aggregate row", aggs, err)
	}
	// D40: dk facets from the tables.
	brands, err := repo.GetBrands(ctx)
	if err != nil || len(brands) != 1 || brands[0].TitleFa != "سامسونگ" {
		t.Fatalf("GetBrands = %+v (err %v)", brands, err)
	}
	catBrands, err := repo.GetCategoryBrandsBySlug(ctx, "mobile-phone")
	if err != nil || len(catBrands) != 1 {
		t.Fatalf("GetCategoryBrandsBySlug = %+v (err %v)", catBrands, err)
	}
	attrs, err := repo.GetCategoryAttributesBySlug(ctx, "mobile-phone")
	if err != nil || len(attrs) != 1 || len(attrs[0].Values) != 1 || attrs[0].Values[0].TitleFa != "Android" {
		t.Fatalf("GetCategoryAttributesBySlug = %+v (err %v)", attrs, err)
	}
	found, err := repo.SearchCategories(ctx, "موبایل")
	if err != nil || len(found) != 1 || found[0].ID != "cat-dk-1" {
		t.Fatalf("SearchCategories = %+v (err %v)", found, err)
	}
	if empty, _ := repo.SearchCategories(ctx, ""); len(empty) != 0 {
		t.Fatalf("SearchCategories(\"\") = %d, want empty", len(empty))
	}
	// D61: dossier serves dims, height, warranty, shipping mode.
	dossier, err := repo.GetCatalogReviewDossier(ctx, "prod-active-1")
	if err != nil || dossier == nil {
		t.Fatalf("GetCatalogReviewDossier = %+v (err %v)", dossier, err)
	}
	if dossier.ShippingMode != "central-warehouse" || dossier.Warranty != "۱۸ ماه گارانتی شرکتی" {
		t.Fatalf("dossier shipping/warranty = %q/%q", dossier.ShippingMode, dossier.Warranty)
	}
	if dossier.ProductHeightMm == nil || *dossier.ProductHeightMm != 40 || dossier.VariantCount != 2 {
		t.Fatalf("dossier height/variants = %v/%d", dossier.ProductHeightMm, dossier.VariantCount)
	}
	if dossier.ApprovalNote == "" {
		t.Fatalf("dossier approval note empty")
	}
	if missing, err := repo.GetCatalogReviewDossier(ctx, "no-such-id"); err != nil || missing != nil {
		t.Fatalf("GetCatalogReviewDossier unknown = %+v (err %v), want nil", missing, err)
	}
	_ = domain.Product{}
}

func idsOf(products []domain.Product) []string {
	out := []string{}
	for _, p := range products {
		out = append(out, p.ID)
	}
	return out
}
