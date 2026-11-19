package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

// P1/P2 handler tests (2026-11-13). Pure tests run always; DB tests
// run ONLY when TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local
// Postgres (same convention as the Team 04 suite).

func p1p2Res(finalPrice, originalPrice int64) pricing.ReferenceResolution {
	return pricing.ReferenceResolution{FinalPrice: finalPrice, OriginalPrice: originalPrice}
}

func p1p2Variant(id string, isDefault, isActive bool, stock int, cost *int64) domain.ProductVariant {
	return domain.ProductVariant{
		ID: id, ProductID: "prod-1", Spnc: "SPNC-" + id, Name: "v-" + id,
		InventoryMode: "tracked", IsActive: isActive, IsDefault: isDefault,
		Stock: stock, SupplierBaseCost: cost, MinCartQty: 1,
		Selection: map[string]string{}, Attributes: map[string]interface{}{},
	}
}

func TestP1ARangeMultiVariant(t *testing.T) {
	p := domain.Product{ID: "prod-1", SupplierBaseCost: 1000000, CategorySlug: "beauty"}
	variants := []domain.ProductVariant{
		p1p2Variant("v1", true, true, 5, nil),
		p1p2Variant("v2", false, true, 5, func() *int64 { c := int64(1500000); return &c }()),
	}
	prices := map[string]VariantPriceResponse{
		"v1": {Price: 1100000, OriginalPrice: 1100000},
		"v2": {Price: 1650000, OriginalPrice: 1650000},
	}
	out := buildProductResponse(p, p1p2Res(1100000, 1250000), variants, prices)
	if out.PriceMin == nil || *out.PriceMin != 1100000 || out.PriceMax == nil || *out.PriceMax != 1650000 {
		t.Fatalf("range = %v/%v, want 1100000/1650000", out.PriceMin, out.PriceMax)
	}
	if out.Price != 1100000 {
		t.Fatalf("card Price = %d, want unchanged 1100000", out.Price)
	}
}

func TestP1ARangeEdgeCases(t *testing.T) {
	p := domain.Product{ID: "prod-1", SupplierBaseCost: 1000000}
	// Single variant: min == max == variant price.
	out := buildProductResponse(p, p1p2Res(1100000, 1100000), []domain.ProductVariant{p1p2Variant("v1", true, true, 5, nil)}, map[string]VariantPriceResponse{"v1": {Price: 1100000}})
	if out.PriceMin == nil || *out.PriceMin != 1100000 || out.PriceMax == nil || *out.PriceMax != 1100000 {
		t.Fatalf("single-variant range = %v/%v", out.PriceMin, out.PriceMax)
	}
	// All variants inactive: omitted (not 0, not product price).
	inactive := []domain.ProductVariant{p1p2Variant("v1", true, false, 5, nil)}
	out = buildProductResponse(p, p1p2Res(1100000, 1100000), inactive, map[string]VariantPriceResponse{"v1": {Price: 1100000}})
	if out.PriceMin != nil || out.PriceMax != nil {
		t.Fatalf("all-inactive range = %v/%v, want nil/nil", out.PriceMin, out.PriceMax)
	}
	// Legacy inactive variant with a priced entry: excluded.
	legacy := p1p2Variant("v1", true, false, 5, nil)
	legacy.InventoryMode = "legacy"
	out = buildProductResponse(p, p1p2Res(1100000, 1100000), []domain.ProductVariant{legacy}, map[string]VariantPriceResponse{"v1": {Price: 1100000}})
	if out.PriceMin != nil || out.PriceMax != nil {
		t.Fatalf("legacy-inactive range = %v/%v, want nil/nil", out.PriceMin, out.PriceMax)
	}
	// Zero-variant legacy product: both = product-level price.
	out = buildProductResponse(p, p1p2Res(880000, 1000000), nil, nil)
	if out.PriceMin == nil || *out.PriceMin != 880000 || out.PriceMax == nil || *out.PriceMax != 880000 {
		t.Fatalf("zero-variant range = %v/%v, want 880000/880000", out.PriceMin, out.PriceMax)
	}
	// Variable product with no priced variants: omitted.
	out = buildProductResponse(p, p1p2Res(1100000, 1100000), []domain.ProductVariant{p1p2Variant("v1", true, true, 5, nil)}, map[string]VariantPriceResponse{})
	if out.PriceMin != nil || out.PriceMax != nil {
		t.Fatalf("unpriced range = %v/%v, want nil/nil", out.PriceMin, out.PriceMax)
	}
	// Zero-stock variant still counts (stock is NOT a filter).
	zeroStock := p1p2Variant("v2", false, true, 0, func() *int64 { c := int64(1500000); return &c }())
	out = buildProductResponse(p, p1p2Res(1100000, 1100000), []domain.ProductVariant{p1p2Variant("v1", true, true, 5, nil), zeroStock}, map[string]VariantPriceResponse{"v1": {Price: 1100000}, "v2": {Price: 1650000}})
	if out.PriceMax == nil || *out.PriceMax != 1650000 {
		t.Fatalf("zero-stock variant excluded from range: max=%v", out.PriceMax)
	}
}

func TestP2BEffectiveClassesInResponse(t *testing.T) {
	p := domain.Product{ID: "prod-1", SupplierBaseCost: 1000000, ShippingClass: "fragile", TaxClass: "reduced"}
	v := p1p2Variant("v1", true, true, 5, nil)
	out := buildProductResponse(p, p1p2Res(1100000, 1100000), []domain.ProductVariant{v}, map[string]VariantPriceResponse{"v1": {Price: 1100000}})
	if out.Variants[0].ShippingClass != "fragile" || out.Variants[0].TaxClass != "reduced" {
		t.Fatalf("inherited classes = %q/%q, want fragile/reduced", out.Variants[0].ShippingClass, out.Variants[0].TaxClass)
	}
	heavy := "heavy"
	v.ShippingClass = &heavy
	out = buildProductResponse(p, p1p2Res(1100000, 1100000), []domain.ProductVariant{v}, map[string]VariantPriceResponse{"v1": {Price: 1100000}})
	if out.Variants[0].ShippingClass != "heavy" {
		t.Fatalf("variant class override = %q, want heavy", out.Variants[0].ShippingClass)
	}
	p2 := domain.Product{ID: "prod-1", SupplierBaseCost: 1000000}
	out = buildProductResponse(p2, p1p2Res(1100000, 1100000), []domain.ProductVariant{p1p2Variant("v1", true, true, 5, nil)}, map[string]VariantPriceResponse{"v1": {Price: 1100000}})
	if out.Variants[0].ShippingClass != "standard" || out.Variants[0].TaxClass != "standard" {
		t.Fatalf("empty classes = %q/%q, want standard/standard", out.Variants[0].ShippingClass, out.Variants[0].TaxClass)
	}
}

func TestP2CStoreDefaultInResponse(t *testing.T) {
	p := domain.Product{ID: "prod-1", SupplierBaseCost: 1000000}
	variants := []domain.ProductVariant{p1p2Variant("v1", true, true, 5, nil), p1p2Variant("v2", false, true, 5, nil)}
	prices := map[string]VariantPriceResponse{"v1": {Price: 1100000}, "v2": {Price: 1650000}}
	out := buildProductResponse(p, p1p2Res(1100000, 1100000), variants, prices, "v2")
	if out.StoreDefaultVariantID == nil || *out.StoreDefaultVariantID != "v2" {
		t.Fatalf("storeDefaultVariantId = %v, want v2", out.StoreDefaultVariantID)
	}
	// Unknown ID is not advertised (hidden/inactive default falls through).
	out = buildProductResponse(p, p1p2Res(1100000, 1100000), variants, prices, "v9")
	if out.StoreDefaultVariantID != nil {
		t.Fatalf("unknown store default advertised: %v", out.StoreDefaultVariantID)
	}
	out = buildProductResponse(p, p1p2Res(1100000, 1100000), variants, prices)
	if out.StoreDefaultVariantID != nil {
		t.Fatalf("no store default must be omitted")
	}
	if got := StoreDefaultVariantForResponse(variants, "v2"); got == nil || got.ID != "v2" {
		t.Fatalf("StoreDefaultVariantForResponse = %v", got)
	}
	if got := StoreDefaultVariantForResponse(variants, "v9"); got != nil {
		t.Fatalf("StoreDefaultVariantForResponse unknown = %v", got)
	}
}

func TestP1CSaleInDisplayPricing(t *testing.T) {
	p := domain.Product{ID: "prod-1", SupplierBaseCost: 1000000, OriginalPrice: 1250000, CategorySlug: "beauty"}
	central := domain.Store{ID: "store-central", Slug: "central", OwnerType: "central"}
	premium := p1p2Variant("v2", false, true, 5, func() *int64 { c := int64(1500000); return &c }())
	// Active sale between R (1,500,000) and P (1,650,000).
	s := int64(1575000)
	premium.SalePrice = &s
	prices := priceVariantsForResponse(p, []domain.ProductVariant{premium}, central, nil, nil, nil, nil)
	vp := prices["v2"]
	if vp.Price != 1575000 || !vp.OnSale || vp.OriginalPrice != 1650000 || vp.SalePrice == nil || *vp.SalePrice != s {
		t.Fatalf("active sale price = %+v, want Price 1575000 OnSale true Original 1650000", vp)
	}
	if vp.DiscountPercent != 5 {
		t.Fatalf("sale discountPercent = %d, want 5", vp.DiscountPercent)
	}
	// Sale below R suspends (cost rose after the sale was set).
	s = int64(1400000)
	premium.SalePrice = &s
	vp = priceVariantsForResponse(p, []domain.ProductVariant{premium}, central, nil, nil, nil, nil)["v2"]
	if vp.Price != 1650000 || vp.OnSale {
		t.Fatalf("sale < R must suspend: %+v", vp)
	}
	// Sale >= P is a no-op.
	s = int64(1650000)
	premium.SalePrice = &s
	vp = priceVariantsForResponse(p, []domain.ProductVariant{premium}, central, nil, nil, nil, nil)["v2"]
	if vp.Price != 1650000 || vp.OnSale {
		t.Fatalf("sale >= P must be a no-op: %+v", vp)
	}
	// Window: future start => inactive.
	s = int64(1575000)
	premium.SalePrice = &s
	from := time.Now().Add(time.Hour)
	premium.SaleFrom = &from
	vp = priceVariantsForResponse(p, []domain.ProductVariant{premium}, central, nil, nil, nil, nil)["v2"]
	if vp.Price != 1650000 || vp.OnSale {
		t.Fatalf("future saleFrom must be inactive: %+v", vp)
	}
}

// --- DB-backed handler tests ---

func p1p2HandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB P1/P2 handler test")
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
		`DROP TABLE IF EXISTS store_variant_defaults, store_variant_visibility, store_margin_locks, store_products, store_product_overrides, store_niche_overrides, product_variant_images, product_variants, product_niches, products, stores CASCADE`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', slug VARCHAR(255) NOT NULL DEFAULT '', owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper', owner_name VARCHAR(255) NOT NULL DEFAULT '', owner_user_id VARCHAR(50), profit_margin_percent DOUBLE PRECISION NOT NULL DEFAULT 0, custom_store_margin_override DOUBLE PRECISION, is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE, active_plan_id VARCHAR(50) NOT NULL DEFAULT '', referral_code VARCHAR(100) NOT NULL DEFAULT '', referrer_seller_id VARCHAR(50), signed_contract_version INT NOT NULL DEFAULT 0, custom_domain VARCHAR(255) NOT NULL DEFAULT '', total_sales BIGINT NOT NULL DEFAULT 0, seller_deals_widget_config JSONB, template_id VARCHAR(100) NOT NULL DEFAULT '', template_config JSONB NOT NULL DEFAULT '{}'::jsonb, logo TEXT NOT NULL DEFAULT '', panel_config JSONB, tagline TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '', status VARCHAR(50) NOT NULL DEFAULT 'active', active_categories TEXT[] NOT NULL DEFAULT '{}', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100), preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50), supply_option VARCHAR(50), is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'پیش‌فرض', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, selection JSONB NOT NULL DEFAULT '{}', combination_key TEXT, inventory_mode TEXT NOT NULL DEFAULT 'legacy', is_active BOOLEAN NOT NULL DEFAULT TRUE, min_cart_qty INT NOT NULL DEFAULT 1, max_cart_qty INT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, sale_price BIGINT, sale_from TIMESTAMPTZ, sale_to TIMESTAMPTZ, shipping_class TEXT, tax_class TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_variant_images (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), variant_id UUID NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE store_product_overrides (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE store_niche_overrides (store_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, override_value DOUBLE PRECISION NOT NULL DEFAULT 0, mode VARCHAR(20) NOT NULL DEFAULT 'percent')`,
		`CREATE TABLE store_variant_visibility (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, variant_id UUID NOT NULL, is_visible BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id, variant_id))`,
		`CREATE TABLE store_variant_defaults (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, variant_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE store_margin_locks (store_id VARCHAR(50) NOT NULL, scope VARCHAR(20) NOT NULL, scope_id VARCHAR(100) NOT NULL, lock_mode VARCHAR(20) NOT NULL, lock_value DOUBLE PRECISION, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	seed := []string{
		`INSERT INTO stores (id,name,slug,owner_type,owner_user_id,panel_config) VALUES ('store-central','مرکزی','central','central',NULL,'{}'::jsonb)`,
		`INSERT INTO stores (id,name,slug,owner_type,owner_user_id,panel_config) VALUES ('store-a','Shop A','shop-a','dropshipper','seller-user-1','{}'::jsonb)`,
		`INSERT INTO stores (id,name,slug,owner_type,owner_user_id,panel_config) VALUES ('store-b','Shop B','shop-b','dropshipper','seller-user-2','{}'::jsonb)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,warehouse_name,description,supplier_warranty,spn_number) VALUES ('prod-1','Test product','test-product','beauty','sup-1',1875000,1000000,'','','',10000001)`,
		`INSERT INTO product_variants (id,product_id,spnc,variant_name,is_default,stock,supplier_base_cost,is_active,created_at) VALUES ('11111111-1111-1111-1111-111111111111','prod-1','AAAAAAAAAAAA','cheap',TRUE,5,NULL,TRUE,NOW())`,
		`INSERT INTO product_variants (id,product_id,spnc,variant_name,is_default,stock,supplier_base_cost,is_active,created_at) VALUES ('22222222-2222-2222-2222-222222222222','prod-1','BBBBBBBBBBBB','premium',FALSE,5,1500000,TRUE,NOW())`,
		`INSERT INTO store_products VALUES ('store-a','prod-1',0)`,
		`INSERT INTO store_products VALUES ('store-b','prod-1',0)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
	return db
}

func TestP1CSaleEndpointValidation(t *testing.T) {
	db := p1p2HandlerDB(t)
	ctx := context.Background()
	supplierRepo := repository.NewSupplierRepository(db)
	pricingRepo := repository.NewPricingRepository(db)
	h := NewSupplierHandlers(supplierRepo, nil, false).WithPricingRepository(pricingRepo)
	// Central: cheap variant cost 1,000,000 => R=1,000,000, P=1,100,000.
	// Premium cost 1,500,000 => R=1,500,000, P=1,650,000.
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/supplier/products/prod-1/variants/22222222-2222-2222-2222-222222222222/sale", strings.NewReader(body))
		req = req.WithContext(context.WithValue(ctx, supplierIDKey, "sup-1"))
		rec := httptest.NewRecorder()
		h.HandleUpdateVariantSale(rec, req, "prod-1", "22222222-2222-2222-2222-222222222222")
		return rec
	}
	// Valid sale.
	rec := call(`{"salePrice":1575000,"saleTo":"2030-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid sale status = %d body %s", rec.Code, rec.Body.String())
	}
	var out repository.SupplierProductVariant
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.SalePrice == nil || *out.SalePrice != 1575000 {
		t.Fatalf("valid sale body = %s", rec.Body.String())
	}
	// S <= R rejected; S >= P_central rejected; S % 100 rejected;
	// past saleTo rejected; from >= to rejected.
	for _, body := range []string{
		`{"salePrice":1500000}`, // == R
		`{"salePrice":1400000}`, // < R
		`{"salePrice":1650000}`, // == P_central
		`{"salePrice":1700000}`, // > P_central
		`{"salePrice":1575050}`, // not %100
		`{"salePrice":1575000,"saleTo":"2020-01-01T00:00:00Z"}`,
		`{"salePrice":1575000,"saleFrom":"2030-02-01T00:00:00Z","saleTo":"2030-01-01T00:00:00Z"}`,
	} {
		if rec := call(body); rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400 (body %s)", body, rec.Code, rec.Body.String())
		}
	}
	// Admin-locked variant => 409.
	if _, err := db.ExecContext(ctx, `INSERT INTO store_margin_locks (store_id,scope,scope_id,lock_mode,lock_value) VALUES ('store-a','variant','22222222-2222-2222-2222-222222222222','admin_set',25)`); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	if rec := call(`{"salePrice":1575000}`); rec.Code != http.StatusConflict {
		t.Fatalf("locked variant: status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM store_margin_locks`); err != nil {
		t.Fatalf("clear locks: %v", err)
	}
	// Clear the sale.
	rec = call(`{"salePrice":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear sale status = %d body %s", rec.Code, rec.Body.String())
	}
	var cleared repository.SupplierProductVariant
	if err := json.Unmarshal(rec.Body.Bytes(), &cleared); err != nil || cleared.SalePrice != nil || cleared.SaleFrom != nil || cleared.SaleTo != nil {
		t.Fatalf("sale not cleared: %s", rec.Body.String())
	}
	// Another supplier's variant => 404.
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"salePrice":1575000}`))
	req = req.WithContext(context.WithValue(ctx, supplierIDKey, "sup-other"))
	rec = httptest.NewRecorder()
	h.HandleUpdateVariantSale(rec, req, "prod-1", "22222222-2222-2222-2222-222222222222")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign supplier: status = %d, want 404", rec.Code)
	}
}

func TestP2SellerVisibilityAndDefaultEndpoints(t *testing.T) {
	db := p1p2HandlerDB(t)
	ctx := context.Background()
	catalogRepo := repository.NewCatalogRepository(db)
	storeRepo := repository.NewStoreRepository(db)
	claims := func(userID string) context.Context {
		return context.WithValue(ctx, userClaimsKey, &auth.UserClaims{UserID: userID, Role: "seller"})
	}
	// Hide premium in shop-a.
	req := httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"isVisible":false}`))
	req = req.WithContext(claims("seller-user-1"))
	rec := httptest.NewRecorder()
	handleSellerStoreVariantVisibility(rec, req, storeRepo, catalogRepo, "shop-a", "prod-1", "22222222-2222-2222-2222-222222222222")
	if rec.Code != http.StatusOK {
		t.Fatalf("hide status = %d body %s", rec.Code, rec.Body.String())
	}
	// Store view now lists only the cheap variant, with the range
	// collapsing to it; central still sees both.
	list := func(slug string) []map[string]json.RawMessage {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/products?storeSlug="+slug, nil)
		w := httptest.NewRecorder()
		handleProductsList(w, r, ctx, catalogRepo, storeRepo, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("list %s status = %d body %s", slug, w.Code, w.Body.String())
		}
		// PERF-01: paged envelope; the bare-array shape is retired.
		var envelope struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("list %s decode: %v", slug, err)
		}
		return envelope.Items
	}
	shopA := list("shop-a")
	if len(shopA) != 1 {
		t.Fatalf("shop-a list len = %d, want 1", len(shopA))
	}
	var variants []map[string]json.RawMessage
	_ = json.Unmarshal(shopA[0]["variants"], &variants)
	if len(variants) != 1 {
		t.Fatalf("shop-a variants = %d, want 1 (hidden premium absent)", len(variants))
	}
	var priceMin, priceMax int64
	_ = json.Unmarshal(shopA[0]["priceMin"], &priceMin)
	_ = json.Unmarshal(shopA[0]["priceMax"], &priceMax)
	// Cheap variant at shop-a max-default margin: cost 1,000,000
	// x 1.35 = 1,350,000 — the range must reflect the STORE price.
	if priceMin != 1350000 || priceMax != 1350000 {
		t.Fatalf("shop-a range = %d/%d, want 1350000/1350000 (store margin)", priceMin, priceMax)
	}
	central := list("central")
	_ = json.Unmarshal(central[0]["variants"], &variants)
	if len(variants) != 2 {
		t.Fatalf("central variants = %d, want 2", len(variants))
	}
	// Non-owner cannot toggle; central toggle => 403.
	req = httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"isVisible":false}`))
	req = req.WithContext(claims("seller-user-2"))
	rec = httptest.NewRecorder()
	handleSellerStoreVariantVisibility(rec, req, storeRepo, catalogRepo, "shop-a", "prod-1", "22222222-2222-2222-2222-222222222222")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-owner hide status = %d, want 403", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"isVisible":false}`))
	req = req.WithContext(claims("seller-user-1"))
	rec = httptest.NewRecorder()
	handleSellerStoreVariantVisibility(rec, req, storeRepo, catalogRepo, "central", "prod-1", "22222222-2222-2222-2222-222222222222")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("central hide status = %d, want 403", rec.Code)
	}
	// A hidden variant cannot become the store default (422)...
	req = httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"variantId":"22222222-2222-2222-2222-222222222222"}`))
	req = req.WithContext(claims("seller-user-1"))
	rec = httptest.NewRecorder()
	handleSellerStoreVariantDefault(rec, req, storeRepo, catalogRepo, "shop-a", "prod-1")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("hidden default status = %d, want 422 (body %s)", rec.Code, rec.Body.String())
	}
	// ...but the visible cheap variant can; the detail then carries it.
	req = httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"variantId":"11111111-1111-1111-1111-111111111111"}`))
	req = req.WithContext(claims("seller-user-1"))
	rec = httptest.NewRecorder()
	handleSellerStoreVariantDefault(rec, req, storeRepo, catalogRepo, "shop-a", "prod-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("set default status = %d body %s", rec.Code, rec.Body.String())
	}
	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/products/prod-1?storeSlug=shop-a", nil)
	detailRec := httptest.NewRecorder()
	handleProductDetail(detailRec, detailReq, ctx, catalogRepo, storeRepo, nil, "prod-1")
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body %s", detailRec.Code, detailRec.Body.String())
	}
	var detail map[string]json.RawMessage
	_ = json.Unmarshal(detailRec.Body.Bytes(), &detail)
	var defID string
	_ = json.Unmarshal(detail["storeDefaultVariantId"], &defID)
	if defID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("detail storeDefaultVariantId = %q", defID)
	}
	// DELETE clears it.
	req = httptest.NewRequest(http.MethodDelete, "/x", nil)
	req = req.WithContext(claims("seller-user-1"))
	rec = httptest.NewRecorder()
	handleSellerStoreVariantDefault(rec, req, storeRepo, catalogRepo, "shop-a", "prod-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("clear default status = %d body %s", rec.Code, rec.Body.String())
	}
}

func TestP1CDisplayEqualsChargeQuote(t *testing.T) {
	db := p1p2HandlerDB(t)
	ctx := context.Background()
	catalogRepo := repository.NewCatalogRepository(db)
	storeRepo := repository.NewStoreRepository(db)
	// Active sale on the premium variant (central): R=1,500,000,
	// P=1,650,000, S=1,575,000.
	if _, err := db.ExecContext(ctx, `UPDATE product_variants SET sale_price=1575000, sale_from=NOW() - INTERVAL '1 hour', sale_to=NOW() + INTERVAL '1 day' WHERE id='22222222-2222-2222-2222-222222222222'`); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	// Display path.
	p, err := catalogRepo.GetProductByID(ctx, "prod-1")
	if err != nil || p == nil {
		t.Fatalf("get product: %v", err)
	}
	central := domain.Store{ID: "store-central", Slug: "central", OwnerType: "central"}
	variants, _ := catalogRepo.GetProductVariants(ctx, "prod-1")
	display := priceVariantsForResponse(*p, variants, central, nil, nil, nil, nil)
	if display["22222222-2222-2222-2222-222222222222"].Price != 1575000 {
		t.Fatalf("display premium = %+v, want 1575000", display["22222222-2222-2222-2222-222222222222"])
	}
	// Charge path (discount quote, no code, central store).
	body := `{"items":[{"productId":"prod-1","variantId":"22222222-2222-2222-2222-222222222222","quantity":2}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/stores/central/discount-quote", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handleDiscountQuote(rec, req, catalogRepo, storeRepo, nil, "central")
	if rec.Code != http.StatusOK {
		t.Fatalf("quote status = %d body %s", rec.Code, rec.Body.String())
	}
	var quote struct {
		Payable int64 `json:"payableAmount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &quote); err != nil || quote.Payable != 3150000 {
		t.Fatalf("quote payable = %+v, want 3150000 (2 x sale 1575000), body %s", quote, rec.Body.String())
	}
	// And a hidden variant fails the quote for that store.
	if _, err := db.ExecContext(ctx, `INSERT INTO store_variant_visibility (store_id,product_id,variant_id,is_visible) VALUES ('store-a','prod-1','22222222-2222-2222-2222-222222222222',FALSE)`); err != nil {
		t.Fatalf("seed hide: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/stores/shop-a/discount-quote", strings.NewReader(body))
	rec = httptest.NewRecorder()
	handleDiscountQuote(rec, req, catalogRepo, storeRepo, nil, "shop-a")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("hidden-variant quote status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}
