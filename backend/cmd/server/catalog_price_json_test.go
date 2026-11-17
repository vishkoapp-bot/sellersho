package main

import (
	"encoding/json"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

func TestConfiguredSuspendedSaleRetainsAuthoritativeFalseOnWire(t *testing.T) {
	p := domain.Product{ID: "sale-wire-product", OriginalPrice: 1000000, SupplierBaseCost: 500000, ProductType: "legacy", SupplyOption: "1day"}
	store := domain.Store{Slug: "central", OwnerType: "central"}
	sale := int64(1) // Below R: backend must suspend it, never let a client reapply it.
	variants := []domain.ProductVariant{{ID: "sale-wire-variant", InventoryMode: "tracked", IsActive: true, SalePrice: &sale}}
	prices := priceVariantsForResponse(p, variants, store, pricing.DefaultPlatformPricingSettings(), nil, nil, nil)
	price, ok := prices["sale-wire-variant"]
	if !ok || price.PricingError != "" || price.OnSale || price.Price <= sale {
		t.Fatalf("fixture failed to produce a suspended sale: %+v", price)
	}
	body, err := json.Marshal(prices)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	flag, present := wire["sale-wire-variant"]["onSale"]
	if !present || flag != false {
		t.Fatal("configured suspended sale lost explicit onSale:false; legacy client fallback can reapply the below-floor sale")
	}
	if wire["sale-wire-variant"]["price"] != float64(price.Price) {
		t.Fatal("transport changed the authoritative price")
	}
}

func TestVariantSaleWirePreservesNoSaleShapeAndTrueFlag(t *testing.T) {
	sale := int64(100)
	for _, tc := range []struct {
		name    string
		price   VariantPriceResponse
		present bool
		flag    bool
	}{
		{"no configured sale", VariantPriceResponse{Price: 200, OriginalPrice: 300}, false, false},
		{"configured no winner", VariantPriceResponse{Price: 200, OriginalPrice: 300, SalePrice: &sale}, true, false},
		{"configured winner", VariantPriceResponse{Price: 100, OriginalPrice: 300, SalePrice: &sale, OnSale: true}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.price)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(body, &wire); err != nil {
				t.Fatal(err)
			}
			flag, present := wire["onSale"]
			if present != tc.present || (present && flag != tc.flag) {
				t.Fatalf("onSale=%v present=%v", flag, present)
			}
		})
	}
}
