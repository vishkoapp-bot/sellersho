package pricing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
)

// Timed-deals widget discounts (2026-11-05): the discount is carved
// out of the seller's own resolved margin in percentage points.
// widgetSettings mirrors the live bracelet ladder: platform pct 20,
// so R = C * 1.2.
func widgetSettings() *PlatformPricingSettings {
	return &PlatformPricingSettings{
		PlatformPct:      20,
		CentralProfitPct: 10,
		MaxProfitMargin:  35,
		MinProfitMargin:  5,
	}
}

func widgetBraceletStore() domain.Store {
	return domain.Store{
		ID: "store-ds", OwnerType: "dropshipper",
		ProductPriceOverrides:     map[string]float64{"prod-bracelet": 35},
		ProductPriceOverrideModes: map[string]string{"prod-bracelet": "percent"},
	}
}

// The owner-confirmed worked example: R = 69,600,000 at a 35% margin
// prices at 93,960,000; a 10-point widget discount leaves a 25%
// margin and a price of 87,000,000 (profit 17,400,000).
func TestDealsWidgetDiscountWorkedExample(t *testing.T) {
	prod := domain.Product{ID: "prod-bracelet", SupplierBaseCost: 58000000, OriginalPrice: 72500000}
	res := ResolveReferencePricing(prod, widgetBraceletStore(), widgetSettings(), nil)
	if res.ReferenceCost != 69600000 || res.MarginPercent != 35 || res.FinalPrice != 93960000 {
		t.Fatalf("regular resolution = R %d margin %v final %d; want 69600000/35/93960000", res.ReferenceCost, res.MarginPercent, res.FinalPrice)
	}
	if res.AdminLocked {
		t.Fatalf("a seller product override must not be marked admin-locked")
	}
	out, amount := ApplyDealsWidgetDiscount(res, 10)
	if out.FinalPrice != 87000000 {
		t.Errorf("discounted final = %d; want 87000000 (69600000 x 1.25)", out.FinalPrice)
	}
	if amount != 6960000 {
		t.Errorf("discount amount = %d; want 6960000", amount)
	}
	if out.SellerMargin != 17400000 || out.MarginPercent != 25 {
		t.Errorf("discounted margin = %d (%v%%); want 17400000 (25%%)", out.SellerMargin, out.MarginPercent)
	}
	// Cost, fee and reference cost are never touched, and the
	// resolution's OriginalPrice keeps its catalog semantics (the
	// display layer derives the compare-at price itself).
	if out.WholesaleCost != res.WholesaleCost || out.PlatformFee != res.PlatformFee || out.ReferenceCost != res.ReferenceCost || out.OriginalPrice != res.OriginalPrice {
		t.Errorf("discount touched cost fields: %+v vs %+v", out, res)
	}
}

// A discount equal to the margin lands exactly on the reference
// price; a larger one clamps there — the price never goes below R.
func TestDealsWidgetDiscountClampsAtReferenceCost(t *testing.T) {
	prod := domain.Product{ID: "prod-bracelet", SupplierBaseCost: 58000000, OriginalPrice: 72500000}
	res := ResolveReferencePricing(prod, widgetBraceletStore(), widgetSettings(), nil)
	for _, pct := range []float64{35, 50, 100} {
		out, amount := ApplyDealsWidgetDiscount(res, pct)
		if out.FinalPrice != 69600000 {
			t.Errorf("discount %v: final = %d; want 69600000 (= R, never below)", pct, out.FinalPrice)
		}
		if out.SellerMargin != 0 || out.MarginPercent != 0 {
			t.Errorf("discount %v: margin = %d (%v%%); want 0 (0%%)", pct, out.SellerMargin, out.MarginPercent)
		}
		if amount != 24360000 {
			t.Errorf("discount %v: amount = %d; want 24360000 (the whole margin)", pct, amount)
		}
	}
}

// Admin locks are never eroded by a widget discount: product lock,
// legacy store lock, variant lock, and a product lock binding its
// variants all keep their locked price.
func TestDealsWidgetDiscountAdminLocks(t *testing.T) {
	prod := domain.Product{ID: "prod-bracelet", SupplierBaseCost: 58000000, OriginalPrice: 72500000}
	lockVal := 30.0

	// (a) product admin_set lock.
	locks := []MarginLock{{Scope: "product", ScopeID: "prod-bracelet", Mode: "admin_set", Value: &lockVal}}
	res := ResolveReferencePricing(prod, widgetBraceletStore(), widgetSettings(), locks)
	if res.ActiveTier != "product-locked" || !res.AdminLocked || res.FinalPrice != 90480000 {
		t.Fatalf("(a) locked resolution = tier %q locked %v final %d; want product-locked/true/90480000", res.ActiveTier, res.AdminLocked, res.FinalPrice)
	}
	if out, amount := ApplyDealsWidgetDiscount(res, 10); out.FinalPrice != res.FinalPrice || amount != 0 {
		t.Errorf("(a) widget discount changed a product-locked price: final %d amount %d", out.FinalPrice, amount)
	}

	// (b) legacy admin store lock (tier "default" — only the marker
	// distinguishes it from a seller value).
	custom := 20.0
	lockedStore := domain.Store{ID: "store-ds", OwnerType: "dropshipper", IsMarginLockedByAdmin: true, CustomStoreMarginOverride: &custom}
	res = ResolveReferencePricing(prod, lockedStore, widgetSettings(), nil)
	if res.ActiveTier != "default" || !res.AdminLocked || res.FinalPrice != 83520000 {
		t.Fatalf("(b) store-locked resolution = tier %q locked %v final %d; want default/true/83520000", res.ActiveTier, res.AdminLocked, res.FinalPrice)
	}
	if out, amount := ApplyDealsWidgetDiscount(res, 10); out.FinalPrice != res.FinalPrice || amount != 0 {
		t.Errorf("(b) widget discount changed a store-locked price: final %d amount %d", out.FinalPrice, amount)
	}

	// (c) variant admin_set lock: the resolver reports tier
	// "variant" — indistinguishable from a seller override by tier
	// alone — so the marker is the only reliable signal.
	vlocks := []MarginLock{{Scope: "variant", ScopeID: "var-1", Mode: "admin_set", Value: &lockVal}}
	v := ResolveReferencePricingForStoreProductVariantWithCost(prod, 500000, "var-1", widgetBraceletStore(), widgetSettings(), vlocks, nil)
	if v.ActiveTier != "variant" || !v.AdminLocked || v.FinalPrice != 780000 {
		t.Fatalf("(c) variant-locked resolution = tier %q locked %v final %d; want variant/true/780000", v.ActiveTier, v.AdminLocked, v.FinalPrice)
	}
	if out, amount := ApplyDealsWidgetDiscount(v, 10); out.FinalPrice != v.FinalPrice || amount != 0 {
		t.Errorf("(c) widget discount changed a variant-locked price: final %d amount %d", out.FinalPrice, amount)
	}

	// (d) a product admin_set lock binds the product's variants too,
	// even over a seller variant override.
	ov := &StoreVariantOverride{StoreID: "store-ds", ProductID: "prod-bracelet", VariantID: "var-1", Value: 15, Mode: "percent"}
	v = ResolveReferencePricingForStoreProductVariantWithCost(prod, 500000, "var-1", widgetBraceletStore(), widgetSettings(), locks, ov)
	if v.ActiveTier != "product-locked" || !v.AdminLocked || v.FinalPrice != 780000 {
		t.Fatalf("(d) product-locked variant = tier %q locked %v final %d; want product-locked/true/780000", v.ActiveTier, v.AdminLocked, v.FinalPrice)
	}
	if out, amount := ApplyDealsWidgetDiscount(v, 10); out.FinalPrice != v.FinalPrice || amount != 0 {
		t.Errorf("(d) widget discount changed a product-locked variant price: final %d amount %d", out.FinalPrice, amount)
	}
}

// A variant with its own margin discounts on the variant's own
// reference cost: variant cost 64,000,000 -> R 76,800,000, override
// 15% -> 88,320,000; a 5-point discount leaves 10% -> 84,480,000.
func TestDealsWidgetDiscountVariantOwnMargin(t *testing.T) {
	prod := domain.Product{ID: "prod-bracelet", SupplierBaseCost: 58000000, OriginalPrice: 72500000}
	ov := &StoreVariantOverride{StoreID: "store-ds", ProductID: "prod-bracelet", VariantID: "var-gem", Value: 15, Mode: "percent"}
	v := ResolveReferencePricingForStoreProductVariantWithCost(prod, 64000000, "var-gem", widgetBraceletStore(), widgetSettings(), nil, ov)
	if v.ReferenceCost != 76800000 || v.FinalPrice != 88320000 || v.AdminLocked {
		t.Fatalf("variant resolution = R %d final %d locked %v; want 76800000/88320000/false", v.ReferenceCost, v.FinalPrice, v.AdminLocked)
	}
	out, amount := ApplyDealsWidgetDiscount(v, 5)
	if out.FinalPrice != 84480000 || amount != 3840000 {
		t.Errorf("discounted variant = final %d amount %d; want 84480000/3840000", out.FinalPrice, amount)
	}
	if out.SellerMargin != 7680000 || out.MarginPercent != 10 {
		t.Errorf("discounted variant margin = %d (%v%%); want 7680000 (10%%)", out.SellerMargin, out.MarginPercent)
	}
}

func TestDealsWidgetDiscountNoOps(t *testing.T) {
	prod := domain.Product{ID: "prod-bracelet", SupplierBaseCost: 58000000, OriginalPrice: 72500000}
	res := ResolveReferencePricing(prod, widgetBraceletStore(), widgetSettings(), nil)
	for _, pct := range []float64{0, -5} {
		if out, amount := ApplyDealsWidgetDiscount(res, pct); out.FinalPrice != res.FinalPrice || amount != 0 {
			t.Errorf("discount %v must be a no-op: final %d amount %d", pct, out.FinalPrice, amount)
		}
	}
	// A zero-margin resolution has nothing to give up.
	zeroStore := domain.Store{
		ID: "store-ds", OwnerType: "dropshipper",
		ProductPriceOverrides:     map[string]float64{"prod-bracelet": 0},
		ProductPriceOverrideModes: map[string]string{"prod-bracelet": "percent"},
	}
	zero := ResolveReferencePricing(prod, zeroStore, widgetSettings(), nil)
	if zero.SellerMargin != 0 {
		t.Fatalf("zero-margin setup: margin = %d; want 0", zero.SellerMargin)
	}
	if out, amount := ApplyDealsWidgetDiscount(zero, 10); out.FinalPrice != zero.FinalPrice || amount != 0 {
		t.Errorf("zero-margin discount must be a no-op: final %d amount %d", out.FinalPrice, amount)
	}
	// Belt-and-braces: a hand-built resolution carrying the
	// product-locked tier is refused even without the marker.
	hand := ReferenceResolution{ActiveTier: "product-locked", ReferenceCost: 1000000, BaseCost: 1000000, FinalPrice: 1300000, SellerMargin: 300000, MarginPercent: 30}
	if out, amount := ApplyDealsWidgetDiscount(hand, 10); out.FinalPrice != hand.FinalPrice || amount != 0 {
		t.Errorf("product-locked tier must be refused: final %d amount %d", out.FinalPrice, amount)
	}
}

// Window and scope rules for the widget config, using the same
// endsAt semantics as the storefront widget: a parseable endsAt in
// the past ends the campaign; a missing endsAt means no expiry.
func TestDealsWidgetConfigDiscountFor(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	parse := func(raw string) *DealsWidgetConfig { return ParseDealsWidgetConfig(json.RawMessage(raw)) }

	active := parse(`{"title":"t","productIds":["prod-a","prod-b"],"countdownHours":24,"endsAt":"2026-11-06T12:00:00Z","discounts":{"prod-a":10,"prod-x":50}}`)
	if pct, ok := active.DiscountFor("prod-a", now); !ok || pct != 10 {
		t.Errorf("active listed product = %v/%v; want 10/true", pct, ok)
	}
	if _, ok := active.DiscountFor("prod-b", now); ok {
		t.Errorf("listed product without a discounts entry must not discount")
	}
	if _, ok := active.DiscountFor("prod-x", now); ok {
		t.Errorf("a discounts entry for an unlisted product must not discount")
	}

	expired := parse(`{"title":"t","productIds":["prod-a"],"countdownHours":24,"endsAt":"2026-11-05T11:59:59Z","discounts":{"prod-a":10}}`)
	if _, ok := expired.DiscountFor("prod-a", now); ok {
		t.Errorf("an expired campaign must not discount")
	}
	endsNow := parse(`{"title":"t","productIds":["prod-a"],"countdownHours":24,"endsAt":"2026-11-05T12:00:00Z","discounts":{"prod-a":10}}`)
	if _, ok := endsNow.DiscountFor("prod-a", now); ok {
		t.Errorf("a campaign ending exactly now must not discount")
	}

	// Frontend parity: no endsAt = no expiry.
	noExpiry := parse(`{"title":"t","productIds":["prod-a"],"discounts":{"prod-a":10}}`)
	if pct, ok := noExpiry.DiscountFor("prod-a", now); !ok || pct != 10 {
		t.Errorf("missing endsAt = %v/%v; want 10/true (no-expiry parity)", pct, ok)
	}

	if ParseDealsWidgetConfig(nil) != nil {
		t.Errorf("empty raw config must parse to nil")
	}
	if ParseDealsWidgetConfig(json.RawMessage(`{broken`)) != nil {
		t.Errorf("invalid JSON must parse to nil")
	}
	var nilCfg *DealsWidgetConfig
	if _, ok := nilCfg.DiscountFor("prod-a", now); ok {
		t.Errorf("nil config must not discount")
	}
	if nilCfg.IsLive(now) {
		t.Errorf("nil config must not be live")
	}
}
