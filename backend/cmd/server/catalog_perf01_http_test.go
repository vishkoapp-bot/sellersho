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
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/catalogpage"
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

// perf01CardPrice independently mirrors the browser's Batch 9 pick over
// the rendered HTTP payload, rather than trusting index fields.
func perf01CardPrice(item ProductResponse) int64 {
	var cheapest, first *int64
	for _, v := range item.Variants {
		if !v.IsActive || (item.ProductType == "variable" && isReservedDefaultVariantName(v.VariantName)) {
			continue
		}
		vp, ok := item.VariantPrices[v.ID]
		if !ok || vp.PricingError != "" {
			continue
		}
		if first == nil {
			price := vp.Price
			first = &price
		}
		stock := v.Stock
		if v.InventoryMode != "tracked" && item.ProductType != "variable" && item.SchemaVersion != 2 && stock <= 0 {
			stock = item.Stock
		}
		minQty := max(1, v.MinCartQty)
		if !item.IsActive || stock < minQty || (v.MaxCartQty != nil && *v.MaxCartQty < minQty) {
			continue
		}
		if item.StoreDefaultVariantID != nil && v.ID == *item.StoreDefaultVariantID && (vp.Price > 0 || (item.ProductType != "variable" && item.SchemaVersion != 2)) {
			return vp.Price
		}
		if vp.Price > 0 && (cheapest == nil || vp.Price < *cheapest) {
			price := vp.Price
			cheapest = &price
		}
	}
	if cheapest != nil {
		return *cheapest
	}
	if first != nil {
		return *first
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
	// 13200..66000, CARD price 13200 (cheapest purchasable variant at cost 12000).
	for _, item := range page.Items {
		if item.ID == "http-001" {
			if item.Price != 12100 {
				t.Fatalf("http-001 product price = %d, want 12100", item.Price)
			}
			if item.PriceMin == nil || *item.PriceMin != 13200 || item.PriceMax == nil || *item.PriceMax != 66000 {
				t.Fatalf("http-001 variant range = %v..%v, want 13200..66000", item.PriceMin, item.PriceMax)
			}
			if perf01CardPrice(item) != 13200 {
				t.Fatalf("http-001 card price = %d, want 13200 (cheapest purchasable variant)", perf01CardPrice(item))
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
		// http-001's card price 13200 is outside this range.
		if item.ID == "http-001" {
			t.Fatal("price-range filter must use the card price: http-001 (card 13200) listed in [20000,25000]")
		}
		if cp := perf01CardPrice(item); cp < 20000 || cp > 25000 {
			t.Fatalf("price-range item %s card price %d outside [20000,25000]", item.ID, cp)
		}
	}
	for _, sample := range []struct {
		lower, upper int
		want         bool
	}{{13200, 13200, true}, {66000, 66000, false}} {
		items := perf01WalkPages(t, handler, fmt.Sprintf("/api/v1/products?category=perf01-cat&min_price=%d&max_price=%d&limit=10", sample.lower, sample.upper))
		found := false
		for _, item := range items {
			found = found || item.ID == "http-001"
		}
		if found != sample.want {
			t.Fatalf("http-001 filter [%d,%d]: found=%v, want %v", sample.lower, sample.upper, found, sample.want)
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

func TestPerf01HTTPFingerprintTransitions(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	handler := perf01ListHandler(t, catalogRepo, storeRepo)
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	const cheap = "22222222-2222-2222-2222-222222222222"
	t.Cleanup(func() { exec("UPDATE product_variants SET min_cart_qty=1,stock=2 WHERE id='" + cheap + "'") })
	path := "/api/v1/products?category=perf01-cat&search=Product%20001&sort=cheapest"
	assertPrice := func(want int64) string {
		t.Helper()
		items := perf01WalkPages(t, handler, path)
		if len(items) != 1 || items[0].ID != "http-001" || perf01CardPrice(items[0]) != want {
			t.Fatalf("expected one matching card at %d, got %+v", want, items)
		}
		var price int64
		var fp string
		if err := db.QueryRow("SELECT main_price,input_fingerprint FROM catalog_public_prices WHERE store_id='store-central' AND product_id='http-001'").Scan(&price, &fp); err != nil {
			t.Fatal(err)
		}
		if price != want || !strings.HasPrefix(fp, repository.FingerprintVersion+":") {
			t.Fatalf("index must refresh to card price %d, got %d / %q", want, price, fp)
		}
		return fp
	}
	fp := assertPrice(13200)
	exec("UPDATE catalog_public_prices SET main_price=66000,input_fingerprint='v2:old' WHERE store_id='store-central' AND product_id='http-001'")
	if got := assertPrice(13200); got != fp {
		t.Fatalf("stale v2 recomputation changed the input fingerprint")
	}
	exec("UPDATE product_variants SET min_cart_qty=3 WHERE id='" + cheap + "'")
	if got := assertPrice(66000); got == fp {
		t.Fatal("minimum quantity change did not invalidate the index")
	}
	exec("UPDATE product_variants SET min_cart_qty=2 WHERE id='" + cheap + "'")
	atMin := assertPrice(13200)
	exec("UPDATE product_variants SET stock=1 WHERE id='" + cheap + "'")
	if got := assertPrice(66000); got == atMin {
		t.Fatal("raw stock dropping below minimum did not invalidate the index")
	}
	exec("UPDATE product_variants SET min_cart_qty=1,stock=2 WHERE id='" + cheap + "'")
	if got := assertPrice(13200); got != fp {
		t.Fatal("restoring availability did not restore the correct fingerprint and price")
	}
	// These are valid JSONB values on older rows. Like the DTO, index
	// rebuilding must treat non-object attributes as empty, not fail the scope.
	t.Cleanup(func() { exec("UPDATE product_variants SET attributes='{}' WHERE id='" + cheap + "'") })
	for _, raw := range []string{`["legacy"]`, `42`, `null`} {
		exec("UPDATE product_variants SET attributes='" + raw + "'::jsonb WHERE id='" + cheap + "'")
		if got := assertPrice(13200); got == fp {
			t.Fatalf("attribute change %s did not refresh the index", raw)
		}
	}
}

func TestPerf01HTTPStockChangeRestartsDuringCursorRequest(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	handler := perf01ListHandler(t, catalogRepo, storeRepo)
	path := "/api/v1/products?category=perf01-cat&sort=cheapest&limit=1"
	page := perf01DecodeList(t, perf01HTTPGet(t, handler, path))
	if len(page.Items) != 1 || page.Items[0].ID != "http-001" || page.NextCursor == nil {
		t.Fatalf("expected cheap variant to lead page before stock change: %+v", page)
	}
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE product_variants SET min_cart_qty=3 WHERE id='22222222-2222-2222-2222-222222222222'"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("UPDATE product_variants SET min_cart_qty=1 WHERE id='22222222-2222-2222-2222-222222222222'")
	rec := perf01HTTPGet(t, handler, path+"&cursor="+*page.NextCursor)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"restart":true`) {
		t.Fatalf("stock changes discovered while paging must return 409 restart, got %d: %s", rec.Code, rec.Body.String())
	}
	restarted := perf01DecodeList(t, perf01HTTPGet(t, handler, path))
	if len(restarted.Items) != 1 || restarted.Items[0].ID == "http-001" || restarted.NextCursor == nil {
		t.Fatalf("fresh pagination must see repricing: %+v", restarted)
	}
}

func TestPerf01HTTPRawStockSnapshotSurvivesConcurrentThresholdChange(t *testing.T) {
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	handler := perf01ListHandler(t, catalogRepo, storeRepo)
	path := "/api/v1/products?category=perf01-cat&search=Product%20001&sort=cheapest&min_price=13200&max_price=13200"
	before := perf01DecodeList(t, perf01HTTPGet(t, handler, path))
	if len(before.Items) != 1 || perf01CardPrice(before.Items[0]) != 13200 {
		t.Fatalf("expected one cheap card before concurrent write: %+v", before)
	}
	ctx := context.Background()
	tx, err := catalogRepo.BeginCatalogTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	revision, err := catalogRepo.GetCatalogScopeRevisionTx(ctx, tx, "store-central")
	if err != nil {
		t.Fatal(err)
	}
	scope := catalogpage.Scope{ID: "store-central", OwnerType: "central", Revision: revision}
	filter := catalogpage.Filter{Sort: "cheapest", Search: "Product 001"}
	selected, err := catalogRepo.GetPricedProductPageTx(ctx, tx, scope, filter, 1, nil)
	if err != nil || len(selected.Keys) != 1 || selected.Keys[0].Price != 13200 {
		t.Fatalf("snapshot selection=%+v err=%v", selected, err)
	}
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const cheap = "22222222-2222-2222-2222-222222222222"
	if _, err := db.Exec("UPDATE product_variants SET min_cart_qty=2, stock=1 WHERE id='" + cheap + "'"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("UPDATE product_variants SET min_cart_qty=1, stock=2 WHERE id='" + cheap + "'")
	// The old post-commit read already sees the new stock, even while
	// this request's indexed key still carries the old price.
	live, err := catalogRepo.GetVariantsByProductIDs(ctx, []string{selected.Keys[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	var liveStock int
	for _, v := range live[selected.Keys[0].ID] {
		if v.ID == cheap {
			liveStock = v.Stock
		}
	}
	if liveStock != 1 {
		t.Fatalf("expected former reader to see new stock, got %d", liveStock)
	}
	products, variants, _, err := catalogRepo.LoadCatalogPageRowsTx(ctx, tx, []string{selected.Keys[0].ID}, "")
	if err != nil || len(products) != 1 {
		t.Fatalf("hydrate snapshot products=%v err=%v", products, err)
	}
	var oldStock int
	for _, v := range variants[products[0].ID] {
		if v.ID == cheap {
			oldStock = v.Stock
		}
	}
	if oldStock != 2 {
		t.Fatalf("same snapshot hydrated stock=%d, expected 2", oldStock)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after := perf01DecodeList(t, perf01HTTPGet(t, handler, path))
	if len(after.Items) != 0 {
		t.Fatalf("fresh request must exclude repriced product: %+v", after)
	}
}

func TestPerf01HTTPVariantCampaignScope(t *testing.T) {
	cat, stores := perf01HTTPRepos(t)
	handler := perf01ListHandler(t, cat, stores)
	ctx := context.Background()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	defer exec("DELETE FROM seller_discount_campaigns WHERE id IN ('parity-product-campaign','parity-variant-campaign')")
	const chosen = "22222222-2222-2222-2222-222222222222"
	const other = "11111111-1111-1111-1111-111111111111"
	const base = "category=perf01-cat&search=Product%20001"
	list := func(query string) []ProductResponse {
		return perf01DecodeList(t, perf01HTTPGet(t, handler, "/api/v1/products?"+query)).Items
	}
	exec(`INSERT INTO stores (id,name,slug,owner_type,owner_name) VALUES ('price-parity-store-b','Parity B','price-parity-store-b','dropshipper','B');
		INSERT INTO store_products (store_id,product_id,position) VALUES ('price-parity-store-b','http-001',1)`)
	defer exec("DELETE FROM stores WHERE id='price-parity-store-b'")
	b := list(base + "&storeSlug=price-parity-store-b")[0]
	a := list(base + "&storeSlug=store-a")[0]
	c := list(base)[0]
	exec(`INSERT INTO seller_discount_campaigns(id,store_id,name,scope,target_id,mode,value,starts_at,ends_at,is_active) VALUES
		('parity-product-campaign','store-a','Product campaign','product','http-001','percent',12, NOW()-INTERVAL '1 hour',NOW()+INTERVAL '1 hour',TRUE)`)
	withProduct := list(base + "&storeSlug=store-a")[0]
	exec(`INSERT INTO seller_discount_campaigns(id,store_id,name,scope,target_id,mode,value,starts_at,ends_at,is_active) VALUES
		('parity-variant-campaign','store-a','Variant campaign','variant','` + chosen + `','percent',5,NOW()-INTERVAL '1 hour',NOW()+INTERVAL '2 hour',TRUE)`)
	selected, err := stores.GetActiveDiscountCampaign(ctx, "store-a", "http-001", nil, chosen)
	if err != nil || selected == nil || selected.ID != "parity-variant-campaign" {
		t.Fatalf("checkout precedence selected %+v err %v", selected, err)
	}
	p := list(base + "&storeSlug=store-a")[0]
	discounted := perf01CardPrice(p)
	if discounted == perf01CardPrice(withProduct) || discounted >= perf01CardPrice(a) ||
		p.VariantPrices[other].Price != withProduct.VariantPrices[other].Price {
		t.Fatalf("variant promotion must replace product campaign only for its target: before=%+v after=%+v", withProduct.VariantPrices, p.VariantPrices)
	}
	indexed := list(base + fmt.Sprintf("&storeSlug=store-a&sort=cheapest&min_price=%d&max_price=%d", discounted, discounted))
	if len(indexed) != 1 || perf01CardPrice(indexed[0]) != discounted {
		t.Fatalf("variant promotion missing from narrow index range: %+v", indexed)
	}
	rec := perf01HTTPGet(t, func(w http.ResponseWriter, r *http.Request) { handleProductPriceBounds(w, r, ctx, cat, stores, nil) }, "/api/v1/products/price-bounds?"+base+"&storeSlug=store-a")
	var bounds productPriceBoundsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &bounds); rec.Code != 200 || err != nil || bounds.MinPrice != discounted || bounds.MaxPrice != 16200 || bounds.Count != 2 {
		t.Fatalf("index bounds must match promoted card; status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := list(base)[0]; perf01CardPrice(got) != perf01CardPrice(c) || got.VariantPrices[chosen].Price != c.VariantPrices[chosen].Price {
		t.Fatal("seller-specific variant promotion leaked to central scope")
	}
	if got := list(base + "&storeSlug=price-parity-store-b")[0]; perf01CardPrice(got) != perf01CardPrice(b) || got.VariantPrices[chosen].CampaignName != "" {
		t.Fatal("seller-specific variant promotion leaked to another seller")
	}
	exec("UPDATE seller_discount_campaigns SET is_active=FALSE WHERE id='parity-variant-campaign'")
	if got := list(base + fmt.Sprintf("&storeSlug=store-a&min_price=%d&max_price=%d", discounted, discounted)); len(got) != 0 {
		t.Fatal("campaign deactivation did not invalidate index")
	}
}

func TestPerf01HTTPCampaignUsesOneTimeAndSnapshot(t *testing.T) {
	cat, stores := perf01HTTPRepos(t)
	ctx := context.Background()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const variant = "22222222-2222-2222-2222-222222222222"
	const id = "price-parity-clock"
	if _, err := db.Exec(`INSERT INTO seller_discount_campaigns(id,store_id,name,scope,target_id,mode,value,starts_at,ends_at,is_active)
		VALUES ($1,'store-a','Snapshot campaign','variant',$2,'percent',5,NOW()-INTERVAL '1 hour',NOW()+INTERVAL '1 hour',TRUE)`, id, variant); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM seller_discount_campaigns WHERE id=$1", id)
	tx, err := cat.BeginCatalogTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	inputs, err := cat.LoadCatalogScopeInputsTx(ctx, tx, "store-a")
	if err != nil || len(inputs.Campaigns) != 1 {
		t.Fatalf("campaign snapshot %+v / %v", inputs, err)
	}
	c := inputs.Campaigns[0]
	if len(activeCatalogCampaignsAt(inputs, c.StartsAt.Add(-time.Nanosecond))) != 0 ||
		len(activeCatalogCampaignsAt(inputs, c.StartsAt)) != 1 ||
		len(activeCatalogCampaignsAt(inputs, c.EndsAt.Add(-time.Nanosecond))) != 1 ||
		len(activeCatalogCampaignsAt(inputs, c.EndsAt)) != 0 {
		t.Fatal("start must be inclusive and end exclusive using the same catalog clock")
	}
	var dbNow time.Time
	if err := tx.QueryRowContext(ctx, "SELECT CURRENT_TIMESTAMP").Scan(&dbNow); err != nil || !dbNow.Equal(inputs.AsOf) {
		t.Fatalf("price clock must equal SQL expiry check clock: %v / %v / %v", inputs.AsOf, dbNow, err)
	}
	p, variants, _, err := cat.LoadCatalogPageRowsTx(ctx, tx, []string{"http-001"}, "store-a")
	if err != nil || len(p) != 1 {
		t.Fatalf("page hydration: %+v / %v", p, err)
	}
	if _, err := db.Exec("UPDATE seller_discount_campaigns SET is_active=false WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	price := priceVariantsForResponseAt(p[0], variants[p[0].ID], inputs.Store, inputs.Settings, inputs.Locks, inputs.Overrides, activeCatalogCampaignsAt(inputs, inputs.AsOf), inputs.AsOf)[variant]
	if price.CampaignName != "Snapshot campaign" {
		t.Fatalf("same response lost its selected campaign after external write: %+v", price)
	}
	page := perf01DecodeList(t, perf01HTTPGet(t, perf01ListHandler(t, cat, stores), "/api/v1/products?storeSlug=store-a&search=Product%20001&sort=cheapest"))
	if len(page.Items) != 1 || page.Items[0].VariantPrices[variant].CampaignName != "" || page.Items[0].VariantPrices[variant].Price <= price.Price {
		t.Fatalf("fresh HTTP request must see deactivation and new variant price: %+v", page)
	}
}
