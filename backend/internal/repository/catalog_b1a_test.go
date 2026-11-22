package repository

// B1(a) tests: the scoped is_active gate (D42 reversal) on the priced
// list path, the price bounds, the probe total, the category slugs and
// the legacy GetProducts mirror — plus the owner-gated IncludeInactive
// carve-out. Fixtures are perf01FixtureSetup (store A assortment:
// 039 inactive, 005/001/002 active, 040 suspended-supplier) and
// team04Seed (prod-inactive-1).

import (
	"context"
	"fmt"
	"testing"

	"github.com/drup/platform/internal/catalogpage"
)

// b1aAllKeys pages a priced scope completely (page size 2, cursor
// round-trip) and returns the ids in page order plus the total the
// first page reported.
func b1aAllKeys(t *testing.T, fx *perf01Fixture, scope catalogpage.Scope, filter catalogpage.Filter) ([]string, int) {
	t.Helper()
	ctx := context.Background()
	var ids []string
	var total int
	var after *catalogpage.Key
	for iter := 0; ; iter++ {
		if iter > 20 {
			t.Fatal("paging did not terminate")
		}
		page, err := fx.repo.GetPricedProductPageTx(ctx, fx.tx, scope, filter, 2, after)
		if err != nil {
			t.Fatal(err)
		}
		if iter == 0 {
			total = page.Total
		} else if page.Total != total {
			t.Fatalf("total moved between pages: %d -> %d", total, page.Total)
		}
		for _, k := range page.Keys {
			ids = append(ids, k.ID)
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
	return ids, total
}

func b1aIDSet(ids []string) map[string]bool {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func TestCatalogPricedPagesExcludeInactiveAssortment(t *testing.T) {
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

	// Priced sort (cheapest): the inactive assortment row 039 and the
	// suspended-supplier row 040 are both out of Keys and the total.
	ids, total := b1aAllKeys(t, fx, storeA, catalogpage.Filter{Sort: "cheapest"})
	if total != 3 || len(ids) != 3 {
		t.Fatalf("store A cheapest: total=%d ids=%v, want total 3, 3 ids", total, ids)
	}
	if got := b1aIDSet(ids); !got[perf01ProdID(1)] || !got[perf01ProdID(2)] || !got[perf01ProdID(5)] {
		t.Fatalf("store A cheapest ids=%v, want {001,002,005}", ids)
	}
	if b1aIDSet(ids)[perf01ProdID(39)] || b1aIDSet(ids)[perf01ProdID(40)] {
		t.Fatalf("store A cheapest leaked inactive 039 or suspended 040: %v", ids)
	}

	// Featured / store-position sort: same exclusion, assortment order
	// of the survivors (positions 1, 2, 3 -> 005, 001, 002).
	ids, total = b1aAllKeys(t, fx, storeA, catalogpage.Filter{Sort: "featured", StorePosition: true})
	if total != 3 || fmt.Sprint(ids) != fmt.Sprint([]string{perf01ProdID(5), perf01ProdID(1), perf01ProdID(2)}) {
		t.Fatalf("store A store-position: total=%d ids=%v, want 3 [005 001 002]", total, ids)
	}
	// Newest (non-priced, non-position) sort: same exclusion, id-desc
	// order on the tied created_at (005, 002, 001).
	ids, total = b1aAllKeys(t, fx, storeA, catalogpage.Filter{Sort: "newest"})
	if total != 3 || fmt.Sprint(ids) != fmt.Sprint([]string{perf01ProdID(5), perf01ProdID(2), perf01ProdID(1)}) {
		t.Fatalf("store A newest: total=%d ids=%v, want 3 [005 002 001]", total, ids)
	}

	// Probe total agrees with the list (3). The stale SET is not
	// asserted here: index rows go time-stale once their sale /
	// campaign window passes the real clock (see
	// TestCatalogPriceIndexStalenessAndRestart, which fails the same
	// way on the unmodified fixture today), but the inactive and
	// suspended rows must never appear in it.
	if total, stale, err := fx.repo.ProbeCatalogPricesTx(ctx, fx.tx, storeA, catalogpage.Filter{Sort: "cheapest"}); err != nil || total != 3 || b1aIDSet(stale)[perf01ProdID(39)] || b1aIDSet(stale)[perf01ProdID(40)] {
		t.Fatalf("store A probe: total=%d stale=%v err=%v, want total 3 without 039/040", total, stale, err)
	}

	// Price bounds agree with the list: min/max/count over {001,002,005}.
	bounds, err := fx.repo.GetCatalogPriceBoundsTx(ctx, fx.tx, storeA, catalogpage.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var wantMin, wantMax int64 = -1, -1
	for _, i := range []int{1, 2, 5} {
		p := fx.oracleMainPrice(t, fx.storeA, i).Price
		if wantMin < 0 || p < wantMin {
			wantMin = p
		}
		if p > wantMax {
			wantMax = p
		}
	}
	if bounds.MinPrice != wantMin || bounds.MaxPrice != wantMax || bounds.Count != 3 {
		t.Fatalf("store A bounds %+v, want min=%d max=%d count=3", bounds, wantMin, wantMax)
	}

	// Central scope is unchanged: 38, no 039, no 040.
	ids, total = b1aAllKeys(t, fx, central, catalogpage.Filter{Sort: "cheapest"})
	if total != 38 || len(ids) != 38 || b1aIDSet(ids)[perf01ProdID(39)] || b1aIDSet(ids)[perf01ProdID(40)] {
		t.Fatalf("central cheapest: total=%d len=%d, want 38 without 039/040", total, len(ids))
	}

	// Carve-out: with IncludeInactive the inactive assortment row
	// returns on every path, and the cursor still round-trips.
	ids, total = b1aAllKeys(t, fx, storeA, catalogpage.Filter{Sort: "cheapest", IncludeInactive: true})
	if total != 4 || len(ids) != 4 || !b1aIDSet(ids)[perf01ProdID(39)] {
		t.Fatalf("store A cheapest +IncludeInactive: total=%d ids=%v, want 4 incl. 039", total, ids)
	}
	ids, total = b1aAllKeys(t, fx, storeA, catalogpage.Filter{Sort: "featured", StorePosition: true, IncludeInactive: true})
	if total != 4 || fmt.Sprint(ids) != fmt.Sprint([]string{perf01ProdID(39), perf01ProdID(5), perf01ProdID(1), perf01ProdID(2)}) {
		t.Fatalf("store A store-position +IncludeInactive: total=%d ids=%v, want 4 [039 005 001 002]", total, ids)
	}
	if total, stale, err := fx.repo.ProbeCatalogPricesTx(ctx, fx.tx, storeA, catalogpage.Filter{Sort: "cheapest", IncludeInactive: true}); err != nil || total != 4 || b1aIDSet(stale)[perf01ProdID(40)] {
		t.Fatalf("store A probe +IncludeInactive: total=%d stale=%v err=%v, want total 4 without 040", total, stale, err)
	}
	bounds, err = fx.repo.GetCatalogPriceBoundsTx(ctx, fx.tx, storeA, catalogpage.Filter{IncludeInactive: true})
	if err != nil {
		t.Fatal(err)
	}
	wantMin, wantMax = -1, -1
	for _, i := range []int{1, 2, 5, 39} {
		p := fx.oracleMainPrice(t, fx.storeA, i).Price
		if wantMin < 0 || p < wantMin {
			wantMin = p
		}
		if p > wantMax {
			wantMax = p
		}
	}
	if bounds.MinPrice != wantMin || bounds.MaxPrice != wantMax || bounds.Count != 4 {
		t.Fatalf("store A bounds +IncludeInactive %+v, want min=%d max=%d count=4", bounds, wantMin, wantMax)
	}
	// Suspension regression: even the carve-out never resurrects a
	// suspended supplier's product (040 in every set above).
	for _, set := range [][]string{ids} {
		if b1aIDSet(set)[perf01ProdID(40)] {
			t.Fatalf("carve-out leaked suspended 040: %v", set)
		}
	}
}

func TestCatalogStoreCategorySlugsExcludeInactiveOnlyCategory(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	// An inactive product in a category nothing else carries,
	// assorted to store A: its category must not appear on the rail.
	if _, err := fx.tx.ExecContext(ctx, `INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_active,is_featured,is_original,brand,description,stock,created_at)
		VALUES('perf01-prod-041','Dead','perf01-prod-041','cat-dead','perf01-active',100000,50000,0,FALSE,FALSE,TRUE,'Beta','Dead',5,'2026-01-01T00:00:00Z'::timestamptz)`); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.tx.ExecContext(ctx, `INSERT INTO store_products(store_id,product_id,position) VALUES('perf01-A','perf01-prod-041',5)`); err != nil {
		t.Fatal(err)
	}
	slugs, err := fx.repo.ListStoreCategorySlugsTx(ctx, fx.tx, "perf01-A", "dropshipper")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(slugs) != "[cat-even cat-odd]" {
		t.Fatalf("store A slugs %v, want [cat-even cat-odd] (no cat-dead from inactive 041)", slugs)
	}
	slugs, err = fx.repo.ListStoreCategorySlugsTx(ctx, fx.tx, "perf01-central", "central")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(slugs) != "[cat-even cat-odd]" {
		t.Fatalf("central slugs %v, want [cat-even cat-odd]", slugs)
	}
}

func TestCatalogLegacyGetProductsExcludesInactiveAssortment(t *testing.T) {
	db := team04TestDB(t)
	team04Seed(t, db)
	repo := NewCatalogRepository(db)
	ctx := context.Background()
	// team04Seed puts prod-active-1 in store-a and leaves the
	// inactive prod-inactive-1 out of every assortment. Add the
	// inactive product to store-a's assortment: the legacy
	// StoreScoped path must still hide it (B1(a) mirror).
	if _, err := db.Exec(`INSERT INTO store_products VALUES ('store-a','prod-inactive-1',1)`); err != nil {
		t.Fatal(err)
	}
	scoped, err := repo.GetProducts(ctx, ProductFilter{StoreScoped: true, StoreProductIDs: []string{"prod-active-1", "prod-inactive-1"}})
	if err != nil {
		t.Fatal(err)
	}
	ids := idsOf(scoped)
	if len(ids) != 1 || ids[0] != "prod-active-1" {
		t.Fatalf("StoreScoped GetProducts = %v, want only [prod-active-1]", ids)
	}
	unscoped, err := repo.GetProducts(ctx, ProductFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range idsOf(unscoped) {
		if id == "prod-inactive-1" {
			t.Fatalf("unscoped GetProducts leaked inactive prod-inactive-1: %v", idsOf(unscoped))
		}
	}
}
