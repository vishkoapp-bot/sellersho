package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	_ "github.com/lib/pq"
)

// P1/P2 variant tests (2026-11-13). The validate* tests are pure JSON
// validation (no DB). The DB tests run ONLY when
// TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres.

func TestP1BSubsetAcceptedAndAggregatesUseSubmittedOnly(t *testing.T) {
	d := v2Fixture(t, 2, 3) // 6 combinations
	vs := d["variants"].([]interface{})
	// Keep 2 of 6: a subset is legal (P1(b)); unselected combinations
	// are never sent and never inserted.
	d["variants"] = []interface{}{vs[0], vs[5]}
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatalf("2-of-6 subset rejected: %v", err)
	}
	if d["stock"] != float64(4) {
		t.Fatalf("subset stock = %v, want 4 (2 selected x 2)", d["stock"])
	}
}

func TestP1BSubsetRejectsDuplicateAndOverCap(t *testing.T) {
	d := v2Fixture(t, 2, 3)
	vs := d["variants"].([]interface{})
	d["variants"] = []interface{}{vs[0], vs[0]}
	if err := ValidateProductVariationProposal(d); err == nil {
		t.Fatalf("duplicate combination accepted")
	}
	d = v2Fixture(t, 26, 20) // 520 submitted > 500 cap
	if err := ValidateProductVariationProposal(d); err == nil {
		t.Fatalf("520 submitted SKUs accepted, want 500-cap error")
	}
}

func TestP1CSaleValidationInProposal(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	v := d["variants"].([]interface{})[0].(map[string]interface{})
	v["sale_price"] = float64(850000)
	v["sale_from"] = "2030-01-01T00:00:00Z"
	v["sale_to"] = "2030-02-01T00:00:00Z"
	v["shipping_class"] = "fragile"
	v["tax_class"] = "reduced"
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatalf("valid sale+classes rejected: %v", err)
	}
	if v["sale_price"] != float64(850000) || v["shipping_class"] != "fragile" {
		t.Fatalf("sale/class not normalised: %v", v)
	}

	bad := func(mut func(map[string]interface{})) {
		d := v2Fixture(t, 1, 1)
		v := d["variants"].([]interface{})[0].(map[string]interface{})
		mut(v)
		if err := ValidateProductVariationProposal(d); err == nil {
			t.Fatalf("invalid sale/class accepted: %v", v)
		}
	}
	bad(func(v map[string]interface{}) { v["sale_price"] = float64(850050) }) // not %100
	bad(func(v map[string]interface{}) { v["sale_price"] = float64(0) })
	bad(func(v map[string]interface{}) { // window without price
		v["sale_from"] = "2030-01-01T00:00:00Z"
	})
	bad(func(v map[string]interface{}) { // from >= to
		v["sale_price"] = float64(850000)
		v["sale_from"] = "2030-02-01T00:00:00Z"
		v["sale_to"] = "2030-01-01T00:00:00Z"
	})
	bad(func(v map[string]interface{}) { v["shipping_class"] = "teleport" })
	bad(func(v map[string]interface{}) { v["tax_class"] = "super" })
	// Expired sale is normalised away, not rejected (re-validation at
	// approval/bulk-add must not fail on a sale that simply ended).
	d = v2Fixture(t, 1, 1)
	v = d["variants"].([]interface{})[0].(map[string]interface{})
	v["sale_price"] = float64(850000)
	v["sale_to"] = "2020-01-01T00:00:00Z"
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatalf("expired sale rejected: %v", err)
	}
	if v["sale_price"] != nil {
		t.Fatalf("expired sale not cleared: %v", v["sale_price"])
	}
}

func p1p2TestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB P1/P2 test")
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
		`DROP TABLE IF EXISTS store_variant_defaults, store_variant_visibility, store_products, product_variant_images, product_variants, product_niches, products, stores CASCADE`,
		`CREATE TABLE stores (id VARCHAR(50) PRIMARY KEY, slug VARCHAR(50) NOT NULL, owner_type VARCHAR(50) NOT NULL DEFAULT 'dropshipper')`,
		`CREATE TABLE products (id VARCHAR(50) PRIMARY KEY, title VARCHAR(255) NOT NULL, slug VARCHAR(255) UNIQUE NOT NULL, category_slug VARCHAR(100), supplier_id VARCHAR(50), original_price BIGINT NOT NULL, supplier_base_cost BIGINT NOT NULL, platform_margin BIGINT NOT NULL DEFAULT 0, is_original BOOLEAN NOT NULL DEFAULT TRUE, stock INT NOT NULL DEFAULT 0, image_url TEXT, specs JSONB DEFAULT '{}'::jsonb, description TEXT, warehouse_name VARCHAR(100), preparation_days INT DEFAULT 1, supplier_warranty VARCHAR(100), is_featured BOOLEAN DEFAULT FALSE, is_special_deal BOOLEAN DEFAULT FALSE, fulfillment_method VARCHAR(50), supply_option VARCHAR(50), is_active BOOLEAN NOT NULL DEFAULT TRUE, brand TEXT NOT NULL DEFAULT '', spn_number BIGINT, length_mm INT, width_mm INT, height_mm INT, weight_grams INT, shipping_class TEXT NOT NULL DEFAULT 'standard', tax_class TEXT NOT NULL DEFAULT 'standard', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_niches (product_id VARCHAR(50) NOT NULL, niche_id VARCHAR(50) NOT NULL, PRIMARY KEY (product_id, niche_id))`,
		`CREATE TABLE product_variants (id UUID PRIMARY KEY, product_id VARCHAR(50) NOT NULL, spnc VARCHAR(12) NOT NULL UNIQUE, spnc_number INT, variant_name TEXT NOT NULL DEFAULT 'پیش‌فرض', attributes JSONB NOT NULL DEFAULT '{}', is_default BOOLEAN NOT NULL DEFAULT FALSE, stock INT NOT NULL DEFAULT 0, supplier_base_cost BIGINT, selection JSONB NOT NULL DEFAULT '{}', combination_key TEXT, inventory_mode TEXT NOT NULL DEFAULT 'legacy', is_active BOOLEAN NOT NULL DEFAULT TRUE, min_cart_qty INT NOT NULL DEFAULT 1, max_cart_qty INT, weight_grams INT, length_mm INT, width_mm INT, height_mm INT, sale_price BIGINT, sale_from TIMESTAMPTZ, sale_to TIMESTAMPTZ, shipping_class TEXT, tax_class TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE product_variant_images (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), variant_id UUID NOT NULL, image_url TEXT NOT NULL, sort_order INT NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE store_products (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, position INT NOT NULL DEFAULT 0, PRIMARY KEY (store_id, product_id))`,
		`CREATE TABLE store_variant_visibility (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, variant_id UUID NOT NULL, is_visible BOOLEAN NOT NULL DEFAULT TRUE, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id, variant_id))`,
		`CREATE TABLE store_variant_defaults (store_id VARCHAR(50) NOT NULL, product_id VARCHAR(50) NOT NULL, variant_id UUID NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY (store_id, product_id))`,
	}
	for _, q := range ddl {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("DDL failed: %v", err)
		}
	}
	seed := []string{
		`INSERT INTO stores VALUES ('store-central','central','central')`,
		`INSERT INTO stores VALUES ('store-a','shop-a','dropshipper')`,
		`INSERT INTO stores VALUES ('store-b','shop-b','dropshipper')`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,warehouse_name,description,supplier_warranty,is_active) VALUES ('prod-1','Test product','test-product','beauty','sup-1',1250000,1000000,'','', '',TRUE)`,
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

func TestP2AVisibilityFiltering(t *testing.T) {
	db := p1p2TestDB(t)
	ctx := context.Background()
	cat := NewCatalogRepository(db)
	storeRepo := NewStoreRepository(db)

	// No rows: every store sees both variants.
	for _, sid := range []string{"", "central", "store-a", "store-b"} {
		byProduct, err := cat.GetVariantsByProductIDsForStore(ctx, []string{"prod-1"}, sid)
		if err != nil {
			t.Fatalf("load store %q: %v", sid, err)
		}
		if len(byProduct["prod-1"]) != 2 {
			t.Fatalf("store %q variants = %d, want 2", sid, len(byProduct["prod-1"]))
		}
	}
	// Hide the premium variant in store A only.
	if err := storeRepo.SetStoreVariantVisibility(ctx, "store-a", "prod-1", "22222222-2222-2222-2222-222222222222", false); err != nil {
		t.Fatalf("hide: %v", err)
	}
	byProduct, _ := cat.GetVariantsByProductIDsForStore(ctx, []string{"prod-1"}, "store-a")
	if len(byProduct["prod-1"]) != 1 || byProduct["prod-1"][0].ID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("store-a after hide = %+v, want only cheap", byProduct["prod-1"])
	}
	byProduct, _ = cat.GetVariantsByProductIDsForStore(ctx, []string{"prod-1"}, "store-b")
	if len(byProduct["prod-1"]) != 2 {
		t.Fatalf("store-b must still see 2, got %d", len(byProduct["prod-1"]))
	}
	byProduct, _ = cat.GetVariantsByProductIDsForStore(ctx, []string{"prod-1"}, "central")
	if len(byProduct["prod-1"]) != 2 {
		t.Fatalf("central must still see 2, got %d", len(byProduct["prod-1"]))
	}
	// Re-show: row deleted, variant returns.
	if err := storeRepo.SetStoreVariantVisibility(ctx, "store-a", "prod-1", "22222222-2222-2222-2222-222222222222", true); err != nil {
		t.Fatalf("re-show: %v", err)
	}
	hidden, err := storeRepo.ListHiddenVariantIDs(ctx, "store-a", "prod-1")
	if err != nil || len(hidden) != 0 {
		t.Fatalf("hidden after re-show = %v, err %v", hidden, err)
	}
	byProduct, _ = cat.GetVariantsByProductIDsForStore(ctx, []string{"prod-1"}, "store-a")
	if len(byProduct["prod-1"]) != 2 {
		t.Fatalf("store-a after re-show = %d, want 2", len(byProduct["prod-1"]))
	}
	// Order-path check: hidden => not visible; no row => visible.
	if err := storeRepo.SetStoreVariantVisibility(ctx, "store-a", "prod-1", "22222222-2222-2222-2222-222222222222", false); err != nil {
		t.Fatalf("hide again: %v", err)
	}
	vis, err := storeRepo.IsVariantVisibleInStore(ctx, "store-a", "22222222-2222-2222-2222-222222222222")
	if err != nil || vis {
		t.Fatalf("hidden variant visible=%v err=%v", vis, err)
	}
	vis, err = storeRepo.IsVariantVisibleInStore(ctx, "store-b", "22222222-2222-2222-2222-222222222222")
	if err != nil || !vis {
		t.Fatalf("other store visible=%v err=%v", vis, err)
	}
}

func TestP2CStoreDefaultRoundTrip(t *testing.T) {
	db := p1p2TestDB(t)
	ctx := context.Background()
	cat := NewCatalogRepository(db)
	storeRepo := NewStoreRepository(db)

	def, err := cat.GetStoreVariantDefaults(ctx, "store-a", []string{"prod-1"})
	if err != nil || len(def) != 0 {
		t.Fatalf("no row => empty defaults, got %v err %v", def, err)
	}
	// A = cheap variant, B = premium variant: independent defaults.
	if err := storeRepo.SetStoreVariantDefault(ctx, "store-a", "prod-1", "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("set A: %v", err)
	}
	if err := storeRepo.SetStoreVariantDefault(ctx, "store-b", "prod-1", "22222222-2222-2222-2222-222222222222"); err != nil {
		t.Fatalf("set B: %v", err)
	}
	def, _ = cat.GetStoreVariantDefaults(ctx, "store-a", []string{"prod-1"})
	if def["prod-1"] != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("store-a default = %v", def)
	}
	def, _ = cat.GetStoreVariantDefaults(ctx, "store-b", []string{"prod-1"})
	if def["prod-1"] != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("store-b default = %v", def)
	}
	// PUT replaces (still exactly one row).
	if err := storeRepo.SetStoreVariantDefault(ctx, "store-a", "prod-1", "22222222-2222-2222-2222-222222222222"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM store_variant_defaults WHERE store_id='store-a' AND product_id='prod-1'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("default rows for store-a = %d err %v, want 1", n, err)
	}
	// DELETE clears to product-level fallback.
	if err := storeRepo.ClearStoreVariantDefault(ctx, "store-a", "prod-1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, err := storeRepo.GetStoreVariantDefault(ctx, "store-a", "prod-1")
	if err != nil || got != "" {
		t.Fatalf("after clear default = %q err %v", got, err)
	}
}

func TestP1CSaleAndClassesRoundTrip(t *testing.T) {
	db := p1p2TestDB(t)
	ctx := context.Background()
	cat := NewCatalogRepository(db)
	sup := NewSupplierRepository(db)

	// Sale + classes survive the catalog loader.
	from := time.Now().Add(-time.Hour)
	to := time.Now().Add(24 * time.Hour)
	sale := int64(1050000)
	if _, err := db.ExecContext(ctx, `UPDATE product_variants SET sale_price=$1, sale_from=$2, sale_to=$3, shipping_class='fragile', tax_class='reduced' WHERE id='22222222-2222-2222-2222-222222222222'`, sale, from, to); err != nil {
		t.Fatalf("seed sale: %v", err)
	}
	byProduct, err := cat.GetVariantsByProductIDs(ctx, []string{"prod-1"})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var found bool
	for _, v := range byProduct["prod-1"] {
		if v.ID == "22222222-2222-2222-2222-222222222222" {
			found = true
			if v.SalePrice == nil || *v.SalePrice != sale || v.SaleFrom == nil || v.SaleTo == nil {
				t.Fatalf("sale not round-tripped: %+v", v)
			}
			if v.ShippingClass == nil || *v.ShippingClass != "fragile" || v.TaxClass == nil || *v.TaxClass != "reduced" {
				t.Fatalf("classes not round-tripped: %+v", v)
			}
		}
		if v.ID == "11111111-1111-1111-1111-111111111111" && (v.SalePrice != nil || v.ShippingClass != nil) {
			t.Fatalf("cheap variant must have nil sale/class: %+v", v)
		}
	}
	if !found {
		t.Fatalf("premium variant missing from load")
	}
	// Supplier-side write path: set then clear.
	to2 := time.Now().Add(48 * time.Hour)
	ns := int64(1100000)
	out, err := sup.UpdateVariantSale(ctx, "sup-1", "prod-1", "11111111-1111-1111-1111-111111111111", &ns, nil, &to2)
	if err != nil {
		t.Fatalf("UpdateVariantSale: %v", err)
	}
	if out.SalePrice == nil || *out.SalePrice != ns || out.SaleTo == nil {
		t.Fatalf("UpdateVariantSale result = %+v", out)
	}
	out, err = sup.UpdateVariantSale(ctx, "sup-1", "prod-1", "11111111-1111-1111-1111-111111111111", nil, nil, nil)
	if err != nil {
		t.Fatalf("clear sale: %v", err)
	}
	if out.SalePrice != nil || out.SaleFrom != nil || out.SaleTo != nil {
		t.Fatalf("sale not cleared: %+v", out)
	}
	// Another supplier cannot write it.
	if _, err := sup.UpdateVariantSale(ctx, "sup-other", "prod-1", "11111111-1111-1111-1111-111111111111", &ns, nil, &to2); err != sql.ErrNoRows {
		t.Fatalf("foreign supplier write err = %v, want sql.ErrNoRows", err)
	}
	// Product-level classes round-trip through the catalog loader.
	prods, err := cat.GetProducts(ctx, ProductFilter{})
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(prods) != 1 || prods[0].ShippingClass != "standard" || prods[0].TaxClass != "standard" {
		t.Fatalf("product classes = %+v", prods)
	}
}
