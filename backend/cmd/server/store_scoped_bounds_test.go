package main

// Store-scoped price-bounds regression tests. These run ONLY when
// TEAM04_TEST_DATABASE_URL is set to a DISPOSABLE local Postgres
// (never the live database); otherwise they skip. They reuse the
// storeScopedRepos seed harness from store_scoped_list_test.go:
// dropshipper "shopx" whose beauty assortment is p-old-1 (cost
// 10,000,000, globally inactive), p-old-2 (cost 20,000,000, globally
// inactive, 20% product override) and p-old-3 (cost 5,000,000,
// active), plus 45 newer unrelated active beauty products at cost
// 1,000,000 and one inactive outsider.
//
// Bug covered: GET /api/v1/products/price-bounds?storeSlug=<store>
// priced the GLOBAL active assortment with the store's resolver, so
// the facets described foreign products (here: 46 items, min
// 1,350,000 from the noise set) while the storefront listed only the
// store's own three. The fix mirrors handleProductsList: a
// non-central dropshipper slug scopes the priced set to the store's
// assortment and applies the deals-widget display discount, so the
// bounds equal the min/max/count of the prices on the cards.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/drup/platform/internal/repository"
	_ "github.com/lib/pq"
)

type boundsDTO struct {
	MinPrice int64 `json:"minPrice"`
	MaxPrice int64 `json:"maxPrice"`
	Count    int   `json:"count"`
}

func storeScopedBounds(t *testing.T, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, path string) boundsDTO {
	t.Helper()
	ctx := context.Background()
	handler := func(w http.ResponseWriter, r *http.Request) {
		handleProductPriceBounds(w, r, ctx, catalogRepo, storeRepo, nil)
	}
	rec := team04Get(t, handler, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, body %s", path, rec.Code, rec.Body.String())
	}
	var b boundsDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("GET %s decode: %v", path, err)
	}
	return b
}

// listBounds derives bounds from the store list's own card prices,
// the ground truth the facets must agree with.
func listBounds(t *testing.T, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, path string) boundsDTO {
	t.Helper()
	list := storeScopedList(t, catalogRepo, storeRepo, path)
	if len(list) == 0 {
		return boundsDTO{}
	}
	b := boundsDTO{Count: len(list)}
	for i, item := range list {
		price := listPrice(t, item)
		if i == 0 || price < b.MinPrice {
			b.MinPrice = price
		}
		if i == 0 || price > b.MaxPrice {
			b.MaxPrice = price
		}
	}
	return b
}

func TestStoreScopedPriceBoundsMatchStorefront(t *testing.T) {
	catalogRepo, storeRepo := storeScopedRepos(t)

	// Scoped bounds: exactly the store's own assortment at store
	// prices (fallback settings: reference = cost; p-old-1 35%
	// max-default = 13,500,000; p-old-2 20% override = 24,000,000;
	// p-old-3 35% = 6,750,000). The old code answered
	// {1,350,000, 6,750,000, 46} here — foreign noise prices.
	scoped := storeScopedBounds(t, catalogRepo, storeRepo, "/api/v1/products/price-bounds?storeSlug=shopx&category=beauty")
	if want := (boundsDTO{MinPrice: 6750000, MaxPrice: 24000000, Count: 3}); scoped != want {
		t.Fatalf("scoped bounds = %+v, want %+v", scoped, want)
	}

	// The bounds must equal the min/max/count of the list's card
	// prices for the same store view.
	if lb := listBounds(t, catalogRepo, storeRepo, "/api/v1/products/?storeSlug=shopx"); lb != scoped {
		t.Fatalf("scoped bounds = %+v, list-derived bounds = %+v, want equal", scoped, lb)
	}

	// A category the store does not carry: empty bounds, not the
	// global category set.
	empty := storeScopedBounds(t, catalogRepo, storeRepo, "/api/v1/products/price-bounds?storeSlug=shopx&category=digital")
	if empty != (boundsDTO{}) {
		t.Fatalf("scoped empty-category bounds = %+v, want zero bounds", empty)
	}

	// Unscoped, central and unknown slugs keep the previous global
	// behaviour: active products only (45 noise + p-old-3) at central
	// prices (cost x 1.10): {1,100,000, 5,500,000, 46}.
	wantGlobal := boundsDTO{MinPrice: 1100000, MaxPrice: 5500000, Count: 46}
	for _, path := range []string{
		"/api/v1/products/price-bounds?category=beauty",
		"/api/v1/products/price-bounds?storeSlug=central&category=beauty",
		"/api/v1/products/price-bounds?storeSlug=no-such-store&category=beauty",
	} {
		if got := storeScopedBounds(t, catalogRepo, storeRepo, path); got != wantGlobal {
			t.Fatalf("GET %s bounds = %+v, want %+v (global behaviour unchanged)", path, got, wantGlobal)
		}
	}

	// With a live deals-widget campaign (5 points off p-old-2's 20%
	// override margin -> 15% -> 23,000,000), the scoped bounds must
	// track the discounted card price, exactly as the list shows it.
	dsn := os.Getenv("TEAM04_TEST_DATABASE_URL")
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open test DB for widget seed: %v", err)
	}
	defer db.Close()
	cfg := `{"title":"کمپین","productIds":["p-old-2"],"countdownHours":24,"endsAt":"2999-01-01T00:00:00Z","discounts":{"p-old-2":5}}`
	if _, err := db.Exec(`UPDATE stores SET seller_deals_widget_config = $1::jsonb WHERE slug = 'shopx'`, cfg); err != nil {
		t.Fatalf("seed widget config: %v", err)
	}
	discounted := storeScopedBounds(t, catalogRepo, storeRepo, "/api/v1/products/price-bounds?storeSlug=shopx&category=beauty")
	if want := (boundsDTO{MinPrice: 6750000, MaxPrice: 23000000, Count: 3}); discounted != want {
		t.Fatalf("discounted scoped bounds = %+v, want %+v", discounted, want)
	}
	if lb := listBounds(t, catalogRepo, storeRepo, "/api/v1/products/?storeSlug=shopx"); lb != discounted {
		t.Fatalf("discounted bounds = %+v, list-derived bounds = %+v, want equal", discounted, lb)
	}
}
