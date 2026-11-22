package repository

// Pre-filter tests (prefilter-soundness.md §8 item 1): the §1.3
// first-literal scan and the §1.4 pre-image sets, plus DB-backed
// behaviour through the real predicate (CountCatalogCandidatesTx /
// GetPricedProductPageTx). The pure-Go tests run without a database;
// the DB test runs through the isolated runner like the other
// repository tests.
import (
	"context"
	"strings"
	"testing"

	"github.com/drup/platform/internal/catalogpage"
)

func TestPrefilterFirstLiteral(t *testing.T) {
	cases := []struct {
		strippedLower string
		want          rune
		wantOK        bool
	}{
		{"ساعت", 'س', true},
		{"طلا", 'ط', true},
		{"product", 'p', true},
		{"product_001", 'p', true}, // '_' at position 7 is not first
		{"%ساعت", 'س', true},       // leading wildcard skipped
		{"_ساعت", 'س', true},
		{"%%", 0, false},           // no literal: fail open
		{"_%", 0, false},           // no literal: fail open
		{"", 0, false},             // no literal: fail open
		{"\\", 0, false},           // lone trailing backslash: fail open
		{"\\%", '%', true},         // escaped % is a literal
		{"\\_", '_', true},         // escaped _ is a literal
		{"\\\\", '\\', true},       // escaped backslash is a literal
		{"\u0307", 0, false},       // combining mark: fail open
		{"\u200d", '\u200d', true}, // ZWJ is a literal (singleton)
	}
	for _, tc := range cases {
		got, ok := firstPrefilterLiteral(tc.strippedLower)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("firstPrefilterLiteral(%q) = %q,%v; want %q,%v", tc.strippedLower, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestPrefilterPreimages(t *testing.T) {
	cases := []struct {
		f    rune
		want string
	}{
		{'ی', "یي"},  // Yeh pair, both directions
		{'ک', "کك"},  // Kaf pair, both directions
		{'p', "pP"},  // Latin case both directions
		{'i', "iIİ"}, // + İ extra (SimpleFold alone misses it)
		{'k', "kKK"}, // K in the fold orbit
		{'å', "åÅÅ"}, // Å in the fold orbit
		{'ß', "ßẞ"},  // ẞ (ToUpper('ß') is 'ß' — shortcut misses it)
		{'σ', "σΣς"}, // sigma fold class
		{'ς', "ςσΣ"}, // final sigma fold class
		{'س', "س"},   // Persian singleton
		{'3', "3"},   // digit singleton
		{'%', "%"},   // escaped-literal singleton
		{'_', "_"},   // escaped-literal singleton
	}
	for _, tc := range cases {
		got, ok := prefilterPreimages(tc.f)
		if !ok {
			t.Errorf("prefilterPreimages(%q) = fail-open; want %q", tc.f, tc.want)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("prefilterPreimages(%q) = %q; want %q", tc.f, string(got), tc.want)
		}
	}
	if _, ok := prefilterPreimages('\u0307'); ok {
		t.Errorf("prefilterPreimages(U+0307) should fail open")
	}
}

// TestPrefilterSQLVacuous checks the pre-filter is vacuous (no
// POSITION fragment) exactly for the fail-open cases, and present
// otherwise.
func TestPrefilterSQLVacuous(t *testing.T) {
	arg := func(v any) string { return "$1" }
	vacuous := []string{"%", "_", "%%", "\\", "\u0307"}
	for _, term := range vacuous {
		if sql, ok := searchPrefilterSQL(term, arg); ok {
			t.Errorf("searchPrefilterSQL(%q) = %q, want vacuous", term, sql)
		}
	}
	for _, term := range []string{"ساعت", "Product", "\\%", "\\_"} {
		sql, ok := searchPrefilterSQL(term, arg)
		if !ok || !strings.Contains(sql, "POSITION(") {
			t.Errorf("searchPrefilterSQL(%q) = %q,%v; want a POSITION pre-filter", term, sql, ok)
		}
	}
}

// TestPrefilterDBBehaviour exercises the pre-filter through the real
// predicate: Yeh/Kaf both directions, Latin case both directions,
// Product_001 vs Product 001, single-char % / _ / \ / \% / \_,
// whitespace/ZWNJ-only → 0 rows, and a category-only row still
// returned and counted.
func TestPrefilterDBBehaviour(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	stmts := []string{
		`CREATE TABLE categories (LIKE public.categories INCLUDING ALL)`,
		`INSERT INTO categories(id,title,slug,icon_name) VALUES(gen_random_uuid(),'ساعت و زیورآلات','cat-saat','')`,
		`INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_active,is_featured,is_original,brand,description,stock,created_at) VALUES
			('pf-yeh-fa','یک محصول','pf-yeh-fa','cat-odd','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','',5,'2026-01-01T00:00:00Z'),
			('pf-yeh-ar','يك محصول','pf-yeh-ar','cat-odd','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','',5,'2026-01-01T00:00:00Z'),
			('pf-kaf-fa','کتاب تست','pf-kaf-fa','cat-odd','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','',5,'2026-01-01T00:00:00Z'),
			('pf-kaf-ar','كتاب تست','pf-kaf-ar','cat-odd','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','',5,'2026-01-01T00:00:00Z'),
			('pf-lat-upper','Product_001','pf-lat-upper','cat-odd','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','',5,'2026-01-01T00:00:00Z'),
			('pf-lat-space','Product 001','pf-lat-space','cat-odd','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','',5,'2026-01-01T00:00:00Z'),
			('pf-cat-only','محصول بدون تطابق','pf-cat-only','cat-saat','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','',5,'2026-01-01T00:00:00Z'),
			('pf-desc','چیز دیگر','pf-desc','cat-odd','perf01-active',100000,20000,0,TRUE,FALSE,TRUE,'','دارای ساعت داخلی',5,'2026-01-01T00:00:00Z')`,
	}
	for _, q := range stmts {
		if _, err := fx.tx.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	scope := catalogpage.Scope{ID: "perf01-central", OwnerType: "central", Revision: 1}
	count := func(term string) int {
		t.Helper()
		n, err := fx.repo.CountCatalogCandidatesTx(ctx, fx.tx, scope, catalogpage.Filter{Search: term, Sort: "featured"})
		if err != nil {
			t.Fatalf("count(%q): %v", term, err)
		}
		return n
	}
	// Yeh both directions: Arabic query finds Persian title and vice versa.
	if n := count("يك"); n != 2 {
		t.Errorf("count(يك Arabic Yeh) = %d, want 2 (both Yeh rows)", n)
	}
	if n := count("یک"); n != 2 {
		t.Errorf("count(یک Persian Yeh) = %d, want 2", n)
	}
	// Kaf both directions.
	if n := count("كتاب"); n != 2 {
		t.Errorf("count(كتاب Arabic Kaf) = %d, want 2", n)
	}
	if n := count("کتاب"); n != 2 {
		t.Errorf("count(کتاب Persian Kaf) = %d, want 2", n)
	}
	// Latin case both directions: 'product' finds both Product rows.
	if n := count("product"); n != 2 {
		t.Errorf("count(product lower) = %d, want 2", n)
	}
	// Product_001 (underscore wildcard) matches both 'Product_001' and 'Product 001'.
	if n := count("Product_001"); n != 2 {
		t.Errorf("count(Product_001) = %d, want 2", n)
	}
	// Product 001 (literal space) matches only the spaced title, not the underscore one.
	if n := count("Product 001"); n != 1 {
		t.Errorf("count(Product 001) = %d, want 1", n)
	}
	// Escaped underscore is a literal: only the underscore title matches.
	if n := count("\\_"); n != 1 {
		t.Errorf("count(\\_) = %d, want 1 (Product_001 only)", n)
	}
	// Escaped percent is a literal: no title contains '%'.
	if n := count("\\%"); n != 0 {
		t.Errorf("count(\\%%) = %d, want 0", n)
	}
	// Single-char '%' / '_' / '\' must not error (fail-open pre-filter).
	for _, term := range []string{"%", "_", "\\"} {
		if n := count(term); n < 0 {
			t.Errorf("count(%q) = %d", term, n)
		}
	}
	// Whitespace-only and ZWNJ-only queries → 0 rows (empty-check first,
	// never leak category matches).
	if n := count("   "); n != 0 {
		t.Errorf("count(whitespace-only) = %d, want 0", n)
	}
	if n := count("‌"); n != 0 {
		t.Errorf("count(ZWNJ-only) = %d, want 0", n)
	}
	// ساعت: only the category match is returned and counted; pf-desc is excluded.
	if n := count("ساعت"); n != 1 {
		t.Errorf("count(ساعت) = %d, want 1 (category-only)", n)
	}
	filter := catalogpage.Filter{Search: "ساعت", Sort: "featured"}
	page, err := fx.repo.GetPricedProductPageTx(ctx, fx.tx, scope, filter, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Keys) != 1 || page.Keys[0].ID != "pf-cat-only" {
		t.Fatalf("page(ساعت): total=%d keys=%v, want pf-cat-only", page.Total, page.Keys)
	}
	second, err := fx.repo.GetPricedProductPageTx(ctx, fx.tx, scope, filter, 1, &page.Keys[0])
	if err != nil {
		t.Fatal(err)
	}
	// An empty page has no row to carry the window total (catalog_price_index.go:891).
	if second.Total != 0 || len(second.Keys) != 0 {
		t.Errorf("page 2: total=%d keys=%v, want total 0 (window total has no row to carry it) and no rows", second.Total, second.Keys)
	}
}
