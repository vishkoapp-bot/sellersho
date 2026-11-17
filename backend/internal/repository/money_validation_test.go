package repository

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/drup/platform/internal/pricing"
)

func TestMoneyInputsRejectedBeforePersistence(t *testing.T) {
	ctx := context.Background()
	r := &PricingRepository{}
	for _, value := range []float64{math.NaN(), math.Inf(1), -1, .001, 1000.01} {
		if err := r.SetProductMarginCeiling(ctx, "p", value); err == nil {
			t.Fatal("ceiling accepted")
		}
		if err := r.SetStoreMarginLock(ctx, "s", pricing.MarginLock{Mode: "admin_set", Value: &value}); err == nil {
			t.Fatal("lock accepted")
		}
		stores := &StoreRepository{}
		if err := stores.SetStoreProductOverride(ctx, "s", "p", value, "percent"); err == nil {
			t.Fatal("product override accepted")
		}
		if err := stores.SetStoreMargin(ctx, "s", value); err == nil {
			t.Fatal("store margin accepted")
		}
	}
}

func TestCampaignDecimalAndFixedMoneyValidation(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		mode  string
		value float64
	}{{"percent", .001}, {"percent", math.NaN()}, {"percent", math.Inf(1)}, {"percent", 101}, {"fixed", .5}, {"fixed", 9007199254740992}} {
		if err := team05ValidateCampaign("campaign", "store", tc.mode, tc.value, now, now.Add(time.Hour)); err == nil {
			t.Fatalf("accepted %s %v", tc.mode, tc.value)
		}
	}
}

func TestAddonMoneyRejectedBeforePersistence(t *testing.T) {
	r := &SupplierRepository{}
	for _, addon := range []*SupplierAddon{{PriceMode: "fixed", PriceValue: .5}, {PriceMode: "percent", PriceValue: .001}, {PriceMode: "fixed", PriceValue: math.NaN()}} {
		if _, err := r.CreateSupplierAddon(context.Background(), "s", addon); err == nil {
			t.Fatal("invalid addon accepted")
		}
	}
}
