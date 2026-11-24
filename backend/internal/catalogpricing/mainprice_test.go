package catalogpricing

// PERF-01: ResolveMainPrice must equal the card price the list handler
// renders for the same inputs (product-level engine + variant pick order
// from cardPrice.ts + campaign/widget/sale composition). All expectations
// below are hand-computed from the engine formulas, not copied from the
// implementation: R = RoundTo100(C * 1.10) with platformPct 10;
// central final = RoundTo100(R * 1.10); dropshipper final (no overrides)
// = RoundTo100(R * 1.35) at the 35% max-allowed margin.
import (
	"encoding/json"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

func perf01Settings() *pricing.PlatformPricingSettings {
	return &pricing.PlatformPricingSettings{
		PlatformPct: 10, CentralProfitPct: 10, MaxProfitMargin: 35,
		MinProfitMargin: 3, DefaultMargin: 20,
		ProductPlatformPcts: map[string]float64{}, CategoryPlatformPcts: map[string]float64{},
		ProductMarginCeilings: map[string]float64{},
	}
}

func perf01Product() domain.Product {
	return domain.Product{
		ID: "perf01-prod", Title: "Test", Slug: "test", CategorySlug: "cat",
		SupplierBaseCost: 1000000, OriginalPrice: 2000000, IsActive: true,
	}
}

func perf01Central() domain.Store {
	return domain.Store{ID: "central", Slug: "central", OwnerType: "central"}
}

func perf01Dropshipper() domain.Store {
	return domain.Store{ID: "store-a", Slug: "store-a", OwnerType: "dropshipper"}
}

func TestMainPriceCentralNoVariants(t *testing.T) {
	asOf := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	got, err := ResolveMainPrice(MainPriceInput{
		Product: perf01Product(), Store: perf01Central(),
		Settings: perf01Settings(), AsOf: asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	// R = 1,100,000; final = 1,210,000; discount = round(790,000/2,000,000) = 40.
	if got.Price != 1210000 || got.OriginalPrice != 2000000 || got.DiscountPercent != 40 {
		t.Fatalf("got %+v", got)
	}
	if got.PriceMin == nil || *got.PriceMin != 1210000 || got.PriceMax == nil || *got.PriceMax != 1210000 {
		t.Fatalf("zero-variant range must be the product price, got %+v", got)
	}
	if got.ValidUntil != nil {
		t.Fatalf("no time windows: validUntil must be nil, got %v", got.ValidUntil)
	}
}

func TestMainPricePicksDefaultVariantThenStoreDefault(t *testing.T) {
	asOf := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	costDefault := int64(2000000)
	costSecond := int64(1500000)
	variants := []domain.ProductVariant{
		{ID: "v-default", ProductID: "perf01-prod", IsDefault: true, IsActive: true, InventoryMode: "tracked", SupplierBaseCost: &costDefault},
		{ID: "v-second", ProductID: "perf01-prod", IsActive: true, InventoryMode: "tracked", SupplierBaseCost: &costSecond},
	}
	// Default variant: R = 2,200,000, final = 2,420,000 (original raised
	// to final). Second: R = 1,650,000, final = 1,815,000.
	got, err := ResolveMainPrice(MainPriceInput{
		Product: perf01Product(), Variants: variants, Store: perf01Central(),
		Settings: perf01Settings(), AsOf: asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 2420000 || got.OriginalPrice != 2420000 || got.DiscountPercent != 0 {
		t.Fatalf("default-variant pick: got %+v", got)
	}
	if got.PriceMin == nil || *got.PriceMin != 1815000 || got.PriceMax == nil || *got.PriceMax != 2420000 {
		t.Fatalf("range over active priced variants: got min=%v max=%v", got.PriceMin, got.PriceMax)
	}
	// Store default wins over the product default.
	got, err = ResolveMainPrice(MainPriceInput{
		Product: perf01Product(), Variants: variants, Store: perf01Central(),
		Settings: perf01Settings(), StoreDefaultVariantID: "v-second", AsOf: asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 1815000 {
		t.Fatalf("store-default pick: got %+v", got)
	}
}

func TestCodedCardVariantRespectsStoreChoiceAndMinimum(t *testing.T) {
	p := domain.Product{IsActive: true, ProductType: "variable", SchemaVersion: 2}
	variants := []domain.ProductVariant{
		{ID: "expensive", IsActive: true, IsDefault: true, InventoryMode: "tracked", MinCartQty: 1},
		{ID: "cheap", IsActive: true, InventoryMode: "tracked", MinCartQty: 3},
	}
	prices := map[string]VariantPrice{
		"expensive": {Price: 66000},
		"cheap":     {Price: 13200, OnSale: true},
	}
	stock := map[string]int{"expensive": 1, "cheap": 3}
	if got := SelectCodedCardVariant(p, variants, prices, "", stock); got != "cheap" {
		t.Fatalf("active effective sale price loses to default: %s", got)
	}
	if got := SelectCodedCardVariant(p, variants, prices, "expensive", stock); got != "expensive" {
		t.Fatalf("purchasable store default loses: %s", got)
	}
	stock["cheap"] = 2
	if got := SelectCodedCardVariant(p, variants, prices, "cheap", stock); got != "expensive" {
		t.Fatalf("store default below minimum did not fall through: %s", got)
	}
	if got := SelectCodedCardVariant(p, variants, prices, "cheap", map[string]int{}); got != "expensive" {
		t.Fatalf("no purchasable variants must preserve the first priced fallback: %s", got)
	}
}

func TestMainPriceDropshipperCampaignWidgetSale(t *testing.T) {
	asOf := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	base := MainPriceInput{
		Product: perf01Product(), Store: perf01Dropshipper(),
		Settings: perf01Settings(), AsOf: asOf,
	}
	// Dropshipper, no overrides: margin 35% -> final 1,485,000;
	// discount round(515,000/2,000,000) = 26.
	got, err := ResolveMainPrice(base)
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 1485000 || got.DiscountPercent != 26 {
		t.Fatalf("dropshipper base: got %+v", got)
	}
	// Store campaign 10%: discount = 10% of final = 148,500;
	// 1,485,000 - 148,500 = 1,336,500 (already a multiple of 100).
	ends := asOf.Add(2 * time.Hour)
	campaign := domain.SellerDiscountCampaign{
		ID: "camp-1", StoreID: "store-a", Scope: "store", Mode: "percent",
		Value: 10, StartsAt: asOf.Add(-time.Hour), EndsAt: ends, IsActive: true,
	}
	got, err = ResolveMainPrice(MainPriceInput{
		Product: perf01Product(), Store: perf01Dropshipper(),
		Settings: perf01Settings(), ActiveCampaigns: []domain.SellerDiscountCampaign{campaign},
		AllCampaigns: []domain.SellerDiscountCampaign{campaign}, AsOf: asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 1336500 {
		t.Fatalf("campaign price: got %+v", got)
	}
	if got.ValidUntil == nil || !got.ValidUntil.Equal(ends) {
		t.Fatalf("validUntil must be the campaign end, got %v", got.ValidUntil)
	}
	// Widget 5 points off the 35% margin -> 30% -> 1,430,000; the
	// widget rewrites OriginalPrice to the pre-widget final (1,485,000).
	raw, _ := json.Marshal(map[string]any{
		"title": "deals", "productIds": []string{"perf01-prod"},
		"discounts": map[string]float64{"perf01-prod": 5},
		"endsAt":    asOf.Add(3 * time.Hour).Format(time.RFC3339),
	})
	widget := pricing.ParseDealsWidgetConfig(raw)
	got, err = ResolveMainPrice(MainPriceInput{
		Product: perf01Product(), Store: perf01Dropshipper(),
		Settings: perf01Settings(), Widget: widget, AsOf: asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 1430000 || got.OriginalPrice != 1485000 || got.DiscountPercent != 4 {
		t.Fatalf("widget price: got %+v", got)
	}
	// Sale: default variant final 2,420,000 (R 2,200,000); sale 2,300,000
	// is inside [R, final) and in-window, so it replaces the price.
	salePrice := int64(2300000)
	saleFrom := asOf.Add(-time.Hour)
	saleTo := asOf.Add(time.Hour)
	costDefault := int64(2000000)
	got, err = ResolveMainPrice(MainPriceInput{
		Product: perf01Product(),
		Variants: []domain.ProductVariant{
			{ID: "v-default", ProductID: "perf01-prod", IsDefault: true, IsActive: true,
				InventoryMode: "tracked", SupplierBaseCost: &costDefault,
				SalePrice: &salePrice, SaleFrom: &saleFrom, SaleTo: &saleTo},
		},
		Store: perf01Central(), Settings: perf01Settings(), AsOf: asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 2300000 || got.DiscountPercent != 5 {
		t.Fatalf("sale price: got %+v", got)
	}
	if got.ValidUntil == nil || !got.ValidUntil.Equal(saleTo) {
		t.Fatalf("validUntil must be the sale end, got %v", got.ValidUntil)
	}
}

func TestMainPriceSkipsInactiveVariantAndPricingError(t *testing.T) {
	asOf := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	costHigh := int64(2000000)
	costLow := int64(1500000)
	variants := []domain.ProductVariant{
		{ID: "v-off", ProductID: "perf01-prod", IsDefault: true, IsActive: false, InventoryMode: "tracked", SupplierBaseCost: &costHigh},
		{ID: "v-on", ProductID: "perf01-prod", IsActive: true, InventoryMode: "tracked", SupplierBaseCost: &costLow},
	}
	got, err := ResolveMainPrice(MainPriceInput{
		Product: perf01Product(), Variants: variants, Store: perf01Central(),
		Settings: perf01Settings(), AsOf: asOf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 1815000 {
		t.Fatalf("inactive default must be skipped, got %+v", got)
	}
	// A product whose pricing errors has no main price at all.
	bad := perf01Product()
	bad.SupplierBaseCost = pricing.MaxTransportMoney + 1
	if _, err := ResolveMainPrice(MainPriceInput{
		Product: bad, Store: perf01Central(), Settings: perf01Settings(), AsOf: asOf,
	}); err == nil {
		t.Fatal("pricing-error product must fail ResolveMainPrice")
	}
}
