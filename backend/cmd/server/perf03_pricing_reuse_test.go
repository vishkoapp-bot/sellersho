package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/testdb"
)

// PERF-03 G2: one catalog list request must load the pricing settings
// (4 queries), the store margin locks (1) and the store variant
// overrides (1) exactly ONCE — the scope transaction's load is reused
// for response pricing instead of being loaded a second time in the
// handler. Proven by pg_stat_statements call deltas, not code reading.
// Runs ONLY against a disposable migrated database.
func TestPERF03CatalogListLoadsPricingInputsOnce(t *testing.T) {
	dsn := os.Getenv("SELLERSHO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SELLERSHO_TEST_DATABASE_URL is not set; skipping disposable-DB integration test")
	}
	catalogRepo, storeRepo := perf01HTTPRepos(t)
	db, err := testdb.Open(t, dsn)
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	var hasStats int
	if err := db.QueryRow(`SELECT count(*) FROM pg_extension WHERE extname = 'pg_stat_statements'`).Scan(&hasStats); err != nil || hasStats == 0 {
		t.Skip("pg_stat_statements is not installed in this disposable database; cannot count statements")
	}
	pricingRepo := repository.NewPricingRepository(db)

	stmtCalls := func(like string) int64 {
		t.Helper()
		var n int64
		if err := db.QueryRow(`SELECT COALESCE(SUM(calls), 0) FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database()) AND query LIKE $1`, "%"+like+"%").Scan(&n); err != nil {
			t.Fatalf("pg_stat_statements read (%s): %v", like, err)
		}
		return n
	}
	// Patterns match the SELECT list of the standalone load query, not
	// just the table: pg_stat_statements normalises literals (so the
	// settings row reads "WHERE id = $1", not "id = 1"), and the price
	// fingerprint probe legitimately reads store_variant_overrides
	// inside a different query — that read is part of page selection,
	// not the handler/tx duplication this test measures.
	queries := map[string]string{
		"settings row":       "central_profit_pct%FROM platform_settings WHERE id = $1",
		"ladder products":    "SELECT product_id, pct FROM platform_pct_products",
		"ladder categories":  "SELECT category_slug, pct FROM platform_pct_categories",
		"margin ceilings":    "SELECT product_id, max_margin_pct FROM product_margin_ceilings",
		"store margin locks": "SELECT scope, scope_id, lock_mode, lock_value%FROM store_margin_locks WHERE store_id = $1",
		"variant overrides":  "SELECT store_id, product_id, variant_id, override_value, mode%FROM store_variant_overrides WHERE store_id = $1",
	}
	before := map[string]int64{}
	for name, like := range queries {
		before[name] = stmtCalls(like)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products?limit=20", nil)
	rec := httptest.NewRecorder()
	handleProductsList(rec, req, context.Background(), catalogRepo, storeRepo, pricingRepo)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	var page ProductListPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode list envelope: %v (body %s)", err, rec.Body.String())
	}
	if len(page.Items) == 0 {
		t.Fatal("list returned no items; the statement deltas below would be meaningless")
	}

	for name, like := range queries {
		if got := stmtCalls(like) - before[name]; got != 1 {
			t.Errorf("%s: %d calls per list request, want exactly 1 (request-local reuse)", name, got)
		}
	}
}
