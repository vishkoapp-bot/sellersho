package pricing

import (
	"testing"

	"github.com/drup/platform/internal/domain"
)

// Proven live examples (MASTER-AUDIT section 3(a)). Global platform_pct
// is 0 in live, so platformFeeF = 0 and baseCostB = C in every case here.
// Dropshipper expectations were updated for the owner rule of 2026-11-05:
// there is no store-wide default profit anymore — a store's legacy
// ProfitMarginPercent is inert, and unset percents resolve at the MAX
// ALLOWED margin (per-product ceiling row, else MaxProfitMargin = 35).

func liveSettings() *PlatformPricingSettings {
	return &PlatformPricingSettings{
		PlatformPct:      0,
		CentralProfitPct: 10,
		DefaultMargin:    20,
		MaxProfitMargin:   35,  // live row max_profit_margin; capSellerMargin needs a real ceiling
	}
}

func TestReferenceEarbudsPro(t *testing.T) {
	prod := domain.Product{ID: "prod-earbuds-pro", SupplierBaseCost: 1200000, OriginalPrice: 1850000}
	central := domain.Store{ID: "store-central", OwnerType: "central"}
	dropshipper := domain.Store{ID: "store-ds", OwnerType: "dropshipper", ProfitMarginPercent: 15}

	c := ResolveReferencePricing(prod, central, liveSettings(), nil)
	if c.FinalPrice != 1320000 {
		t.Errorf("central final = %d; want 1320000", c.FinalPrice)
	}
	if c.PlatformFee != 0 {
		t.Errorf("central platformFee = %d; want 0", c.PlatformFee)
	}
	if DiscountPercent(prod.OriginalPrice, c.FinalPrice) != 29 {
		t.Errorf("central discountPercent = %d; want 29", DiscountPercent(prod.OriginalPrice, c.FinalPrice))
	}

	// Owner rule (2026-11-05): the store's ProfitMarginPercent (15) is a
	// legacy store default and no longer influences pricing; with no
	// product override the margin is the max allowed (35).
	d := ResolveReferencePricing(prod, dropshipper, liveSettings(), nil)
	if d.FinalPrice != 1620000 {
		t.Errorf("dropshipper final = %d; want 1620000 (1200000 x 1.35, max-allowed fallback)", d.FinalPrice)
	}
	if d.MarginPercent != 35 || d.ActiveTier != "max-default" {
		t.Errorf("dropshipper margin=%v tier=%q; want 35/max-default", d.MarginPercent, d.ActiveTier)
	}
	if d.BaseCost != 1200000 {
		t.Errorf("dropshipper baseCostB = %d; want 1200000 (= C, not C+F)", d.BaseCost)
	}
	if d.PlatformFee != 0 {
		t.Errorf("dropshipper platformFee = %d; want 0", d.PlatformFee)
	}
	if DiscountPercent(prod.OriginalPrice, d.FinalPrice) != 12 {
		t.Errorf("dropshipper discountPercent = %d; want 12", DiscountPercent(prod.OriginalPrice, d.FinalPrice))
	}
}

func TestReferenceVitaminSerumProductOverride7Pct(t *testing.T) {
	prod := domain.Product{ID: "prod-vitamin-serum", SupplierBaseCost: 420000, OriginalPrice: 650000}
	store := domain.Store{
		ID: "store-ds", OwnerType: "dropshipper", ProfitMarginPercent: 15,
		ProductPriceOverrides:     map[string]float64{"prod-vitamin-serum": 7},
		ProductPriceOverrideModes: map[string]string{"prod-vitamin-serum": "percent"},
	}
	// Central price is still x1.10.
	c := ResolveReferencePricing(prod, domain.Store{OwnerType: "central"}, liveSettings(), nil)
	if c.FinalPrice != 462000 {
		t.Errorf("central final = %d; want 462000", c.FinalPrice)
	}
	// Product percent override applies on C, not on C+F.
	d := ResolveReferencePricing(prod, store, liveSettings(), nil)
	if d.FinalPrice != 449400 {
		t.Errorf("override final = %d; want 449400 (420000 x 1.07)", d.FinalPrice)
	}
	if d.ActiveTier != "product" {
		t.Errorf("activeTier = %q; want product", d.ActiveTier)
	}
}

func TestReferenceDemoGold(t *testing.T) {
	prod := domain.Product{ID: "prod-1793551155965", SupplierBaseCost: 58000000, OriginalPrice: 72500000}
	c := ResolveReferencePricing(prod, domain.Store{OwnerType: "central"}, liveSettings(), nil)
	if c.FinalPrice != 63800000 {
		t.Errorf("central final = %d; want 63800000", c.FinalPrice)
	}
	if DiscountPercent(prod.OriginalPrice, c.FinalPrice) != 12 {
		t.Errorf("central discountPercent = %d; want 12 (the x1.10 phenomenon, not a separate 12%% rule, U27)", DiscountPercent(prod.OriginalPrice, c.FinalPrice))
	}
	// Owner rule (2026-11-05): no store default — the max-allowed margin
	// (35) applies, and the final price rises above OriginalPrice, so the
	// derived discount is 0.
	d := ResolveReferencePricing(prod, domain.Store{OwnerType: "dropshipper", ProfitMarginPercent: 15}, liveSettings(), nil)
	if d.FinalPrice != 78300000 {
		t.Errorf("dropshipper final = %d; want 78300000 (58000000 x 1.35, max-allowed fallback)", d.FinalPrice)
	}
	if DiscountPercent(prod.OriginalPrice, d.FinalPrice) != 0 {
		t.Errorf("dropshipper discountPercent = %d; want 0 (final above original)", DiscountPercent(prod.OriginalPrice, d.FinalPrice))
	}
}

func TestReferencePlatformPctLadder(t *testing.T) {
	prod := domain.Product{ID: "prod-x", CategorySlug: "cat-y", SupplierBaseCost: 1000000}
	s := liveSettings()
	s.PlatformPct = 30
	// Global 30%: reference cost 1,300,000, central 1,430,000 — the
	// October 30% regime shape (history only; today global is 0).
	c := ResolveReferencePricing(prod, domain.Store{OwnerType: "central"}, s, nil)
	if c.ReferenceCost != 1300000 || c.PlatformFee != 300000 || c.FinalPrice != 1430000 {
		t.Errorf("global 30%%: reference=%d fee=%d final=%d; want 1300000/300000/1430000", c.ReferenceCost, c.PlatformFee, c.FinalPrice)
	}
	// Product ladder row beats global.
	s.ProductPlatformPcts = map[string]float64{"prod-x": 10}
	pct, src := ResolvePlatformPct(prod.ID, prod.CategorySlug, s)
	if pct != 10 || src != "product" {
		t.Errorf("ladder product = %v/%q; want 10/product", pct, src)
	}
	// Category ladder row beats global when no product row exists.
	s.ProductPlatformPcts = map[string]float64{}
	s.CategoryPlatformPcts = map[string]float64{"cat-y": 5}
	pct, src = ResolvePlatformPct(prod.ID, prod.CategorySlug, s)
	if pct != 5 || src != "category" {
		t.Errorf("ladder category = %v/%q; want 5/category", pct, src)
	}
}

func TestReferenceProfitSplit(t *testing.T) {
	prod := domain.Product{ID: "prod-earbuds-pro", SupplierBaseCost: 1200000}
	d := ResolveReferencePricing(prod, domain.Store{OwnerType: "dropshipper", ProfitMarginPercent: 15}, liveSettings(), nil)
	split := ReferenceProfitSplit(d, false)
	if split.PlatformFeeAmount != 0 || split.DropshipperMargin != 420000 || split.PlatformTotal != 0 {
		t.Errorf("dropshipper split = %+v; want fee 0, margin 420000 (max-allowed 35%% of C), platform total 0", split)
	}
	c := ResolveReferencePricing(prod, domain.Store{OwnerType: "central"}, liveSettings(), nil)
	csplit := ReferenceProfitSplit(c, true)
	if csplit.CentralProfit != 120000 || csplit.PlatformTotal != 120000 {
		t.Errorf("central split = %+v; want central profit 120000, platform total 120000", csplit)
	}
}

func TestReferenceVariantPricing(t *testing.T) {
	prod := domain.Product{ID: "prod-x", SupplierBaseCost: 1000000, OriginalPrice: 1500000}
	store := domain.Store{OwnerType: "dropshipper", ProfitMarginPercent: 15}
	// Variant with its own cost, no override: owner rule (2026-11-05) —
	// the max-allowed margin (35) on the variant cost; the store's legacy
	// ProfitMarginPercent (15) is inert and nothing is inherited.
	v := ResolveReferencePricingForStoreProductVariantWithCost(prod, 500000, "var-1", store, liveSettings(), nil, nil)
	if v.FinalPrice != 675000 || v.ActiveTier != "max-default" {
		t.Errorf("variant final = %d tier %q; want 675000/max-default (500000 x 1.35)", v.FinalPrice, v.ActiveTier)
	}
	// Variant percent override wins over the product margin.
	ov := &StoreVariantOverride{StoreID: "store-ds", ProductID: "prod-x", VariantID: "var-1", Value: 25, Mode: "percent"}
	v2 := ResolveReferencePricingForStoreProductVariantWithCost(prod, 500000, "var-1", store, liveSettings(), nil, ov)
	if v2.FinalPrice != 625000 || v2.ActiveTier != "variant" {
		t.Errorf("variant override final = %d tier %q; want 625000/variant", v2.FinalPrice, v2.ActiveTier)
	}
}

func TestReferenceMarginLockAdminSet(t *testing.T) {
	prod := domain.Product{ID: "prod-x", SupplierBaseCost: 1000000}
	store := domain.Store{
		OwnerType: "dropshipper", ProfitMarginPercent: 15,
		ProductPriceOverrides:     map[string]float64{"prod-x": 7},
		ProductPriceOverrideModes: map[string]string{"prod-x": "percent"},
	}
	locked := 30.0
	locks := []MarginLock{{Scope: "product", ScopeID: "prod-x", Mode: "admin_set", Value: &locked}}
	res := ResolveReferencePricing(prod, store, liveSettings(), locks)
	if res.FinalPrice != 1300000 || res.ActiveTier != "product-locked" {
		t.Errorf("locked final = %d tier %q; want 1300000/product-locked", res.FinalPrice, res.ActiveTier)
	}
}

func TestReferenceNicheAndFixedAreTODOU10(t *testing.T) {
	prod := domain.Product{ID: "prod-x", SupplierBaseCost: 1000000, NicheIDs: []string{"niche-a"}}
	store := domain.Store{
		OwnerType: "dropshipper", ProfitMarginPercent: 15,
		NichePriceOverrides:     map[string]float64{"niche-a": 50},
		NichePriceOverrideModes: map[string]string{"niche-a": "percent"},
	}
	res := ResolveReferencePricing(prod, store, liveSettings(), nil)
	if res.FinalPrice != 1350000 {
		t.Errorf("niche fallback final = %d; want 1350000 (max-allowed fallback; niche NOT applied, U10)", res.FinalPrice)
	}
	if res.UnverifiedNote == "" {
		t.Errorf("expected a U10 unverified note for the niche override")
	}
}

// Owner rule (2026-11-05): no store-wide default profit exists anymore.
// Margins live only per-product and per-variant; wherever no percent is
// set, the MAX ALLOWED margin for the product applies — its per-product
// ceiling row when present, else settings.MaxProfitMargin (35 here).
func TestMaxAllowedFallbackOwnerRule(t *testing.T) {
	// (a) plain product, no override -> MaxProfitMargin, even though the
	// store still carries a legacy ProfitMarginPercent (15) and the
	// settings a DefaultMargin (20): both are inert now.
	prod := domain.Product{ID: "prod-a", SupplierBaseCost: 1000000, OriginalPrice: 2000000}
	store := domain.Store{ID: "store-ds", OwnerType: "dropshipper", ProfitMarginPercent: 15}
	res := ResolveReferencePricing(prod, store, liveSettings(), nil)
	if res.MarginPercent != 35 || res.FinalPrice != 1350000 || res.ActiveTier != "max-default" {
		t.Errorf("(a) margin=%v final=%d tier=%q; want 35/1350000/max-default", res.MarginPercent, res.FinalPrice, res.ActiveTier)
	}

	// (b) per-product ceiling row 25 replaces the global max.
	s := liveSettings()
	s.ProductMarginCeilings = map[string]float64{"prod-a": 25}
	res = ResolveReferencePricing(prod, store, s, nil)
	if res.MarginPercent != 25 || res.FinalPrice != 1250000 || res.ActiveTier != "max-default" {
		t.Errorf("(b) margin=%v final=%d tier=%q; want 25/1250000/max-default", res.MarginPercent, res.FinalPrice, res.ActiveTier)
	}

	// (c) variant with NO override while a product override (20) exists:
	// the variant does NOT inherit the product override — it resolves at
	// the max allowed on the variant cost.
	prodC := domain.Product{ID: "prod-c", SupplierBaseCost: 1000000, OriginalPrice: 2000000}
	storeC := domain.Store{
		ID: "store-ds", OwnerType: "dropshipper",
		ProductPriceOverrides:     map[string]float64{"prod-c": 20},
		ProductPriceOverrideModes: map[string]string{"prod-c": "percent"},
	}
	v := ResolveReferencePricingForStoreProductVariantWithCost(prodC, 500000, "var-1", storeC, liveSettings(), nil, nil)
	if v.MarginPercent != 35 || v.FinalPrice != 675000 || v.ActiveTier != "max-default" {
		t.Errorf("(c) variant margin=%v final=%d tier=%q; want 35/675000/max-default (NOT the product override 20)", v.MarginPercent, v.FinalPrice, v.ActiveTier)
	}
	// (c2) the variant fallback is ceiling-aware: a ceiling row of 25
	// binds the variant too.
	sC := liveSettings()
	sC.ProductMarginCeilings = map[string]float64{"prod-c": 25}
	v = ResolveReferencePricingForStoreProductVariantWithCost(prodC, 500000, "var-1", storeC, sC, nil, nil)
	if v.MarginPercent != 25 || v.FinalPrice != 625000 {
		t.Errorf("(c2) variant margin=%v final=%d; want 25/625000", v.MarginPercent, v.FinalPrice)
	}
	// (c3) a legacy fixed-mode variant row is inert and also resolves at
	// the max fallback on the variant cost, keeping its U10 note.
	fixed := &StoreVariantOverride{StoreID: "store-ds", ProductID: "prod-c", VariantID: "var-1", Value: 999, Mode: "fixed"}
	v = ResolveReferencePricingForStoreProductVariantWithCost(prodC, 500000, "var-1", storeC, liveSettings(), nil, fixed)
	if v.MarginPercent != 35 || v.FinalPrice != 675000 || v.UnverifiedNote == "" {
		t.Errorf("(c3) fixed variant margin=%v final=%d note=%q; want 35/675000/U10 note", v.MarginPercent, v.FinalPrice, v.UnverifiedNote)
	}

	// (e) product override 20 on a plain product -> 20 (unchanged).
	p := ResolveReferencePricing(prodC, storeC, liveSettings(), nil)
	if p.MarginPercent != 20 || p.FinalPrice != 1200000 || p.ActiveTier != "product" {
		t.Errorf("(e) margin=%v final=%d tier=%q; want 20/1200000/product", p.MarginPercent, p.FinalPrice, p.ActiveTier)
	}

	// (d) variant override 15 -> 15 (unchanged, ceiling-capped as before).
	ov15 := &StoreVariantOverride{StoreID: "store-ds", ProductID: "prod-c", VariantID: "var-1", Value: 15, Mode: "percent"}
	v = ResolveReferencePricingForStoreProductVariantWithCost(prodC, 500000, "var-1", storeC, liveSettings(), nil, ov15)
	if v.MarginPercent != 15 || v.FinalPrice != 575000 || v.ActiveTier != "variant" {
		t.Errorf("(d) variant margin=%v final=%d tier=%q; want 15/575000/variant", v.MarginPercent, v.FinalPrice, v.ActiveTier)
	}

	// (f) admin product lock wins over everything: locked 30 beats the
	// product override (20) on the product, and beats a variant override
	// (15) on the variant.
	locked := 30.0
	locks := []MarginLock{{Scope: "product", ScopeID: "prod-c", Mode: "admin_set", Value: &locked}}
	p = ResolveReferencePricing(prodC, storeC, liveSettings(), locks)
	if p.MarginPercent != 30 || p.FinalPrice != 1300000 || p.ActiveTier != "product-locked" {
		t.Errorf("(f) locked product margin=%v final=%d tier=%q; want 30/1300000/product-locked", p.MarginPercent, p.FinalPrice, p.ActiveTier)
	}
	v = ResolveReferencePricingForStoreProductVariantWithCost(prodC, 500000, "var-1", storeC, liveSettings(), locks, ov15)
	if v.MarginPercent != 30 || v.FinalPrice != 650000 || v.ActiveTier != "product-locked" {
		t.Errorf("(f) locked variant margin=%v final=%d tier=%q; want 30/650000/product-locked", v.MarginPercent, v.FinalPrice, v.ActiveTier)
	}
}

func TestVariantAdminSetLockWinsOverOverrideAndCeiling(t *testing.T) {
	product := domain.Product{ID: "prod-c", SupplierBaseCost: 1000000}
	store := domain.Store{ID: "store-ds", OwnerType: "dropshipper"}
	settings := liveSettings()
	settings.ProductMarginCeilings = map[string]float64{product.ID: 25}
	override := StoreVariantOverride{StoreID: store.ID, ProductID: product.ID, VariantID: "var-1", Value: 15, Mode: "percent"}
	lockedMargin := 30.0
	locks := []MarginLock{{Scope: "variant", ScopeID: override.VariantID, Mode: "admin_set", Value: &lockedMargin}}

	res := ResolveReferencePricingForStoreProductVariantWithCost(product, 500000, override.VariantID, store, settings, locks, &override)
	if res.MarginPercent != 30 || res.FinalPrice != 650000 || res.ActiveTier != "variant" || !res.AdminLocked {
		t.Fatalf("variant lock resolution = %+v, want 30%% at 650000 with variant tier and AdminLocked", res)
	}
}
