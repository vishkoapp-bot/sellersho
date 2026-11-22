package main

// Batch 2 HTTP integration tests (A1 ranking/normalisation, E2 logging
// guard). They run ONLY when TEAM04_TEST_DATABASE_URL is set to a
// DISPOSABLE local Postgres (never the live database); otherwise they
// skip. Each test re-seeds the team04 fixture, then adds its own rows
// through a second connection to the same disposable DSN.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
)

func batch2TestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEAM04_TEST_DATABASE_URL not set; skipping disposable-DB HTTP test")
	}
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// batch2HTTPRepos seeds the team04 fixture, then removes the two
// colliding product-variant rows. team04 and perf01 fixtures share the
// variant UUIDs 11111111-…/22222222-… (prod-active-1 vs http-001), and
// perf01 seeds with ON CONFLICT (id) DO NOTHING: if the team04 rows are
// present when perf01 seeds, http-001 silently gets no variants and
// TestPerf01HTTPListEnvelope fails on its variant range. This file
// sorts before catalog_perf01_http_test.go, so without the delete the
// full-package run breaks perf01. These tests never read variant
// prices, so dropping the two rows is safe here.
func batch2HTTPRepos(t *testing.T) (*repository.CatalogRepository, *repository.StoreRepository, *sql.DB) {
	t.Helper()
	catalogRepo, storeRepo := team04HTTPRepos(t)
	db := batch2TestDB(t)
	if _, err := db.Exec(`DELETE FROM product_variants WHERE id IN ('11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222222')`); err != nil {
		t.Fatalf("drop colliding variants: %v", err)
	}
	return catalogRepo, storeRepo, db
}

func batch2SearchIDs(t *testing.T, handler http.HandlerFunc, path string) (ids []string, total int) {
	t.Helper()
	rec := team04Get(t, handler, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, body %s", path, rec.Code, rec.Body.String())
	}
	var env struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("GET %s decode: %v", path, err)
	}
	for _, it := range env.Items {
		ids = append(ids, it.ID)
	}
	return ids, env.Total
}

func batch2SeedSearchProducts(t *testing.T, db *sql.DB) {
	t.Helper()
	// ZWNJ (U+200C) and Arabic Yeh (U+064A) are written as Go escapes so
	// the seeded bytes are unambiguous.
	zwnjTitle := "هندز\u200cفری بلوتوثی تست"
	arabicTitle := "هندزفر\u064a بی\u200cسیم تست"
	brandName := "ساعت\u200cسازان تست"
	seed := []string{
		`INSERT INTO categories (id, title, slug, icon_name) VALUES ('cat-saat','ساعت و زیورآلات','cat-saat','box') ON CONFLICT (id) DO NOTHING`,
		fmt.Sprintf(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-zwnj-1','%s','zwnj-1','digital','sup-1',1000000,800000,0,TRUE,5,'img','{}','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'',10000101,120,80,40,500)`, zwnjTitle),
		fmt.Sprintf(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-arabic-1','%s','arabic-1','digital','sup-1',1000000,800000,0,TRUE,5,'img','{}','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'',10000102,120,80,40,500)`, arabicTitle),
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-desc-1','اسپیکر بلوتوثی تست','desc-1','digital','sup-1',1000000,800000,0,TRUE,5,'img','{}','پشتیبانی ۲۴ ساعته','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'',10000103,120,80,40,500)`,
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-title-1','ساعت هوشمند تست','title-1','digital','sup-1',1000000,800000,0,TRUE,5,'img','{}','بدون کلمه کلیدی','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'',10000104,120,80,40,500)`,
		fmt.Sprintf(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-brand-1','محصول برند تست','brand-1','digital','sup-1',1000000,800000,0,TRUE,5,'img','{}','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'%s',10000105,120,80,40,500)`, brandName),
		`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,image_url,specs,description,warehouse_name,preparation_days,supplier_warranty,is_featured,is_special_deal,fulfillment_method,supply_option,is_active,brand,spn_number,length_mm,width_mm,height_mm,weight_grams) VALUES ('prod-cat-1','محصول دسته تست','cat-1','cat-saat','sup-1',1000000,800000,0,TRUE,5,'img','{}','desc','انبار مرکزی تهران',1,'گارانتی',FALSE,FALSE,'warehouse','1day',TRUE,'',10000106,120,80,40,500)`,
	}
	for _, q := range seed {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("search seed failed: %v\n%s", err, q)
		}
	}
}

func batch2Has(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestBatch2SearchRankingHTTP(t *testing.T) {
	catalogRepo, storeRepo, db := batch2HTTPRepos(t)
	batch2SeedSearchProducts(t, db)
	ctx := context.Background()
	listHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, ctx, catalogRepo, storeRepo, nil)
	}
	search := func(term string) ([]string, int) {
		return batch2SearchIDs(t, listHandler, "/api/v1/products?search="+url.QueryEscape(term)+"&limit=24")
	}

	// (a) plain query matches both the ZWNJ-form and Arabic-Yeh-form titles.
	ids, total := search("هندزفری")
	if total != 2 || !batch2Has(ids, "prod-zwnj-1") || !batch2Has(ids, "prod-arabic-1") {
		t.Fatalf("(a) search هندزفری = %v total=%d, want [prod-zwnj-1 prod-arabic-1]", ids, total)
	}
	// (b) ZWNJ-form, Arabic-Yeh-form and space-form queries match the same two rows.
	for _, q := range []string{"هندز\u200cفری", "هندزفر\u064a", "هندز فری"} {
		ids, total := search(q)
		if total != 2 || !batch2Has(ids, "prod-zwnj-1") || !batch2Has(ids, "prod-arabic-1") {
			t.Fatalf("(b) search %q = %v total=%d, want both normalised rows", q, ids, total)
		}
	}
	// (c)+(f) ساعت: title first, then brand/category; description-only is excluded.
	ids, total = search("ساعت")
	if total != 3 || len(ids) != 3 || ids[0] != "prod-title-1" || batch2Has(ids, "prod-desc-1") {
		t.Fatalf("(c) search ساعت = %v total=%d, want 3 with title first and no description-only match", ids, total)
	}
	if !batch2Has(ids[1:3], "prod-brand-1") || !batch2Has(ids[1:3], "prod-cat-1") {
		t.Fatalf("(f) brand/category must follow the title match: %v", ids)
	}
	if ids, total := search("پشتیبانی"); total != 0 || len(ids) != 0 {
		t.Fatalf("description-only search = %v total=%d, want 0", ids, total)
	}
	// (d) pinned: طلا still returns exactly the one active gold product.
	ids, total = search("طلا")
	if total != 1 || len(ids) != 1 || ids[0] != "prod-active-1" {
		t.Fatalf("(d) search طلا = %v total=%d, want [prod-active-1]", ids, total)
	}
	// (e) pinned: گوشی returns 0 — the only گوشی row is inactive.
	ids, total = search("گوشی")
	if total != 0 || len(ids) != 0 {
		t.Fatalf("(e) search گوشی = %v total=%d, want 0 (is_active gate unchanged)", ids, total)
	}
	// (g) cursor paging over the mixed-rank ساعت search enumerates each
	// matching row exactly once, in rank order, across the boundary.
	seen := map[string]bool{}
	var order []string
	cursor := ""
	for {
		p := "/api/v1/products?search=" + url.QueryEscape("ساعت") + "&limit=2"
		if cursor != "" {
			p += "&cursor=" + cursor
		}
		rec := team04Get(t, listHandler, p)
		if rec.Code != http.StatusOK {
			t.Fatalf("(g) page status = %d, body %s", rec.Code, rec.Body.String())
		}
		var env struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			NextCursor *string `json:"nextCursor"`
			HasMore    bool    `json:"hasMore"`
			Total      int     `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("(g) decode: %v", err)
		}
		if env.Total != 3 {
			t.Fatalf("(g) total=%d, want 3", env.Total)
		}
		if cursor == "" && (len(env.Items) != 2 || !env.HasMore) {
			t.Fatalf("(g) first page has %d items, hasMore=%t", len(env.Items), env.HasMore)
		}
		if cursor != "" && (len(env.Items) != 1 || env.HasMore) {
			t.Fatalf("(g) second page has %d items, hasMore=%t", len(env.Items), env.HasMore)
		}
		for _, it := range env.Items {
			if seen[it.ID] {
				t.Fatalf("(g) duplicate %s while paging", it.ID)
			}
			seen[it.ID] = true
			order = append(order, it.ID)
		}
		if !env.HasMore {
			break
		}
		if env.NextCursor == nil || *env.NextCursor == "" {
			t.Fatal("(g) hasMore without a nextCursor")
		}
		cursor = *env.NextCursor
	}
	if len(order) != 3 || order[0] != "prod-title-1" || !seen["prod-brand-1"] || !seen["prod-cat-1"] || seen["prod-desc-1"] {
		t.Fatalf("(g) paged order = %v, want title first followed by brand and category", order)
	}
}

func TestBatch2SearchLoggingGuardHTTP(t *testing.T) {
	catalogRepo, storeRepo, db := batch2HTTPRepos(t)
	ctx := context.Background()
	listHandler := func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, ctx, catalogRepo, storeRepo, nil)
	}
	logCount := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM search_queries`).Scan(&n); err != nil {
			t.Fatalf("log count: %v", err)
		}
		return n
	}
	lastTerm := func() string {
		t.Helper()
		var term string
		if err := db.QueryRow(`SELECT term_normalized FROM search_queries ORDER BY id DESC LIMIT 1`).Scan(&term); err != nil {
			t.Fatalf("last term: %v", err)
		}
		return term
	}
	get := func(path string) int {
		t.Helper()
		rec := team04Get(t, listHandler, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, body %s", path, rec.Code, rec.Body.String())
		}
		var env struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("GET %s decode: %v", path, err)
		}
		return env.Total
	}
	if n := logCount(); n != 0 {
		t.Fatalf("search log must start empty, got %d rows", n)
	}
	// E2-1: a 1-rune search still returns results but writes no row.
	// (دستبند contains س, so the listing itself is non-empty.)
	if total := get("/api/v1/products?search=" + url.QueryEscape("س")); total != 1 {
		t.Fatalf("1-rune search total=%d, want 1 (matching unaffected)", total)
	}
	if n := logCount(); n != 0 {
		t.Fatalf("1-rune search wrote %d log rows, want 0", n)
	}
	// E2-2: a 2-rune search writes exactly one row.
	if total := get("/api/v1/products?search=" + url.QueryEscape("گل")); total != 0 {
		t.Fatalf("2-rune search total=%d, want 0", total)
	}
	if n := logCount(); n != 1 {
		t.Fatalf("2-rune search wrote %d log rows, want 1", n)
	}
	if term := lastTerm(); term != "گل" {
		t.Fatalf("logged term = %q, want گل", term)
	}
	// E2-3: suggest=1 writes no row; the same search without the flag writes one.
	if total := get("/api/v1/products?search=" + url.QueryEscape("ساعت") + "&suggest=1"); total != 0 {
		t.Fatalf("suggest search total=%d, want 0", total)
	}
	if n := logCount(); n != 1 {
		t.Fatalf("suggest=1 search wrote a log row (count=%d, want 1)", n)
	}
	if total := get("/api/v1/products?search=" + url.QueryEscape("ساعت")); total != 0 {
		t.Fatalf("plain search total=%d, want 0", total)
	}
	if n := logCount(); n != 2 {
		t.Fatalf("plain search wrote %d log rows total, want 2", n)
	}
	if term := lastTerm(); term != "ساعت" {
		t.Fatalf("logged term = %q, want ساعت", term)
	}
}
