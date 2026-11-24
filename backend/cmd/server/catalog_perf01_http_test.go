package main

// PERF-01 paged catalog HTTP tests (handlers only). These run ONLY
// when SELLERSHO_TEST_DATABASE_URL is set to a DISPOSABLE local
// Postgres (never the live database); otherwise they skip. The
// isolated runner applies ALL migrations first, so the test seeds
// against the full schema (central + dropshipper store, settings,
// supplier/category rows for the FKs, 30 products, variants,
// assortment) instead of the TEAM04 minimal DDL — the price-index
// fingerprint reads many tables the minimal DDL lacks.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
	_ "github.com/lib/pq"
)

func perf01HTTPRepos(t *testing.T) (*repository.CatalogRepository, *repository.StoreRepository) {
	t.Helper()
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SELLERSHO_TEST_DATABASE_URL not set; skipping disposable-DB HTTP test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// TEST-02 round 4: the stores rebuilds in this package drop the
	// migrated stores_slug_key unique index (their slug column has no
	// UNIQUE), so the seed's ON CONFLICT (slug) below would fail with
	// 42P10. Restore the migrated unique index first.
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS stores_slug_key ON stores(slug)`); err != nil {
		t.Fatalf("restore stores_slug_key: %v", err)
	}

	seed := []string{
		`INSERT INTO suppliers (id, name, approval_status) VALUES ('sup-1', 'Supplier One', 'approved'), ('sup-2', 'Supplier Two', 'suspended') ON CONFLICT (id) DO NOTHING`,
		// The migrated schema is pre-seeded (demo stores/products), so
		// this test owns one category slug, 'perf01-cat', and scopes
		// every count assertion to it. The central store already
		// exists as 'store-central' (slug 'central') — never insert it.
		`INSERT INTO categories (id, title, slug, icon_name) VALUES ('perf01-cat', 'Perf01 Cat', 'perf01-cat', 'box') ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO platform_settings DEFAULT VALUES ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO stores (id, name, slug, owner_type, owner_name) VALUES ('store-a', 'Store A', 'store-a', 'dropshipper', 'Owner A') ON CONFLICT (slug) DO NOTHING`,
	}
	// 30 products: http-001..http-030, cost 11000+i*1000, original
	// 100000, all in 'perf01-cat', odd = Alpha, even = Beta, featured
	// every 5th, http-029 inactive, http-030 suspended supplier.
	// created_at staggered so newest order is deterministic. Products
	// go FIRST: variants and store_products reference them.
	vals := ""
	for i := 1; i <= 30; i++ {
		cat, brand := "perf01-cat", "Alpha"
		if i%2 == 0 {
			cat, brand = "perf01-cat", "Beta"
		}
		supplier, active := "sup-1", true
		if i == 30 {
			supplier = "sup-2"
		}
		if i == 29 {
			active = false
		}
		featured := i%5 == 0
		if vals != "" {
			vals += ","
		}
		vals += fmt.Sprintf("('http-%03d','Product %03d','http-%03d','%s','%s',100000,%d,0,TRUE,10,%t,'%s',%t,NOW() - INTERVAL '%d days')",
			i, i, i, cat, supplier, 10000+i*1000, active, brand, featured, i)
	}
	seed = append(seed, `INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,is_active,brand,is_featured,created_at) VALUES `+vals+` ON CONFLICT (id) DO NOTHING`)
	seed = append(seed,
		`INSERT INTO product_variants (id, product_id, spnc, spnc_number, variant_name, attributes, is_default, stock, supplier_base_cost) VALUES ('11111111-1111-1111-1111-111111111111', 'http-001', 'A01B02C3D4', 20990101, 'Default', '{}', TRUE, 5, 60000), ('22222222-2222-2222-2222-222222222222', 'http-001', 'E5F6A7B8C9', 20990102, 'Second', '{}', FALSE, 2, 12000) ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO store_products (store_id, product_id, position) VALUES ('store-a', 'http-029', 0), ('store-a', 'http-001', 1), ('store-a', 'http-002', 2) ON CONFLICT (store_id, product_id) DO NOTHING`,
	)
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed: %v\n%s", err, q)
		}
	}
	return repository.NewCatalogRepository(db), repository.NewStoreRepository(db)
}

func perf01HTTPGet(t *testing.T, handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func perf01ListHandler(t *testing.T, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, context.Background(), catalogRepo, storeRepo, nil)
	}
}

func perf01DecodeList(t *testing.T, rec *httptest.ResponseRecorder) ProductListPageResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	var page ProductListPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list envelope: %v (body %s)", err, rec.Body.String())
	}
	if page.Items == nil {
		t.Fatal("items must be an array, never null")
	}
	return page
}

// walkPages follows nextCursor from path until hasMore is false and
// returns every item id in order.
func perf01WalkPages(t *testing.T, handler http.HandlerFunc, path string) []ProductResponse {
	t.Helper()
	var all []ProductResponse
	seen := map[string]bool{}
	cursor := ""
	for {
		p := path
		if cursor != "" {
			p += "&cursor=" + cursor
		}
		page := perf01DecodeList(t, perf01HTTPGet(t, handler, p))
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate id %s while paging %s", item.ID, path)
			}
			seen[item.ID] = true
			all = append(all, item)
		}
		if !page.HasMore {
			if page.NextCursor != nil {
				t.Fatalf("last page must carry a null nextCursor (%s)", path)
			}
			if page.Total != len(all) {
				t.Fatalf("total %d != enumerated %d (%s)", page.Total, len(all), path)
			}
			return all
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			t.Fatalf("hasMore without a nextCursor (%s)", path)
		}
		cursor = *page.NextCursor
	}
}

// perf01CardPrice mirrors src/utils/cardPrice.ts on the list response:
// store-default variant, then product default, then first active priced
// variant; else the product-level price. This is the price the index
// main_price, the price sorts, the price filters and the bounds all
// operate on — NOT item.Price (the product-level price).
func perf01CardPrice(item ProductResponse) int64 {
	if item.StoreDefaultVariantID != nil {
		if vp, ok := item.VariantPrices[*item.StoreDefaultVariantID]; ok && vp.Price > 0 {
			return vp.Price
		}
	}
	for _, v := range item.Variants {
		if v.IsDefault {
			if vp, ok := item.VariantPrices[v.ID]; ok && vp.Price > 0 {
				return vp.Price
			}
		}
	}
	for _, v := range item.Variants {
		if v.IsActive {
			if vp, ok := item.VariantPrices[v.ID]; ok && vp.Price > 0 {
				return vp.Price
			}
		}
	}
	return item.Price
}

func TestPerf01HTTPListEnvelope(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	handler := perf01ListHandler(t, catalogRepo, storeRepo)

	// Default page is 24 of 28 eligible (30 seeded - 1 inactive -
	// 1 suspended supplier).
	page := perf01DecodeList(t, perf01HTTPGet(t, handler, "/api/v1/products?category=perf01-cat"))
	if len(page.Items) != 24 || !page.HasMore || page.NextCursor == nil || page.Total != 28 {
		t.Fatalf("default page = %d items hasMore=%v total=%d (want 24/true/28)", len(page.Items), page.HasMore, page.Total)
	}
	// http-001: product-level price 12100 (cost 11000), variant range
	// 13200..66000, CARD price 66000 (default variant at cost 60000).
	for _, item := range page.Items {
		if item.ID == "http-001" {
			if item.Price != 12100 {
				t.Fatalf("http-001 product price = %d, want 12100", item.Price)
			}
			if item.PriceMin == nil || *item.PriceMin != 13200 || item.PriceMax == nil || *item.PriceMax != 66000 {
				t.Fatalf("http-001 variant range = %v..%v, want 13200..66000", item.PriceMin, item.PriceMax)
			}
			if perf01CardPrice(item) != 66000 {
				t.Fatalf("http-001 card price = %d, want 66000 (default variant)", perf01CardPrice(item))
			}
		}
	}
	// Full enumeration, every sort the handler serves.
	for _, sortParam := range []string{"featured", "newest", "cheapest", "expensive", "discount"} {
		all := perf01WalkPages(t, handler, "/api/v1/products?category=perf01-cat&sort="+sortParam+"&limit=7")
		if len(all) != 28 {
			t.Fatalf("sort %s enumerated %d, want 28", sortParam, len(all))
		}
	}
	// Price sorts are monotonic in the displayed price.
	cheap := perf01WalkPages(t, handler, "/api/v1/products?category=perf01-cat&sort=cheapest&limit=10")
	for i := 1; i < len(cheap); i++ {
		if perf01CardPrice(cheap[i]) < perf01CardPrice(cheap[i-1]) {
			t.Fatalf("cheapest not ascending at %d: %d < %d", i, perf01CardPrice(cheap[i]), perf01CardPrice(cheap[i-1]))
		}
	}
	expensive := perf01WalkPages(t, handler, "/api/v1/products?category=perf01-cat&sort=expensive&limit=10")
	for i := 1; i < len(expensive); i++ {
		if perf01CardPrice(expensive[i]) > perf01CardPrice(expensive[i-1]) {
			t.Fatalf("expensive not descending at %d: %d > %d", i, perf01CardPrice(expensive[i]), perf01CardPrice(expensive[i-1]))
		}
	}
	// Filters: category + brand + search + price range, all server-side.
	brand := perf01WalkPages(t, handler, "/api/v1/products?category=perf01-cat&brand=Beta&limit=10")
	if len(brand) != 14 {
		t.Fatalf("brand=Beta enumerated %d, want 14", len(brand))
	}
	search := perf01WalkPages(t, handler, "/api/v1/products?category=perf01-cat&search=Product%20003&limit=10")
	if len(search) != 1 || search[0].ID != "http-003" {
		t.Fatalf("search enumerated %+v, want [http-003]", search)
	}
	priced := perf01WalkPages(t, handler, "/api/v1/products?category=perf01-cat&min_price=20000&max_price=25000&limit=10")
	if len(priced) == 0 {
		t.Fatal("price-range filter returned nothing")
	}
	for _, item := range priced {
		// The range applies to the CARD price (index main price):
		// http-001's product price 12100 is inside the range but its
		// card price 66000 is not, so it must be absent.
		if item.ID == "http-001" {
			t.Fatal("price-range filter must use the card price: http-001 (card 66000) listed in [20000,25000]")
		}
		if cp := perf01CardPrice(item); cp < 20000 || cp > 25000 {
			t.Fatalf("price-range item %s card price %d outside [20000,25000]", item.ID, cp)
		}
	}
}

func TestPerf01IndexFollowsPurchasableCardPrice(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{
			`DELETE FROM product_units WHERE product_id='http-001' AND code IN ('A01B02C3D4-X0001','E5F6A7B8C9-X0001')`,
			`DELETE FROM inventory_balances WHERE id='perf01-card-lot'`,
			`DELETE FROM warehouses WHERE id='perf01-card-origin'`,
		} {
			if _, err := db.Exec(statement); err != nil {
				t.Errorf("clean stock-aware card fixture: %v", err)
			}
		}
	})
	for _, statement := range []string{
		`INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name)
		VALUES ('perf01-card-origin','Card price test',1,1,'Tehran','Tehran') ON CONFLICT DO NOTHING`,
		`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id)
		VALUES ('perf01-card-lot','http-001','sup-1','supplier','warehouse','warehouse:perf01-card-origin','sellable',2,'perf01-card-origin')
		ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id)
		SELECT spnc||'-X0001',id,product_id,1,'in_stock','perf01-card-lot'
		FROM product_variants WHERE product_id='http-001'
		ON CONFLICT (code) DO NOTHING`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	handler := perf01ListHandler(t, catalogRepo, storeRepo)
	inRange := func(min, max int) *ProductResponse {
		t.Helper()
		page := perf01DecodeList(t, perf01HTTPGet(t, handler, fmt.Sprintf("/api/v1/products?category=perf01-cat&min_price=%d&max_price=%d", min, max)))
		for i := range page.Items {
			if page.Items[i].ID == "http-001" {
				return &page.Items[i]
			}
		}
		return nil
	}
	low := inRange(13000, 14000)
	if low == nil || perf01CardPrice(*low) != 13200 || inRange(65000, 67000) != nil {
		t.Fatal("both variants are purchasable: range index must choose the lowest priced SKU")
	}
	if _, err := db.Exec(`UPDATE product_units SET status='removed',balance_id=NULL WHERE code='E5F6A7B8C9-X0001'`); err != nil {
		t.Fatal(err)
	}
	high := inRange(65000, 67000)
	if inRange(13000, 14000) != nil || high == nil || perf01CardPrice(*high) != 66000 {
		t.Fatal("depleting the cheap coded SKU must invalidate the price index and choose the remaining purchasable SKU")
	}
	if high.PriceMin == nil || *high.PriceMin != 13200 || high.PriceMax == nil || *high.PriceMax != 66000 {
		t.Fatal("secondary displayed range must still include all active priced variants")
	}
	for _, variant := range high.Variants {
		if variant.StockAuthority != "coded-v1" {
			t.Fatal("authoritative stock marker missing from public SKU")
		}
		if variant.Spnc == "E5F6A7B8C9" && variant.Stock != 0 {
			t.Fatal("a depleted SKU must not borrow its sibling's coded units")
		}
	}
}

func TestPerf01HTTPListValidation(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	handler := perf01ListHandler(t, catalogRepo, storeRepo)
	for _, path := range []string{
		"/api/v1/products?limit=0",
		"/api/v1/products?limit=101",
		"/api/v1/products?limit=-1",
		"/api/v1/products?limit=abc",
		"/api/v1/products?limit=5&limit=10",
		"/api/v1/products?page=2",
		"/api/v1/products?sort=bogus",
		"/api/v1/products?min_price=abc",
		"/api/v1/products?min_price=-5",
	} {
		rec := perf01HTTPGet(t, handler, path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("GET %s = %d, want 400 (body %s)", path, rec.Code, rec.Body.String())
		}
	}
	page := perf01DecodeList(t, perf01HTTPGet(t, handler, "/api/v1/products?category=perf01-cat&limit=5"))
	if len(page.Items) != 5 || !page.HasMore || page.Total != 28 {
		t.Fatalf("limit=5 page = %d items total=%d (want 5/28)", len(page.Items), page.Total)
	}
}

func TestPerf01HTTPListRestart(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	handler := perf01ListHandler(t, catalogRepo, storeRepo)
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	page1 := perf01DecodeList(t, perf01HTTPGet(t, handler, "/api/v1/products?category=perf01-cat&sort=cheapest&limit=5"))
	if page1.NextCursor == nil {
		t.Fatal("page 1 has no cursor")
	}
	cursor := *page1.NextCursor

	// A title edit is price-neutral: the old cursor still works.
	if _, err := db.Exec(`UPDATE products SET title='Renamed' WHERE id='http-003'`); err != nil {
		t.Fatal(err)
	}
	rec := perf01HTTPGet(t, handler, "/api/v1/products?category=perf01-cat&sort=cheapest&limit=5&cursor="+cursor)
	if rec.Code != http.StatusOK {
		t.Fatalf("title edit: next page = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if _, err := db.Exec(`UPDATE products SET title='Product 003' WHERE id='http-003'`); err != nil {
		t.Fatal(err)
	}

	// A price-changing edit bumps the scope revision on the next
	// refresh; a cursor issued before the bump must fail with 409 +
	// restart:true.
	if _, err := db.Exec(`UPDATE products SET supplier_base_cost=99000 WHERE id='http-003'`); err != nil {
		t.Fatal(err)
	}
	// This fresh request performs the refresh (revision bumps here).
	perf01DecodeList(t, perf01HTTPGet(t, handler, "/api/v1/products?category=perf01-cat&sort=cheapest&limit=5"))
	rec = perf01HTTPGet(t, handler, "/api/v1/products?category=perf01-cat&sort=cheapest&limit=5&cursor="+cursor)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale cursor after price change = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["restart"] != true {
		t.Fatalf("409 body must carry restart:true, got %s", rec.Body.String())
	}
	// And paging restarts cleanly from page 1 under the new revision.
	all := perf01WalkPages(t, handler, "/api/v1/products?category=perf01-cat&sort=cheapest&limit=7")
	if len(all) != 28 {
		t.Fatalf("restart enumeration = %d, want 28", len(all))
	}
}

func TestPerf01HTTPBounds(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	listHandler := perf01ListHandler(t, catalogRepo, storeRepo)
	boundsHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductPriceBounds(w, r, context.Background(), catalogRepo, storeRepo, nil)
	}
	getBounds := func(path string) productPriceBoundsResponse {
		t.Helper()
		rec := perf01HTTPGet(t, boundsHandler, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("bounds %s = %d (body %s)", path, rec.Code, rec.Body.String())
		}
		var b productPriceBoundsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			t.Fatalf("decode bounds: %v", err)
		}
		return b
	}
	all := perf01WalkPages(t, listHandler, "/api/v1/products?category=perf01-cat&sort=cheapest&limit=10")
	prices := make([]int64, 0, len(all))
	for _, item := range all {
		prices = append(prices, perf01CardPrice(item))
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
	b := getBounds("/api/v1/products/price-bounds?category=perf01-cat")
	if b.MinPrice != prices[0] || b.MaxPrice != prices[len(prices)-1] || b.Count != 28 {
		t.Fatalf("bounds = %+v, want min=%d max=%d count=28", b, prices[0], prices[len(prices)-1])
	}
	// B1(a): store-a's assortment is http-029 (inactive), http-001
	// and http-002 — the bounds cover the 2 active rows only.
	storeA := getBounds("/api/v1/products/price-bounds?storeSlug=store-a&category=perf01-cat")
	if storeA.Count != 2 {
		t.Fatalf("store-a bounds count = %d, want 2", storeA.Count)
	}
}

func TestPerf01HTTPStoreCategoriesAndScope(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	listHandler := perf01ListHandler(t, catalogRepo, storeRepo)
	catsHandler := func(w http.ResponseWriter, r *http.Request) {
		handleStoreCategorySlugs(w, r, context.Background(), catalogRepo, storeRepo)
	}
	getSlugs := func(path string) []string {
		t.Helper()
		rec := perf01HTTPGet(t, catsHandler, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("store-categories %s = %d (body %s)", path, rec.Code, rec.Body.String())
		}
		var body struct {
			Slugs []string `json:"slugs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode slugs: %v", err)
		}
		return body.Slugs
	}
	has := func(slugs []string, want string) bool {
		for _, s := range slugs {
			if s == want {
				return true
			}
		}
		return false
	}
	slugs := getSlugs("/api/v1/products/store-categories?storeSlug=store-a")
	if !has(slugs, "perf01-cat") {
		t.Fatalf("store-a slugs = %v, want perf01-cat among them", slugs)
	}
	central := getSlugs("/api/v1/products/store-categories")
	if !has(central, "perf01-cat") {
		t.Fatalf("central slugs = %v, want perf01-cat among them", central)
	}
	unknown := getSlugs("/api/v1/products/store-categories?storeSlug=nope")
	if !has(unknown, "perf01-cat") {
		t.Fatalf("unknown slug must fall back to central, got %v", unknown)
	}

	// Store A scopes to its assortment in store position order —
	// B1(a): the inactive http-029 is excluded (suspended http-030
	// was never in the assortment), so the page is http-001, http-002.
	page := perf01DecodeList(t, perf01HTTPGet(t, listHandler, "/api/v1/products?storeSlug=store-a&category=perf01-cat"))
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("store-a page total=%d items=%d (want 2/2)", page.Total, len(page.Items))
	}
	want := []string{"http-001", "http-002"}
	for i, id := range want {
		if page.Items[i].ID != id {
			t.Fatalf("store-a position %d = %s, want %s", i, page.Items[i].ID, id)
		}
	}
}
