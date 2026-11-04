package pricing

import (
	"testing"

	"github.com/drup/platform/internal/domain"
)

// Proven live examples (MASTER-AUDIT section 3(a)). Global platform_pct
// is 0 in live, so platformFeeF = 0 and baseCostB = C in every case here.

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

	d := ResolveReferencePricing(prod, dropshipper, liveSettings(), nil)
	if d.FinalPrice != 1380000 {
		t.Errorf("dropshipper final = %d; want 1380000", d.FinalPrice)
	}
	if d.BaseCost != 1200000 {
		t.Errorf("dropshipper baseCostB = %d; want 1200000 (= C, not C+F)", d.BaseCost)
	}
	if d.PlatformFee != 0 {
		t.Errorf("dropshipper platformFee = %d; want 0", d.PlatformFee)
	}
	if DiscountPercent(prod.OriginalPrice, d.FinalPrice) != 25 {
		t.Errorf("dropshipper discountPercent = %d; want 25", DiscountPercent(prod.OriginalPrice, d.FinalPrice))
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
	d := ResolveReferencePricing(prod, domain.Store{OwnerType: "dropshipper", ProfitMarginPercent: 15}, liveSettings(), nil)
	if d.FinalPrice != 66700000 {
		t.Errorf("dropshipper final = %d; want 66700000", d.FinalPrice)
	}
	if DiscountPercent(prod.OriginalPrice, d.FinalPrice) != 8 {
		t.Errorf("dropshipper discountPercent = %d; want 8", DiscountPercent(prod.OriginalPrice, d.FinalPrice))
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
	if split.PlatformFeeAmount != 0 || split.DropshipperMargin != 180000 || split.PlatformTotal != 0 {
		t.Errorf("dropshipper split = %+v; want fee 0, margin 180000, platform total 0", split)
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
	// Variant with its own cost, no override: product resolution on 500,000.
	v := ResolveReferencePricingForStoreProductVariantWithCost(prod, 500000, "var-1", store, liveSettings(), nil, nil)
	if v.FinalPrice != 575000 {
		t.Errorf("variant final = %d; want 575000 (500000 x 1.15)", v.FinalPrice)
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
	if res.FinalPrice != 1150000 {
		t.Errorf("niche fallback final = %d; want 1150000 (store default; niche NOT applied, U10)", res.FinalPrice)
	}
	if res.UnverifiedNote == "" {
		t.Errorf("expected a U10 unverified note for the niche override")
	}
}
