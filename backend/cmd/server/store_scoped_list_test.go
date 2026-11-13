package main

// Store-scoped product list regression tests. These run ONLY when
// TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres
// (never the live database); otherwise they skip.
//
// Bug covered: GET /api/v1/products/?storeSlug=<dropshipper> returned
// the globally-active catalog priced for the store, NOT the store's
// own assortment. A store whose picks are old (or globally inactive)
// got back a page of unrelated newer products while its own products
// were dropped by the global is_active gate — the storefront then
// intersected with activeProductIds and rendered a single product.
// The fix scopes the listing to the store's store_products set for
// non-central dropshipper store views; unscoped and central listings
// keep the exact previous behaviour.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

func storeScopedRepos(t *testing.T) (*repository.CatalogRepository, *repository.StoreRepository) {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB HTTP test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	ddl := []string{
		`DROP TABLE IF EXISTS search_queries, store_products, store_product_overrides, store_niche_overrides, stores, product_images, product_variant_images, product_variants, product_niches, pending_approvals, products, store_variant_visibility, store_variant_defaults CASCADE`,
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
		`CREATE TABLE store_variant_visibility (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, variant_id UUID NOT NULL, is_visible BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id, variant_id))`,
		`CREATE TABLE store_variant_defaults (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, variant_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE pending_approvals (id VARCHAR(50) PRIMARY KEY, entity_type VARCHAR(50) NOT NULL, target_id VARCHAR(50) NOT NULL, title TEXT NOT NULL, requested_data JSONB NOT NULL DEFAULT '{}', requested_by VARCHAR(50) NOT NULL, status VARCHAR(50) NOT NULL, rejection_reason TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), reviewed_at TIMESTAMPTZ)`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	product := `INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,created_at) VALUES ($1,$2,$3,'beauty','sup-1',$4,$5,0,TRUE,3,'img','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',$6,'نشر دمو',$7,$8)`
	seedProducts := []struct {
		id, title, slug string
		original, cost  int64
		active          bool
		spn             int64
		created         string
	}{
		// The store's own picks: old, two of them globally inactive.
		{"p-old-1", "سرم قدیمی فروشگاه", "old-serum", 20000000, 10000000, false, 20000001, "2025-01-01T00:00:00Z"},
		{"p-old-2", "کرم قدیمی فروشگاه", "old-cream", 30000000, 20000000, false, 20000002, "2025-01-02T00:00:00Z"},
		{"p-old-3", "روغن قدیمی فروشگاه", "old-oil", 9000000, 5000000, true, 20000003, "2025-01-03T00:00:00Z"},
		// Inactive and NOT in the store: must never appear anywhere.
		{"p-outsider-inactive", "کالای غیرفعال بیگانه", "outsider-inactive", 9000000, 7000000, false, 20000004, "2025-06-01T00:00:00Z"},
	}
	for _, sp := range seedProducts {
		if _, err := db.Exec(product, sp.id, sp.title, sp.slug, sp.original, sp.cost, sp.active, sp.spn, sp.created); err != nil {
			t.Fatalf("seed product %s: %v", sp.id, err)
		}
	}
	// A large NEWER unrelated active catalog (the "democat" flood).
	for i := 1; i <= 45; i++ {
		id := fmt.Sprintf("noise-%02d", i)
		if _, err := db.Exec(product, id, "کالای کاتالوگ "+id, "slug-"+id, 2000000, 1000000, true, 30000000+i, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatalf("seed noise %s: %v", id, err)
		}
	}
	seed := []string{
		`INSERT INTO stores (id,name,slug,owner_type,panel_config,seller_deals_widget_config) VALUES ('store-shopx','فروشگاه ایکس','shopx','dropshipper','{}'::jsonb,'null'::jsonb)`,
		`INSERT INTO stores (id,name,slug,owner_type,panel_config,seller_deals_widget_config) VALUES ('store-central','مرکزی','central','central','{}'::jsonb,'null'::jsonb)`,
		`INSERT INTO store_products VALUES ('store-shopx','p-old-1',0)`,
		`INSERT INTO store_products VALUES ('store-shopx','p-old-2',1)`,
		`INSERT INTO store_products VALUES ('store-shopx','p-old-3',2)`,
		// p-old-2 carries a 20% product override for shopx.
		`INSERT INTO store_product_overrides VALUES ('store-shopx','p-old-2',20,'percent')`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
	return repository.NewCatalogRepository(db), repository.NewStoreRepository(db)
}

func storeScopedList(t *testing.T, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, path string) []map[string]json.RawMessage {
	t.Helper()
	ctx := context.Background()
	handler := func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, ctx, catalogRepo, storeRepo, nil)
	}
	rec := team04Get(t, handler, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body %s", path, rec.Code, rec.Body.String())
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("GET %s decode: %v", path, err)
	}
	return list
}

func listIDs(list []map[string]json.RawMessage) []string {
	ids := make([]string, 0, len(list))
	for _, item := range list {
		var id string
		_ = json.Unmarshal(item["id"], &id)
		ids = append(ids, id)
	}
	return ids
}

func listPrice(t *testing.T, item map[string]json.RawMessage) int64 {
	t.Helper()
	var price int64
	if err := json.Unmarshal(item["price"], &price); err != nil {
		t.Fatalf("price decode: %v", err)
	}
	return price
}

func TestStoreScopedListReturnsOwnAssortment(t *testing.T) {
	catalogRepo, storeRepo := storeScopedRepos(t)

	// Store view: exactly the store's 3 products, in assortment
	// (position) order, at store-scoped prices. Default pricing
	// settings: platform pct 0, so reference = supplier cost;
	// dropshipper fallback margin is the max allowed (35), and
	// p-old-2's 20% product override wins for that product.
	list := storeScopedList(t, catalogRepo, storeRepo, "/api/v1/products/?storeSlug=shopx")
	ids := listIDs(list)
	wantIDs := []string{"p-old-1", "p-old-2", "p-old-3"}
	if len(ids) != len(wantIDs) {
		t.Fatalf("store list ids = %v, want exactly %v", ids, wantIDs)
	}
	for i, want := range wantIDs {
		if ids[i] != want {
			t.Fatalf("store list order = %v, want %v", ids, wantIDs)
		}
	}
	wantPrices := map[string]int64{"p-old-1": 13500000, "p-old-2": 24000000, "p-old-3": 6750000}
	for _, item := range list {
		var id string
		_ = json.Unmarshal(item["id"], &id)
		if got := listPrice(t, item); got != wantPrices[id] {
			t.Fatalf("store price of %s = %d, want %d", id, got, wantPrices[id])
		}
		// FIX 14 (auth-keyed): this call is anonymous, so the store
		// view must NOT carry the supplier cost fields; they are
		// served only to the store's owner, the product's supplier,
		// or an admin (see supplier_field_gate_test.go).
		if _, ok := item["supplierId"]; ok {
			t.Fatalf("anonymous store item %s must not carry supplierId", id)
		}
	}

	// The explicit ?limit= still truncates the store list.
	limited := storeScopedList(t, catalogRepo, storeRepo, "/api/v1/products/?storeSlug=shopx&limit=2")
	if got := listIDs(limited); len(got) != 2 || got[0] != "p-old-1" || got[1] != "p-old-2" {
		t.Fatalf("limited store list = %v, want [p-old-1 p-old-2]", got)
	}

	// Unscoped: previous behaviour untouched — globally active only
	// (45 noise + p-old-3), central pricing, no supplier fields.
	all := storeScopedList(t, catalogRepo, storeRepo, "/api/v1/products/")
	if len(all) != 46 {
		t.Fatalf("unscoped len = %d, want 46", len(all))
	}
	for _, item := range all {
		var id string
		_ = json.Unmarshal(item["id"], &id)
		if id == "p-old-1" || id == "p-old-2" || id == "p-outsider-inactive" {
			t.Fatalf("unscoped list must not contain globally inactive %s", id)
		}
		if _, ok := item["supplierId"]; ok {
			t.Fatalf("unscoped item %s must not carry supplierId", id)
		}
		if id == "p-old-3" {
			if got := listPrice(t, item); got != 5500000 {
				t.Fatalf("unscoped p-old-3 price = %d, want 5500000 (central)", got)
			}
		}
	}

	// Central store view: identical to unscoped (no assortment scope,
	// no supplier fields).
	central := storeScopedList(t, catalogRepo, storeRepo, "/api/v1/products/?storeSlug=central")
	if len(central) != 46 {
		t.Fatalf("central list len = %d, want 46", len(central))
	}
	for _, item := range central {
		if _, ok := item["supplierId"]; ok {
			t.Fatalf("central item must not carry supplierId")
		}
	}

	// Unknown slug: falls back to the unscoped catalog behaviour.
	unknown := storeScopedList(t, catalogRepo, storeRepo, "/api/v1/products/?storeSlug=no-such-store")
	if len(unknown) != 46 {
		t.Fatalf("unknown-slug list len = %d, want 46", len(unknown))
	}
}
