package catalogpricing

import (
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

func fixture() VariantInput {
	settings := pricing.DefaultPlatformPricingSettings()
	settings.PlatformPct = 0
	settings.CentralProfitPct = 20
	return VariantInput{Product: domain.Product{ID: "p", OriginalPrice: 1000000, SupplierBaseCost: 500000, SupplyOption: "1day"},
		Store: domain.Store{Slug: "central", OwnerType: "central"}, Settings: settings,
		Variants: []domain.ProductVariant{{ID: "v1", InventoryMode: "tracked", IsActive: true}, {ID: "v2", InventoryMode: "tracked", IsActive: true}},
		AsOf:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func TestVariantPricesUseOneExplicitTimeForAllSaleWindows(t *testing.T) {
	input := fixture()
	start := input.AsOf
	end := start.Add(time.Hour)
	sale := int64(550000)
	for i := range input.Variants {
		input.Variants[i].SalePrice = &sale
		input.Variants[i].SaleFrom = &start
		input.Variants[i].SaleTo = &end
	}
	for _, tc := range []struct {
		at     time.Time
		want   int64
		onSale bool
	}{
		{start.Add(-time.Nanosecond), 600000, false}, {start, 550000, true}, {end.Add(-time.Nanosecond), 550000, true}, {end, 600000, false},
	} {
		input.AsOf = tc.at
		prices, err := ResolveVariantsAt(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"v1", "v2"} {
			if prices[id].Price != tc.want || prices[id].OnSale != tc.onSale {
				t.Fatalf("asOf%s variant%s: %+v", tc.at, id, prices[id])
			}
		}
	}
}

func TestVariantTimeAppliesToWidgetAndSaleTogether(t *testing.T) {
	input := fixture()
	// Variants without an override use the allowed maximum, not store defaults.
	input.Settings.MaxProfitMargin = 30
	input.Store = domain.Store{Slug: "seller", OwnerType: "dropshipper"}
	end := input.AsOf.Add(time.Hour)
	sale := int64(575000)
	input.Widget = &pricing.DealsWidgetConfig{ProductIDs: []string{"p"}, Discounts: map[string]float64{"p": 10}, EndsAt: end.Format(time.RFC3339)}
	for i := range input.Variants {
		input.Variants[i].SalePrice = &sale
		input.Variants[i].SaleTo = &end
	}
	before, err := ResolveVariantsAt(input)
	if err != nil {
		t.Fatal(err)
	}
	input.AsOf = end
	after, err := ResolveVariantsAt(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"v1", "v2"} {
		if before[id].Price != 575000 || !before[id].OnSale || after[id].Price != 650000 || after[id].OnSale {
			t.Fatalf("shared widget/sale time mismatch: before%+v after%+v", before[id], after[id])
		}
	}
}

func TestVariantSourceRequiresTimeAndSettingsAndPreservesTrackedQuarantine(t *testing.T) {
	input := fixture()
	input.AsOf = time.Time{}
	if _, err := ResolveVariantsAt(input); err == nil {
		t.Fatal("missing asOf silently used wall clock")
	}
	input = fixture()
	input.Settings = nil
	if _, err := ResolveVariantsAt(input); err == nil {
		t.Fatal("missing source settings silently used defaults")
	}
	input = fixture()
	input.Variants[0].IsActive = false
	prices, err := ResolveVariantsAt(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := prices["v1"]; ok {
		t.Fatal("inactive tracked SKU was priced")
	}
	if _, ok := prices["v2"]; !ok {
		t.Fatal("active tracked SKU vanished")
	}
}
