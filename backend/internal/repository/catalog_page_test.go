package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/catalogpage"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func catalogCandidateFixture(t *testing.T) (*CatalogRepository, *sql.Tx) {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	schema := pq.QuoteIdentifier("catalog_page_" + uuid.NewString()[:8])
	if _, err := tx.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"products", "suppliers", "stores", "store_products", "niches", "product_niches", "product_attribute_values"} {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE `+schema+`.`+table+` (LIKE public.`+table+` INCLUDING ALL)`); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		`SET LOCAL search_path = ` + schema,
		`INSERT INTO stores(id,name,slug,owner_type,owner_name) VALUES('page-central','Central','page-central','central','Fixture'),('page-A','A','page-A','dropshipper','Fixture'),('page-B','B','page-B','dropshipper','Fixture')`,
		`INSERT INTO suppliers(id,name,approval_status) VALUES('page-active','Active','approved'),('page-suspended','Suspended','suspended')`,
		`INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_active,is_featured,is_original,brand,description,stock,created_at)
 SELECT 'page-prod-'||lpad(i::text,3,'0'),'Product '||i,'page-prod-'||i,CASE WHEN i%2=0 THEN 'page-even' ELSE 'page-odd' END,
 CASE WHEN i=250 THEN 'page-suspended' ELSE 'page-active' END,1000,500,10,i<>249,CASE WHEN i%10=0 THEN NULL ELSE i%2=0 END,i<>237,
 CASE WHEN i%2=0 THEN 'Beta' ELSE 'Alpha' END,CASE WHEN i%2=0 THEN 'Blue Shoe' ELSE 'Red Bag' END,CASE WHEN i=238 THEN 0 ELSE 5 END,'2026-01-01T00:00:00.123456Z'::timestamptz FROM generate_series(1,250) i`,
		`INSERT INTO store_products(store_id,product_id,position) VALUES('page-A','page-prod-249',-1),('page-A','page-prod-240',1),('page-A','page-prod-250',2),('page-B','page-prod-001',0)`,
		`INSERT INTO niches(id,title,slug,category_slug) VALUES('page-niche','Fixture','page-niche','page-even')`,
		`INSERT INTO product_niches(product_id,niche_id) VALUES('page-prod-239','page-niche'),('page-prod-240','page-niche')`,
		`INSERT INTO product_attribute_values(product_id,dk_attribute_id,dk_value_id) VALUES('page-prod-100',10,1),('page-prod-101',10,2),('page-prod-102',10,1),('page-prod-102',11,3)`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	return NewCatalogRepository(db), tx
}

func TestCatalogCandidateStablePagesEnumerateTiesExactlyOnce(t *testing.T) {
	r, tx := catalogCandidateFixture(t)
	scope := catalogpage.Scope{ID: "page-central", OwnerType: "central", Revision: 1}
	for _, sortBy := range []string{"featured", "newest"} {
		seen := map[string]bool{}
		var after *catalogpage.Key
		for iteration := 0; ; iteration++ {
			if iteration > 20 {
				t.Fatal("paging did not terminate")
			}
			page, err := r.GetProductCandidatePageTx(context.Background(), tx, scope, catalogpage.Filter{Sort: sortBy}, 24, after)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Keys) > 24 {
				t.Fatal("candidate page exceeded requested size")
			}
			if iteration == 0 {
				wantSecond := "page-prod-246"
				if sortBy == "newest" {
					wantSecond = "page-prod-247"
				}
				if len(page.Keys) < 2 || page.Keys[0].ID != "page-prod-248" || page.Keys[1].ID != wantSecond {
					t.Fatalf("%s ordering mismatch: %+v", sortBy, page.Keys)
				}
			}
			for _, key := range page.Keys {
				if seen[key.ID] {
					t.Fatalf("duplicate page member %s", key.ID)
				}
				seen[key.ID] = true
				if key.Price != 0 {
					t.Fatal("source candidate cursor contains an unpriced or protected cost")
				}
			}
			if !page.HasMore {
				break
			}
			if len(page.Keys) == 0 {
				t.Fatal("empty nonterminal page")
			}
			key := page.Keys[len(page.Keys)-1]
			token, err := catalogpage.Encode(scope, catalogpage.Filter{Sort: sortBy}, key)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := catalogpage.Decode(token, scope, catalogpage.Filter{Sort: sortBy})
			if err != nil {
				t.Fatal(err)
			}
			after = &decoded
		}
		if len(seen) != 248 {
			t.Fatalf("%s enumerated%d instead of248", sortBy, len(seen))
		}
		for i := 1; i <= 248; i++ {
			if !seen[fmt.Sprintf("page-prod-%03d", i)] {
				t.Fatalf("missing product%d", i)
			}
		}
	}
}

func TestCatalogCandidateScopeFiltersPrecedeLimit(t *testing.T) {
	r, tx := catalogCandidateFixture(t)
	central := catalogpage.Scope{ID: "page-central", OwnerType: "central", Revision: 1}
	falseValue := false
	for _, tc := range []struct {
		filter catalogpage.Filter
		want   string
	}{
		{catalogpage.Filter{Category: "page-even", Niche: "page-niche", Brands: []string{"Beta"}, Search: "bLuE"}, "page-prod-240"},
		{catalogpage.Filter{Attributes: map[int][]int{10: {1, 2}, 11: {3}}}, "page-prod-102"},
		{catalogpage.Filter{IsOriginal: &falseValue}, "page-prod-237"},
	} {
		page, err := r.GetProductCandidatePageTx(context.Background(), tx, central, tc.filter, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Keys) != 1 || page.Keys[0].ID != tc.want || page.HasMore {
			t.Fatalf("filters were not applied before limit: %+v", page)
		}
	}
	storeA := catalogpage.Scope{ID: "page-A", OwnerType: "dropshipper", Revision: 1}
	filter := catalogpage.Filter{StorePosition: true}
	first, err := r.GetProductCandidatePageTx(context.Background(), tx, storeA, filter, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Keys) != 1 || first.Keys[0].ID != "page-prod-249" || !first.HasMore {
		t.Fatalf("assortment/inactive/suspension mismatch: %+v", first)
	}
	key := first.Keys[0]
	second, err := r.GetProductCandidatePageTx(context.Background(), tx, storeA, filter, 1, &key)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Keys) != 1 || second.Keys[0].ID != "page-prod-240" || second.HasMore {
		t.Fatalf("position cursor mismatch: %+v", second)
	}
	// A cursor scope cannot turn an existing dropshipper into the central catalog.
	storeA.OwnerType = "central"
	wrong, err := r.GetProductCandidatePageTx(context.Background(), tx, storeA, catalogpage.Filter{}, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrong.Keys) != 0 {
		t.Fatal("mismatched canonical owner type widened the catalog")
	}
	storeB := catalogpage.Scope{ID: "page-B", OwnerType: "dropshipper", Revision: 1}
	other, err := r.GetProductCandidatePageTx(context.Background(), tx, storeB, filter, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Keys) != 1 || other.Keys[0].ID != "page-prod-001" || other.HasMore {
		t.Fatalf("store assortment leaked: %+v", other)
	}
	stock, err := r.GetProductCandidatePageTx(context.Background(), tx, central, catalogpage.Filter{Search: "Product 238", InStockOnly: true}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stock.Keys) != 0 {
		t.Fatal("stock filter applied after page selection or ignored")
	}
}

func TestCatalogCandidateRejectsUnsupportedPriceWorkRatherThanMisorder(t *testing.T) {
	r, tx := catalogCandidateFixture(t)
	scope := catalogpage.Scope{ID: "page-central", OwnerType: "central", Revision: 1}
	minimum := int64(100)
	for _, filter := range []catalogpage.Filter{{Sort: "cheapest"}, {Sort: "expensive"}, {Sort: "discount"}, {MinPrice: &minimum}} {
		if _, err := r.GetProductCandidatePageTx(context.Background(), tx, scope, filter, 24, nil); err != ErrCatalogPriceIndexRequired {
			t.Fatalf("price projection gate bypassed: %v", err)
		}
	}
	for _, size := range []int{0, -1, 101} {
		if _, err := r.GetProductCandidatePageTx(context.Background(), tx, scope, catalogpage.Filter{}, size, nil); err != catalogpage.ErrInvalid {
			t.Fatalf("unbounded candidate size accepted: %v", err)
		}
	}
	page, err := r.GetProductCandidatePageTx(context.Background(), tx, scope, catalogpage.Filter{Search: "' OR TRUE --"}, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Keys) != 0 {
		t.Fatal("search text escaped its SQL parameter")
	}
}
