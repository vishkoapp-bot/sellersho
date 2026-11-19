package repository

// PERF-01 index tests (disposable DB only, via the isolated runner).
// The fixture mirrors catalog_page_test.go: a private schema with LIKE
// copies of every table the index reads, so probe/refresh/selection run
// against fixture data without touching public rows. The differential
// oracle is catalogpricing.ResolveMainPrice fed with hand-built inputs
// from the fixture's known values; the refresh loads the same inputs
// through tx SQL, so any loader/fingerprint drift shows up as a price
// mismatch.
import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/catalogpage"
	"github.com/drup/platform/internal/catalogpricing"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type perf01Fixture struct {
	repo      *CatalogRepository
	tx        *sql.Tx
	central   domain.Store
	storeA    domain.Store
	settings  *pricing.PlatformPricingSettings
	locks     []pricing.MarginLock
	overrides map[string]pricing.StoreVariantOverride
	campaign  domain.SellerDiscountCampaign
	widget    *pricing.DealsWidgetConfig
	asOf      time.Time
}

func perf01FixtureSetup(t *testing.T) *perf01Fixture {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	schema := pq.QuoteIdentifier("perf01_" + uuid.NewString()[:8])
	if _, err := tx.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	tables := []string{"products", "suppliers", "stores", "store_products", "niches",
		"product_niches", "product_attribute_values", "product_variants",
		"platform_settings", "platform_pct_products", "platform_pct_categories",
		"product_margin_ceilings", "store_product_overrides", "store_variant_overrides",
		"store_margin_locks", "seller_discount_campaigns", "store_variant_defaults",
		"store_variant_visibility", "catalog_public_prices", "catalog_scope_revisions"}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE `+schema+`.`+table+` (LIKE public.`+table+` INCLUDING ALL)`); err != nil {
			t.Fatal(err)
		}
	}
	asOf := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	widgetJSON, _ := json.Marshal(map[string]any{
		"title": "deals", "productIds": []string{"perf01-prod-001"},
		"discounts": map[string]float64{"perf01-prod-001": 5},
		"endsAt":    "2030-01-01T00:00:00Z",
	})
	seed := []string{
		`SET LOCAL search_path = ` + schema,
		`INSERT INTO stores(id,name,slug,owner_type,owner_name,seller_deals_widget_config) VALUES
			('perf01-central','Central','perf01-central','central','Fixture',NULL),
			('perf01-A','A','perf01-A','dropshipper','Fixture','` + string(widgetJSON) + `'::jsonb),
			('perf01-B','B','perf01-B','dropshipper','Fixture',NULL)`,
		`INSERT INTO suppliers(id,name,approval_status) VALUES('perf01-active','Active','approved'),('perf01-suspended','Suspended','suspended')`,
		`INSERT INTO platform_settings(id,platform_pct,central_profit_pct,increase_cap_pct,increase_locked,max_profit_margin,min_profit_margin,default_margin,central_store_markup_percent)
			VALUES (1,10,10,10,TRUE,35,3,20,15)`,
		// 40 products: cost 10000+i*1000, original 100000, even cat-even /
		// odd cat-odd, even 'Blue Shoe' / odd 'Red Bag', featured every 3rd.
		// 038 stock 0, 039 inactive, 040 suspended supplier.
		`INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_active,is_featured,is_original,brand,description,stock,created_at)
			SELECT 'perf01-prod-'||lpad(i::text,3,'0'),
			CASE WHEN i%2=0 THEN 'Blue Shoe '||i ELSE 'Red Bag '||i END,
			'perf01-prod-'||i,
			CASE WHEN i%2=0 THEN 'cat-even' ELSE 'cat-odd' END,
			CASE WHEN i=40 THEN 'perf01-suspended' ELSE 'perf01-active' END,
			100000, 10000+i*1000, 0, i<>39, i%3=0, TRUE,
			CASE WHEN i%2=0 THEN 'Beta' ELSE 'Alpha' END,
			CASE WHEN i%2=0 THEN 'Blue Shoe' ELSE 'Red Bag' END,
			CASE WHEN i=38 THEN 0 ELSE 5 END,
			'2026-01-01T00:00:00Z'::timestamptz
			FROM generate_series(1,40) i`,
		`INSERT INTO store_products(store_id,product_id,position) VALUES
			('perf01-A','perf01-prod-039',-1),('perf01-A','perf01-prod-005',1),
			('perf01-A','perf01-prod-001',2),('perf01-A','perf01-prod-002',3),
			('perf01-A','perf01-prod-040',4)`,
		`INSERT INTO niches(id,title,slug,category_slug) VALUES('perf01-niche','Fixture','perf01-niche','cat-odd')`,
		`INSERT INTO product_niches(product_id,niche_id) VALUES('perf01-prod-001','perf01-niche')`,
		`INSERT INTO product_attribute_values(product_id,dk_attribute_id,dk_value_id) VALUES('perf01-prod-010',10,1)`,
		`INSERT INTO product_variants(id,product_id,spnc,variant_name,attributes,is_default,stock,supplier_base_cost,inventory_mode,is_active) VALUES
			('11111111-1111-1111-1111-111111111001','perf01-prod-001','P001AAAA','Default','{}',TRUE,0,60000,'tracked',TRUE),
			('11111111-1111-1111-1111-111111111002','perf01-prod-001','P001AAAB','Second','{}',FALSE,0,12000,'tracked',TRUE),
			('11111111-1111-1111-1111-111111111003','perf01-prod-002','P002AAAA','Default','{}',TRUE,0,13000,'tracked',TRUE),
			('11111111-1111-1111-1111-111111111004','perf01-prod-002','P002AAAB','Second','{}',FALSE,0,14000,'tracked',TRUE),
			('11111111-1111-1111-1111-111111111005','perf01-prod-003','P003AAAA','Sale','{}',TRUE,0,20000,'tracked',TRUE),
			('11111111-1111-1111-1111-111111111006','perf01-prod-004','P004AAAA','Off','{}',TRUE,0,20000,'tracked',FALSE),
			('11111111-1111-1111-1111-111111111007','perf01-prod-004','P004AAAB','On','{}',FALSE,0,15000,'tracked',TRUE)`,
		`UPDATE product_variants SET sale_price=23000, sale_from='2026-11-18T00:00:00Z'::timestamptz, sale_to='2026-11-20T00:00:00Z'::timestamptz WHERE id='11111111-1111-1111-1111-111111111005'`,
		`UPDATE products SET stock=CASE WHEN id='perf01-prod-038' THEN 0 ELSE 5 END`,
		`INSERT INTO store_variant_defaults(store_id,product_id,variant_id) VALUES
			('perf01-A','perf01-prod-002','11111111-1111-1111-1111-111111111004')`,
		`INSERT INTO store_product_overrides(store_id,product_id,override_value,mode) VALUES
			('perf01-A','perf01-prod-002',20,'percent')`,
		`INSERT INTO store_margin_locks(store_id,scope,scope_id,lock_mode,lock_value) VALUES
			('perf01-A','product','perf01-prod-003','admin_set',15)`,
		`INSERT INTO seller_discount_campaigns(id,store_id,name,scope,target_id,mode,value,starts_at,ends_at,is_active) VALUES
			('perf01-camp-1','perf01-A','Ten','store','', 'percent',10,'2026-11-18T00:00:00Z'::timestamptz,'2026-11-20T00:00:00Z'::timestamptz,TRUE)`,
	}
	for _, query := range seed {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	fx := &perf01Fixture{
		repo: NewCatalogRepository(db), tx: tx, asOf: asOf,
		settings: &pricing.PlatformPricingSettings{
			PlatformPct: 10, CentralProfitPct: 10, MaxProfitMargin: 35, MinProfitMargin: 3,
			DefaultMargin: 20, ProductPlatformPcts: map[string]float64{},
			CategoryPlatformPcts: map[string]float64{}, ProductMarginCeilings: map[string]float64{},
		},
		overrides: map[string]pricing.StoreVariantOverride{},
	}
	fx.central = domain.Store{ID: "perf01-central", Slug: "perf01-central", OwnerType: "central"}
	fx.storeA = domain.Store{
		ID: "perf01-A", Slug: "perf01-A", OwnerType: "dropshipper",
		SellerDealsWidgetConfig:   widgetJSON,
		ProductPriceOverrides:     map[string]float64{"perf01-prod-002": 20},
		ProductPriceOverrideModes: map[string]string{"perf01-prod-002": "percent"},
	}
	fx.locks = []pricing.MarginLock{{Scope: "product", ScopeID: "perf01-prod-003", Mode: "admin_set", Value: ptrFloat64(15)}}
	fx.campaign = domain.SellerDiscountCampaign{
		ID: "perf01-camp-1", StoreID: "perf01-A", Name: "Ten", Scope: "store",
		Mode: "percent", Value: 10,
		StartsAt: time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), IsActive: true,
	}
	fx.widget = pricing.ParseDealsWidgetConfig(widgetJSON)
	return fx
}

func ptrFloat64(v float64) *float64 { return &v }

func perf01ProdID(i int) string { return fmt.Sprintf("perf01-prod-%03d", i) }

// perf01OracleProduct builds the fixture-truth product row for the
// differential oracle (same values as the SQL seed).
func perf01OracleProduct(i int) domain.Product {
	niches := []string{}
	if i == 1 {
		niches = []string{"perf01-niche"}
	}
	category := "cat-odd"
	if i%2 == 0 {
		category = "cat-even"
	}
	return domain.Product{
		ID: perf01ProdID(i), Title: "t", Slug: perf01ProdID(i), CategorySlug: category,
		SupplierID: "perf01-active", OriginalPrice: 100000,
		SupplierBaseCost: int64(10000 + i*1000), IsActive: i != 39,
		IsFeatured: i%3 == 0, IsOriginal: true, Stock: 5, NicheIDs: niches,
	}
}

func perf01OracleVariants(i int) []domain.ProductVariant {
	cost := func(v int64) *int64 { return &v }
	salePrice := int64(23000)
	saleFrom := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	saleTo := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	switch i {
	case 1:
		return []domain.ProductVariant{
			{ID: "11111111-1111-1111-1111-111111111001", ProductID: perf01ProdID(1), IsDefault: true, IsActive: true, InventoryMode: "tracked", SupplierBaseCost: cost(60000)},
			{ID: "11111111-1111-1111-1111-111111111002", ProductID: perf01ProdID(1), IsActive: true, InventoryMode: "tracked", SupplierBaseCost: cost(12000)},
		}
	case 2:
		return []domain.ProductVariant{
			{ID: "11111111-1111-1111-1111-111111111003", ProductID: perf01ProdID(2), IsDefault: true, IsActive: true, InventoryMode: "tracked", SupplierBaseCost: cost(13000)},
			{ID: "11111111-1111-1111-1111-111111111004", ProductID: perf01ProdID(2), IsActive: true, InventoryMode: "tracked", SupplierBaseCost: cost(14000)},
		}
	case 3:
		return []domain.ProductVariant{
			{ID: "11111111-1111-1111-1111-111111111005", ProductID: perf01ProdID(3), IsDefault: true, IsActive: true, InventoryMode: "tracked", SupplierBaseCost: cost(20000), SalePrice: &salePrice, SaleFrom: &saleFrom, SaleTo: &saleTo},
		}
	case 4:
		return []domain.ProductVariant{
			{ID: "11111111-1111-1111-1111-111111111006", ProductID: perf01ProdID(4), IsDefault: true, IsActive: false, InventoryMode: "tracked", SupplierBaseCost: cost(20000)},
			{ID: "11111111-1111-1111-1111-111111111007", ProductID: perf01ProdID(4), IsActive: true, InventoryMode: "tracked", SupplierBaseCost: cost(15000)},
		}
	}
	return nil
}

// oracleMainPrice prices one fixture product through ResolveMainPrice with
// hand-built inputs (the differential oracle for the refresh).
func (fx *perf01Fixture) oracleMainPrice(t *testing.T, store domain.Store, i int) catalogpricing.MainPrice {
	t.Helper()
	product := perf01OracleProduct(i)
	var activeCampaigns, allCampaigns []domain.SellerDiscountCampaign
	var widget *pricing.DealsWidgetConfig
	var locks []pricing.MarginLock
	storeDefault := ""
	if store.OwnerType == "dropshipper" {
		allCampaigns = []domain.SellerDiscountCampaign{fx.campaign}
		activeCampaigns = allCampaigns
		widget = fx.widget
		locks = fx.locks
		if i == 2 {
			storeDefault = "11111111-1111-1111-1111-111111111004"
		}
	}
	main, err := catalogpricing.ResolveMainPrice(catalogpricing.MainPriceInput{
		Product: product, Variants: perf01OracleVariants(i), Store: store,
		Settings: fx.settings, Locks: locks, Overrides: fx.overrides,
		ActiveCampaigns: activeCampaigns, AllCampaigns: allCampaigns,
		Widget: widget, StoreDefaultVariantID: storeDefault, AsOf: fx.asOf,
	})
	if err != nil {
		t.Fatalf("oracle pricing failed for product %d: %v", i, err)
	}
	return main
}

// refreshAll refreshes the eligible catalog for a store and returns the
// scope revision read AFTER the refresh (see the audit: the handler must
// re-read the revision post-refresh, because a first build bumps it).
// Locks are per-store: central refreshes with none (the prod-003 lock
// belongs to store A only).
func (fx *perf01Fixture) refreshAll(t *testing.T, store domain.Store, ids []string) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := fx.repo.GetCatalogScopeRevisionTx(ctx, fx.tx, store.ID); err != nil {
		t.Fatal(err)
	}
	locks := fx.locks
	campaigns := []domain.SellerDiscountCampaign{fx.campaign}
	if store.OwnerType != "dropshipper" {
		locks = nil
		campaigns = nil
	}
	inputs := &CatalogScopeInputs{
		Store: store, Settings: fx.settings, Locks: locks,
		Overrides: fx.overrides, Campaigns: campaigns,
	}
	if _, err := fx.repo.RefreshCatalogPricesTx(ctx, fx.tx, inputs, ids, fx.asOf); err != nil {
		t.Fatal(err)
	}
	revision, err := fx.repo.GetCatalogScopeRevisionTx(ctx, fx.tx, store.ID)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func TestCatalogPriceIndexRefreshMatchesEngine(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		store domain.Store
		// eligible product numbers for this scope
		eligible []int
	}{
		{"central", fx.central, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38}},
		{"storeA", fx.storeA, []int{1, 2, 5, 39}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eligibleIDs := make([]string, 0, len(tc.eligible))
			for _, i := range tc.eligible {
				eligibleIDs = append(eligibleIDs, perf01ProdID(i))
			}
			fx.refreshAll(t, tc.store, eligibleIDs)
			rows, err := fx.tx.QueryContext(ctx, `SELECT product_id, main_price, original_price, discount_pct, price_min, price_max FROM catalog_public_prices WHERE store_id = $1 ORDER BY product_id`, tc.store.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			got := map[string]catalogpricing.MainPrice{}
			for rows.Next() {
				var id string
				var m catalogpricing.MainPrice
				var priceMin, priceMax sql.NullInt64
				if err := rows.Scan(&id, &m.Price, &m.OriginalPrice, &m.DiscountPercent, &priceMin, &priceMax); err != nil {
					t.Fatal(err)
				}
				if priceMin.Valid {
					v := priceMin.Int64
					m.PriceMin = &v
				}
				if priceMax.Valid {
					v := priceMax.Int64
					m.PriceMax = &v
				}
				got[id] = m
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.eligible) {
				t.Fatalf("index holds %d rows, want %d (eligible %v)", len(got), len(tc.eligible), tc.eligible)
			}
			for _, i := range tc.eligible {
				want := fx.oracleMainPrice(t, tc.store, i)
				g := got[perf01ProdID(i)]
				if g.Price != want.Price || g.OriginalPrice != want.OriginalPrice || g.DiscountPercent != want.DiscountPercent {
					t.Fatalf("product %d: index %+v, oracle %+v", i, g, want)
				}
				if (g.PriceMin == nil) != (want.PriceMin == nil) || (g.PriceMin != nil && *g.PriceMin != *want.PriceMin) ||
					(g.PriceMax == nil) != (want.PriceMax == nil) || (g.PriceMax != nil && *g.PriceMax != *want.PriceMax) {
					t.Fatalf("product %d range: index min=%v max=%v, oracle min=%v max=%v", i, g.PriceMin, g.PriceMax, want.PriceMin, want.PriceMax)
				}
			}
			// Hand-computed spot checks (exact arithmetic, no oracle):
			// central prod-005: R=16500, final=RoundTo100(18150)=18200.
			// Central prod-001: default variant 60000 -> R=66000, final=72600.
			if tc.store.OwnerType == "central" {
				if got[perf01ProdID(5)].Price != 18200 {
					t.Fatalf("central prod-005 spot check: got %d, want 18200", got[perf01ProdID(5)].Price)
				}
				if got[perf01ProdID(1)].Price != 72600 {
					t.Fatalf("central prod-001 spot check: got %d, want 72600", got[perf01ProdID(1)].Price)
				}
			}
		})
	}
}

func TestCatalogPricedPagesEnumerateExactlyOnce(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	central := catalogpage.Scope{ID: "perf01-central", OwnerType: "central", Revision: 1}
	storeA := catalogpage.Scope{ID: "perf01-A", OwnerType: "dropshipper", Revision: 1}
	centralIDs := []string{}
	for i := 1; i <= 38; i++ {
		centralIDs = append(centralIDs, perf01ProdID(i))
	}
	fx.refreshAll(t, fx.central, centralIDs)
	fx.refreshAll(t, fx.storeA, []string{perf01ProdID(1), perf01ProdID(2), perf01ProdID(5), perf01ProdID(39)})
	// Central: 38 eligible (039 inactive, 040 suspended).
	enumerate := func(t *testing.T, scope catalogpage.Scope, filter catalogpage.Filter, want int) []catalogpage.Key {
		t.Helper()
		seen := map[string]bool{}
		var keys []catalogpage.Key
		var after *catalogpage.Key
		for iter := 0; ; iter++ {
			if iter > 20 {
				t.Fatal("paging did not terminate")
			}
			page, err := fx.repo.GetPricedProductPageTx(ctx, fx.tx, scope, filter, 7, after)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != want {
				t.Fatalf("sort %s: total %d, want %d", filter.Sort, page.Total, want)
			}
			for _, k := range page.Keys {
				if seen[k.ID] {
					t.Fatalf("sort %s: duplicate %s", filter.Sort, k.ID)
				}
				seen[k.ID] = true
				// Keys already carry ONLY the active sort's key field
				// (Price for cheapest/expensive, Discount for discount,
				// Featured/CreatedAt for featured/newest, Position for
				// store position); carrying another field fails
				// validation by design.
				keys = append(keys, k)
			}
			if !page.HasMore {
				break
			}
			last := page.Keys[len(page.Keys)-1]
			token, err := catalogpage.Encode(scope, filter, last)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := catalogpage.Decode(token, scope, filter)
			if err != nil {
				t.Fatal(err)
			}
			after = &decoded
		}
		if len(keys) != want {
			t.Fatalf("sort %s: enumerated %d, want %d", filter.Sort, len(keys), want)
		}
		return keys
	}
	cheapest := enumerate(t, central, catalogpage.Filter{Sort: "cheapest"}, 38)
	for i := 1; i < len(cheapest); i++ {
		if cheapest[i].Price < cheapest[i-1].Price {
			t.Fatalf("cheapest not ascending at %d: %d < %d", i, cheapest[i].Price, cheapest[i-1].Price)
		}
	}
	expensive := enumerate(t, central, catalogpage.Filter{Sort: "expensive"}, 38)
	for i := 1; i < len(expensive); i++ {
		if expensive[i].Price > expensive[i-1].Price {
			t.Fatalf("expensive not descending at %d", i)
		}
	}
	discount := enumerate(t, central, catalogpage.Filter{Sort: "discount"}, 38)
	for i := 1; i < len(discount); i++ {
		if discount[i].Discount > discount[i-1].Discount {
			t.Fatalf("discount not descending at %d", i)
		}
	}
	// Store A: 4 eligible (039 inactive-but-assorted, 005, 001, 002;
	// 040 suspended out).
	enumerate(t, storeA, catalogpage.Filter{Sort: "cheapest"}, 4)
	// Non-price sorts through the same unified selection.
	featured := enumerate(t, central, catalogpage.Filter{Sort: "featured"}, 38)
	if len(featured) == 0 || !featured[0].Featured {
		t.Fatalf("featured sort must lead with featured products, got %+v", featured[0])
	}
	newest := enumerate(t, central, catalogpage.Filter{Sort: "newest"}, 38)
	if newest[0].ID != perf01ProdID(38) {
		t.Fatalf("newest sort must lead with the highest id on tied created_at, got %s", newest[0].ID)
	}
	// Filtered pages: search, category, brand, stock, attribute.
	enumerate(t, central, catalogpage.Filter{Sort: "cheapest", Search: "Blue Shoe"}, 19)
	enumerate(t, central, catalogpage.Filter{Sort: "cheapest", Category: "cat-odd"}, 19)
	enumerate(t, central, catalogpage.Filter{Sort: "cheapest", Brands: []string{"Alpha"}}, 19)
	enumerate(t, central, catalogpage.Filter{Sort: "cheapest", InStockOnly: true}, 37)
	enumerate(t, central, catalogpage.Filter{Sort: "cheapest", Attributes: map[int][]int{10: {1}}}, 1)
	// Price bounds on the page filter.
	enumerate(t, central, catalogpage.Filter{Sort: "cheapest", MinPrice: ptrInt64(20000)}, 34)
	// Non-price total agrees with enumeration (candidate path).
	total, err := fx.repo.CountCatalogCandidatesTx(ctx, fx.tx, central, catalogpage.Filter{Sort: "featured"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 38 {
		t.Fatalf("candidate count %d, want 38", total)
	}
	// Store-position order for store A: -1, 1, 2, 3 -> 039, 005, 001, 002.
	seen := map[string]bool{}
	var after *catalogpage.Key
	var order []string
	for iter := 0; ; iter++ {
		if iter > 10 {
			t.Fatal("store-position paging did not terminate")
		}
		page, err := fx.repo.GetPricedProductPageTx(ctx, fx.tx, storeA, catalogpage.Filter{Sort: "featured", StorePosition: true}, 2, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range page.Keys {
			if seen[k.ID] {
				t.Fatalf("duplicate %s", k.ID)
			}
			seen[k.ID] = true
			order = append(order, k.ID)
		}
		if !page.HasMore {
			break
		}
		last := page.Keys[len(page.Keys)-1]
		after = &last
	}
	want := []string{perf01ProdID(39), perf01ProdID(5), perf01ProdID(1), perf01ProdID(2)}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("store-position order %v, want %v", order, want)
	}
}

func ptrInt64(v int64) *int64 { return &v }

func TestCatalogPriceIndexStalenessAndRestart(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	scope := catalogpage.Scope{ID: "perf01-central", OwnerType: "central", Revision: 1}
	centralIDs := []string{}
	for i := 1; i <= 38; i++ {
		centralIDs = append(centralIDs, perf01ProdID(i))
	}
	rev1 := fx.refreshAll(t, fx.central, centralIDs)
	if rev1 < 2 {
		t.Fatalf("first build must bump the revision (rows added), got %d", rev1)
	}
	// A title edit is NOT a price input: the probe stays fresh and the
	// revision does not move, so paging continues.
	if _, err := fx.tx.ExecContext(ctx, `UPDATE products SET title='Renamed' WHERE id='perf01-prod-005'`); err != nil {
		t.Fatal(err)
	}
	total, stale, err := fx.repo.ProbeCatalogPricesTx(ctx, fx.tx, scope, catalogpage.Filter{Sort: "cheapest"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 38 || len(stale) != 0 {
		t.Fatalf("title edit: total=%d stale=%v, want 38/[]", total, stale)
	}
	rev, err := fx.repo.GetCatalogScopeRevisionTx(ctx, fx.tx, scope.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev != rev1 {
		t.Fatalf("title edit moved the revision %d -> %d", rev1, rev)
	}
	// A cost edit IS a price input: the probe names the product, the
	// refresh changes its price and bumps the revision, and a cursor
	// bound to the old revision now fails binding (restart).
	if _, err := fx.tx.ExecContext(ctx, `UPDATE products SET supplier_base_cost=90000 WHERE id='perf01-prod-005'`); err != nil {
		t.Fatal(err)
	}
	_, stale, err = fx.repo.ProbeCatalogPricesTx(ctx, fx.tx, scope, catalogpage.Filter{Sort: "cheapest"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0] != perf01ProdID(5) {
		t.Fatalf("cost edit: stale=%v, want [perf01-prod-005]", stale)
	}
	changed, err := fx.repo.RefreshCatalogPricesTx(ctx, fx.tx, &CatalogScopeInputs{
		Store: fx.central, Settings: fx.settings, Overrides: fx.overrides,
	}, stale, fx.asOf)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("price-changing refresh must report changed=true")
	}
	rev2, err := fx.repo.GetCatalogScopeRevisionTx(ctx, fx.tx, scope.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev2 != rev1+1 {
		t.Fatalf("revision %d -> %d, want +1", rev1, rev2)
	}
	// Old cursor (bound to rev1) must not decode under rev2.
	page, err := fx.repo.GetPricedProductPageTx(ctx, fx.tx, catalogpage.Scope{ID: scope.ID, OwnerType: scope.OwnerType, Revision: rev1}, catalogpage.Filter{Sort: "cheapest"}, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	last := page.Keys[len(page.Keys)-1]
	oldScope := catalogpage.Scope{ID: scope.ID, OwnerType: scope.OwnerType, Revision: rev1}
	token, err := catalogpage.Encode(oldScope, catalogpage.Filter{Sort: "cheapest"}, last)
	if err != nil {
		t.Fatal(err)
	}
	newScope := catalogpage.Scope{ID: scope.ID, OwnerType: scope.OwnerType, Revision: rev2}
	if _, err := catalogpage.Decode(token, newScope, catalogpage.Filter{Sort: "cheapest"}); err != catalogpage.ErrRestart {
		t.Fatalf("old cursor under new revision: err=%v, want ErrRestart", err)
	}
	// And the refreshed price is the new one: prod-005 cost 90000 ->
	// R=99000, final=108900, now the most expensive central product.
	total, stale, err = fx.repo.ProbeCatalogPricesTx(ctx, fx.tx, newScope, catalogpage.Filter{Sort: "cheapest"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 38 || len(stale) != 0 {
		t.Fatalf("after refresh: total=%d stale=%v", total, stale)
	}
	expensive, err := fx.repo.GetPricedProductPageTx(ctx, fx.tx, newScope, catalogpage.Filter{Sort: "expensive"}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if expensive.Keys[0].ID != perf01ProdID(5) || expensive.Keys[0].Price != 108900 {
		t.Fatalf("most expensive after edit: %+v, want prod-005 @108900", expensive.Keys[0])
	}
}

func TestCatalogPriceBoundsMatchEngine(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	centralIDs := []string{}
	for i := 1; i <= 38; i++ {
		centralIDs = append(centralIDs, perf01ProdID(i))
	}
	fx.refreshAll(t, fx.central, centralIDs)
	scope := catalogpage.Scope{ID: "perf01-central", OwnerType: "central", Revision: 1}
	bounds, err := fx.repo.GetCatalogPriceBoundsTx(ctx, fx.tx, scope, catalogpage.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var wantMin, wantMax int64 = -1, -1
	for i := 1; i <= 38; i++ {
		p := fx.oracleMainPrice(t, fx.central, i).Price
		if wantMin < 0 || p < wantMin {
			wantMin = p
		}
		if p > wantMax {
			wantMax = p
		}
	}
	if bounds.MinPrice != wantMin || bounds.MaxPrice != wantMax || bounds.Count != 38 {
		t.Fatalf("bounds %+v, want min=%d max=%d count=38", bounds, wantMin, wantMax)
	}
	// Category-scoped bounds: cat-odd central products only (19).
	bounds, err = fx.repo.GetCatalogPriceBoundsTx(ctx, fx.tx, scope, catalogpage.Filter{Category: "cat-odd"})
	if err != nil {
		t.Fatal(err)
	}
	wantMin, wantMax = -1, -1
	for i := 1; i <= 38; i += 2 {
		p := fx.oracleMainPrice(t, fx.central, i).Price
		if wantMin < 0 || p < wantMin {
			wantMin = p
		}
		if p > wantMax {
			wantMax = p
		}
	}
	if bounds.MinPrice != wantMin || bounds.MaxPrice != wantMax || bounds.Count != 19 {
		t.Fatalf("cat-odd bounds %+v, want min=%d max=%d count=19", bounds, wantMin, wantMax)
	}
}

func TestCatalogPriceSearchIndexExists(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	// NOTE: CREATE TABLE ... (LIKE ... INCLUDING ALL) copies index
	// definitions but renames them to column-derived default names, so
	// assert by definition, not by the public index names.
	rows, err := fx.tx.QueryContext(ctx, `SELECT pg_get_indexdef(indexrelid)
		FROM pg_index WHERE indrelid = (SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = 'products' AND n.nspname = current_schema())`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	trigram := 0
	for rows.Next() {
		var def string
		if err := rows.Scan(&def); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(def, "gin_trgm_ops") {
			trigram++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if trigram != 2 {
		t.Fatalf("trigram GIN indexes on fixture products: %d, want 2 (title + description)", trigram)
	}
	var priceIndexCount int
	if err := fx.tx.QueryRowContext(ctx, `SELECT count(*) FROM pg_index
		WHERE indrelid = (SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relname = 'catalog_public_prices' AND n.nspname = current_schema())`).Scan(&priceIndexCount); err != nil {
		t.Fatal(err)
	}
	// PK + main + discount + range + valid_until = 5.
	if priceIndexCount != 5 {
		t.Fatalf("indexes on fixture catalog_public_prices: %d, want 5", priceIndexCount)
	}
}

func TestCatalogStoreCategorySlugs(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	slugs, err := fx.repo.ListStoreCategorySlugsTx(ctx, fx.tx, "perf01-A", "dropshipper")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(slugs) != "[cat-even cat-odd]" {
		t.Fatalf("store A slugs %v, want [cat-even cat-odd]", slugs)
	}
	slugs, err = fx.repo.ListStoreCategorySlugsTx(ctx, fx.tx, "perf01-central", "central")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(slugs) != "[cat-even cat-odd]" {
		t.Fatalf("central slugs %v, want [cat-even cat-odd]", slugs)
	}
}
