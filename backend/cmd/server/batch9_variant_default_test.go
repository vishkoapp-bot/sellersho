package main

import (
	"testing"

	"github.com/drup/platform/internal/domain"
)

func TestBatch9VariantDefault(t *testing.T) {
	p := domain.Product{Title: "کالای تست", ProductType: "variable"}
	variants := []domain.ProductVariant{
		p1p2Variant("expensive", false, true, 1, nil),
		p1p2Variant("stored", true, true, 1, nil),
		p1p2Variant("cheap", false, true, 0, nil),
	}
	variants[0].Name = "پیش‌فرض"
	variants[0].Attributes = map[string]interface{}{"size": 42, "empty": "", "color": "زرد"}
	variants[2].Name = "پیش‌فرض"
	for _, tc := range []struct {
		name   string
		prices map[string]VariantPriceResponse
		winner string
	}{
		{"cheapest even without stock", map[string]VariantPriceResponse{"expensive": {Price: 200}, "stored": {Price: 300}, "cheap": {Price: 100}}, "cheap"},
		{"stored wins tie", map[string]VariantPriceResponse{"expensive": {Price: 100}, "stored": {Price: 100}, "cheap": {Price: 100}}, "stored"},
		{"first wins tie without stored", map[string]VariantPriceResponse{"expensive": {Price: 100}, "stored": {Price: 300}, "cheap": {Price: 100}}, "expensive"},
		{"no priced variants", map[string]VariantPriceResponse{"cheap": {Price: 0}}, "stored"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := buildProductResponse(p, p1p2Res(300, 300), variants, tc.prices, "stored")
			if out.StoreDefaultVariantID == nil || *out.StoreDefaultVariantID != "stored" {
				t.Fatalf("storeDefaultVariantId = %v, want stored", out.StoreDefaultVariantID)
			}
			if len(out.Variants) != len(variants) {
				t.Fatalf("rendered %d variants, want %d", len(out.Variants), len(variants))
			}
			for i, v := range out.Variants {
				if v.ID != variants[i].ID || v.IsDefault != (v.ID == tc.winner) {
					t.Errorf("variant %d = %s, default %t, want %s with default on %s", i, v.ID, v.IsDefault, variants[i].ID, tc.winner)
				}
			}
			for i, name := range []string{"زرد / 42", variants[1].Name, p.Title} {
				v := out.Variants[i]
				if v.Name != name || v.VariantName != name {
					t.Errorf("variant %s names = %q / %q, want %q", v.ID, v.Name, v.VariantName, name)
				}
			}
		})
	}
}

func TestBatch9SimpleVariantDefault(t *testing.T) {
	p := domain.Product{ProductType: "simple"}
	variant := p1p2Variant("simple", true, true, 1, nil)
	variant.Name = "پیش‌فرض"
	out := buildProductResponse(p, p1p2Res(100, 100), []domain.ProductVariant{variant}, map[string]VariantPriceResponse{"simple": {Price: 100}})
	if len(out.Variants) != 1 || out.Variants[0].Name != variant.Name || out.Variants[0].VariantName != variant.Name || !out.Variants[0].IsDefault {
		t.Fatalf("simple variants = %+v", out.Variants)
	}
}

func TestBatch9LegacyVariableBracelet(t *testing.T) {
	for _, tc := range []struct {
		name, productType, reservedName string
		pricingError                    string
	}{
		{"legacy", "legacy", "پیش‌فرض", ""},
		{"implicit legacy without ZWNJ", "", "پیشفرض", ""},
		{"cheaper price with error", "legacy", "پیش‌فرض", "invalid price"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Product{Title: "دستبند طلا", ProductType: tc.productType}
			synthetic := p1p2Variant("synthetic", true, true, 5, nil)
			synthetic.Name = tc.reservedName
			synthetic.Attributes = map[string]interface{}{"color": "زرد"}
			cheaper := p1p2Variant("gemstone", false, true, 1, nil)
			cheaper.Name = "نگین‌دار"
			out := buildProductResponse(p, p1p2Res(200, 200), []domain.ProductVariant{synthetic, cheaper}, map[string]VariantPriceResponse{
				"synthetic": {Price: 200},
				"gemstone":  {Price: 100, PricingError: tc.pricingError},
			})
			wantCount := 2
			if tc.pricingError != "" {
				wantCount = 1
				if _, ok := out.VariantPrices["gemstone"]; ok {
					t.Error("errored price is still present")
				}
			}
			if out.ProductType != "legacy" || len(out.Variants) != wantCount {
				t.Fatalf("productType = %q, variants = %+v", out.ProductType, out.Variants)
			}
			v := out.Variants[0]
			if v.ID != synthetic.ID || v.Name != "زرد" || v.VariantName != "زرد" || !v.IsActive || v.Stock != 5 || out.VariantPrices[v.ID].Price != 200 {
				t.Errorf("synthetic bracelet variant is not purchasable under its attribute label: %+v", v)
			}
			if v.IsDefault != (wantCount == 1) || (wantCount == 2 && !out.Variants[1].IsDefault) {
				t.Errorf("wrong cheapest default: %+v", out.Variants)
			}
		})
	}
}
