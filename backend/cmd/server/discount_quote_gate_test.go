package main

// Discount-quote assortment-gate regression tests. These run ONLY
// when TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local
// Postgres (never the live database); otherwise they skip.
//
// Bug covered: POST /api/v1/stores/<dropshipper>/discount-quote
// priced ANY product under any store slug, including products the
// store does not publish — a cross-store price disclosure (a quote
// for another store's product returned that store's resolution).
// Order creation already rejects such items with 400 "Product %s
// is not published by this store"; the quote now applies the
// identical gate (and the identical "Product %s not found" 400
// for unknown product ids, where it previously skipped silently),
// so a quote stays a faithful predictor of checkout.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func discountQuoteGateRepos(t *testing.T) (*repository.CatalogRepository, *repository.StoreRepository) {
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
		`DROP TABLE IF EXISTS search_queries, store_products, store_product_overrides, store_niche_overrides, stores, product_images, product_variant_images, product_variants, product_niches, pending_approvals, products CASCADE`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100), preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50), supply_option VARCHAR(50), is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'x', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, sale_price BIGINT, sale_from TIMESTAMPTZ, sale_to TIMESTAMPTZ, shipping_class TEXT, tax_class TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_images (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_variant_images (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), variant_id UUID NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper', owner_name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id VARCHAR(50) NOT NULL DEFAULT '', referral_code VARCHAR(100) NOT NULL DEFAULT '', referrer_seller_id VARCHAR(50), signed_contract_version INT NOT NULL DEFAULT 0, custom_domain VARCHAR(255) NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, template_id VARCHAR(100) NOT NULL DEFAULT '', template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '', panel_config JSONB, tagline TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', status VARCHAR(50) NOT NULL DEFAULT 'active', active_categories TEXT[] NOT NULL DEFAULT '{}', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_product_overrides (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE store_niche_overrides (store_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE search_queries (id BIGSERIAL PRIMARY KEY, term_normalized TEXT NOT NULL, store_slug TEXT NOT NULL, result_count INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE pending_approvals (id VARCHAR(50) PRIMARY KEY, entity_type VARCHAR(50) NOT NULL, target_id VARCHAR(50) NOT NULL, title TEXT NOT NULL, requested_data JSONB NOT NULL DEFAULT '{}', requested_by VARCHAR(50) NOT NULL, status VARCHAR(50) NOT NULL, rejection_reason TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), reviewed_at TIMESTAMPTZ)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	seed := []string{
		`INSERT INTO stores (id,name,slug,owner_type,panel_config,seller_deals_widget_config) VALUES ('store-dropq','فروشگاه تست','dropq','dropshipper','{}'::jsonb,'null'::jsonb)`,
		`INSERT INTO stores (id,name,slug,owner_type,panel_config,seller_deals_widget_config) VALUES ('store-central','مرکزی','central','central','{}'::jsonb,'null'::jsonb)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('prod-pub','کالای منتشرشده','pub-product','beauty','sup-1',12000000,10000000,0,TRUE,5,'img','desc','انبار',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'برند',40000001)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number) VALUES ('prod-unpub','کالای بیگانه','unpub-product','beauty','sup-1',24000000,20000000,0,TRUE,5,'img','desc','انبار',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'برند',40000002)`,
		// Only prod-pub is in the dropshipper store's assortment.
		`INSERT INTO store_products VALUES ('store-dropq','prod-pub',0)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
	return repository.NewCatalogRepository(db), repository.NewStoreRepository(db)
}

func discountQuotePost(t *testing.T, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, slug, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/"+slug+"/discount-quote", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleDiscountQuote(rec, req, catalogRepo, storeRepo, nil, slug)
	return rec
}

// A dropshipper quote for a product the store does not publish is
// rejected with the exact order-creation 400, not priced.
func TestDiscountQuoteGateRejectsUnpublishedForDropshipper(t *testing.T) {
	catalogRepo, storeRepo := discountQuoteGateRepos(t)
	rec := discountQuotePost(t, catalogRepo, storeRepo, "dropq", `{"items":[{"productId":"prod-unpub","quantity":1}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
	want := `{"error":"Product prod-unpub is not published by this store"}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// A published product quotes exactly as before the gate: the
// resolver price for this store, no discounts configured.
func TestDiscountQuoteGatePublishedProductUnchanged(t *testing.T) {
	catalogRepo, storeRepo := discountQuoteGateRepos(t)
	rec := discountQuotePost(t, catalogRepo, storeRepo, "dropq", `{"items":[{"productId":"prod-pub","quantity":2}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	ctx := context.Background()
	store, err := storeRepo.GetStoreBySlug(ctx, "dropq")
	if err != nil || store == nil {
		t.Fatalf("load store: %v", err)
	}
	p, err := catalogRepo.GetProductByID(ctx, "prod-pub")
	if err != nil || p == nil {
		t.Fatalf("load product: %v", err)
	}
	want := pricing.ResolveReferencePricing(*p, *store, pricing.DefaultPlatformPricingSettings(), nil).FinalPrice * 2
	var quote struct {
		PayableAmount   int64 `json:"payableAmount"`
		OriginalPayable int64 `json:"originalPayable"`
		SellerDiscount  int64 `json:"sellerDiscount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &quote); err != nil {
		t.Fatalf("decode quote: %v", err)
	}
	if quote.PayableAmount != want || quote.OriginalPayable != want || quote.SellerDiscount != 0 {
		t.Fatalf("quote = %+v, want payable/original %d and no discount", quote, want)
	}
}

// Central stores are exempt from the assortment gate, exactly as
// in order creation: any product quotes.
func TestDiscountQuoteGateCentralExempt(t *testing.T) {
	catalogRepo, storeRepo := discountQuoteGateRepos(t)
	rec := discountQuotePost(t, catalogRepo, storeRepo, "central", `{"items":[{"productId":"prod-unpub","quantity":1}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", rec.Code, rec.Body.String())
	}
	var quote struct {
		PayableAmount int64 `json:"payableAmount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &quote); err != nil {
		t.Fatalf("decode quote: %v", err)
	}
	if quote.PayableAmount <= 0 {
		t.Fatalf("payable = %d, want a positive central price", quote.PayableAmount)
	}
}

// Unknown product ids now fail the quote with order creation's
// 400 instead of being silently skipped.
func TestDiscountQuoteGateUnknownProduct(t *testing.T) {
	catalogRepo, storeRepo := discountQuoteGateRepos(t)
	rec := discountQuotePost(t, catalogRepo, storeRepo, "dropq", `{"items":[{"productId":"prod-ghost","quantity":1}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", rec.Code, rec.Body.String())
	}
	want := `{"error":"Product prod-ghost not found"}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}
