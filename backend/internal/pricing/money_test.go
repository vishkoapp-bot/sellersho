package pricing

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
)

func TestReferencePricingNeverPaysNegativePlatformFee(t *testing.T) {
	s := DefaultPlatformPricingSettings()
	s.CentralProfitPct = 0
	for _, cost := range []int64{1, 49, 50, 101, 149, 150, 151} {
		r := ResolveReferencePricing(domain.Product{SupplierBaseCost: cost}, domain.Store{}, s, nil)
		if r.ReferenceCost < cost || r.PlatformFee < 0 || r.FinalPrice < r.ReferenceCost || r.SellerMargin < 0 {
			t.Fatalf("cost=%d produced negative fee/margin: %+v", cost, r)
		}
	}
}

func TestDecimalParserRejectsUnboundedAndNonJSONTokens(t *testing.T) {
	// First case safely reproduces the missing lexical guard before huge
	// exponents are ever evaluated by the old implementation.
	for _, value := range []string{"1/2", "+1", "01", ".5", "1.", "1e1000000000", "1e-1000000000", "0." + strings.Repeat("0", 256)} {
		started := time.Now()
		if _, err := ParsePercentDecimal(value); err == nil {
			t.Fatalf("invalid decimal percent accepted: %s", value)
		}
		if _, err := MoneyFromDecimal(value); err == nil {
			t.Fatalf("invalid decimal money accepted: %s", value)
		}
		if time.Since(started) > time.Second {
			t.Fatalf("decimal rejection exceeded bounded processing time")
		}
	}
	for _, value := range []string{"0", "0.29", "2.9e-1", "1000.00", "0e128", "0e-128"} {
		if _, err := ParsePercentDecimal(value); err != nil {
			t.Fatalf("valid bounded decimal %q: %v", value, err)
		}
	}
}

func TestInvalidDiscountCannotConsumeMargin(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), -1, 0.001} {
		res := testPercentRes()
		got := ApplyCampaignDiscount(res, DiscountCampaignInput{Mode: "percent", Value: value})
		if got.Resolution.FinalPrice != res.FinalPrice || got.DiscountAmount != 0 {
			t.Fatalf("invalid discount %v changed payable: %+v", value, got)
		}
	}
}

func TestMoneyBoundaries(t *testing.T) {
	for _, tc := range []struct{ in, want int64 }{{0, 0}, {49, 0}, {50, 100}, {149, 100}, {150, 200}, {math.MaxInt64 - 107, math.MaxInt64 - 107}} {
		got, err := RoundMoneyTo100(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("round(%d)=%d,%v want %d", tc.in, got, err, tc.want)
		}
	}
	if got, err := RoundMoneyTo100(math.MaxInt64); err != nil || got != math.MaxInt64-7 {
		t.Fatalf("near limit rounding=%d,%v", got, err)
	}
	if _, err := MarkedUpPrice(math.MaxInt64, .01); err == nil {
		t.Fatal("marked-up overflow accepted")
	}
	if _, err := CheckedMultiply(math.MaxInt64, 2); err == nil {
		t.Fatal("multiply overflow accepted")
	}
	if _, err := CheckedAdd(math.MaxInt64, 1); err == nil {
		t.Fatal("addition overflow accepted")
	}
	if _, err := CheckedMultiply(-1, 1); err == nil {
		t.Fatal("negative money accepted")
	}
	for _, pct := range []float64{math.NaN(), math.Inf(1), -1, .001, 1000.01} {
		if _, err := ParsePercent(pct); err == nil {
			t.Fatalf("invalid percent %v accepted", pct)
		}
	}
	for _, pct := range []float64{0, .01, .29, 5.25, 1000} {
		if _, err := ParsePercent(pct); err != nil {
			t.Fatalf("valid percent %v: %v", pct, err)
		}
	}
	fee, err := TruncatedPercent(math.MaxInt64, 5.25)
	if err != nil || fee != 484227031934875729 {
		t.Fatalf("exact large fee = %d,%v", fee, err)
	}
	price, err := MarkedUpPrice(100000, .25)
	if err != nil || price != 100300 {
		t.Fatalf("fractional half100 = %d,%v", price, err)
	}
}

func TestExactAllocationAndCumulativeRefunds(t *testing.T) {
	for _, total := range []int64{0, 1, 101, math.MaxInt64} {
		for _, weights := range [][]int64{{1, 1, 1}, {math.MaxInt64, math.MaxInt64}, {0, 0}} {
			parts, err := AllocateMoney(total, weights)
			if err != nil {
				t.Fatal(err)
			}
			var sum int64
			for _, p := range parts {
				if p < 0 {
					t.Fatal("negative allocation")
				}
				sum += p
			}
			if sum != total {
				t.Fatalf("split %d,%v = %v", total, weights, parts)
			}
		}
	}
	parts, _ := AllocateMoney(5, []int64{1, 1, 1})
	if parts[0] != 1 || parts[1] != 1 || parts[2] != 3 {
		t.Fatalf("last positive index must absorb remainder: %v", parts)
	}
	var refunded int64
	for i := int64(0); i < 7; i++ {
		part, err := PartialRefund(101, 7, i, 1)
		if err != nil {
			t.Fatal(err)
		}
		refunded += part
		if refunded > 101 {
			t.Fatal("over refunded")
		}
	}
	if refunded != 101 {
		t.Fatalf("refund total=%d", refunded)
	}
	if _, err := PartialRefund(101, 7, 6, 2); err == nil {
		t.Fatal("excess refund accepted")
	}
}

func TestTransportMoneyAndTotalsRejectOverflow(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), -1, 1.5, 9007199254740992} {
		if _, err := MoneyFromNumber(value); err == nil {
			t.Fatalf("invalid transport money accepted: %v", value)
		}
	}
	total := int64(9007199254740990)
	if err := AccumulateMoney(&total, 1, 2); err == nil || total != 9007199254740990 {
		t.Fatal("overflow total mutated")
	}
	if err := AccumulateMoney(&total, 1, 1); err != nil || total != MaxTransportMoney {
		t.Fatal("valid boundary rejected")
	}
}

func TestMoneyConservationProperties(t *testing.T) {
	s := DefaultPlatformPricingSettings()
	for cost := int64(0); cost < 501; cost++ {
		for _, pct := range []float64{0, .01, .25, 5.25, 100} {
			s.PlatformPct = pct
			s.CentralProfitPct = pct
			r := ResolveReferencePricing(domain.Product{SupplierBaseCost: cost}, domain.Store{}, s, nil)
			if r.PricingError != "" || r.PlatformFee < 0 || r.SellerMargin < 0 || r.FinalPrice != cost+r.PlatformFee+r.SellerMargin {
				t.Fatalf("nonconserving %+v", r)
			}
			for _, off := range []float64{0, .01, .25, 50, 100} {
				d := ApplyCodeDiscount(r, DiscountCodeInput{Mode: "percent", Value: off})
				if d.Resolution.FinalPrice < r.BaseCost || d.Resolution.FinalPrice > r.FinalPrice || d.DiscountAmount+d.Resolution.FinalPrice != r.FinalPrice {
					t.Fatalf("bad discount %+v", d)
				}
				fee, err := TruncatedPercent(cost, off)
				if err != nil || fee < 0 || fee > cost || fee+(cost-fee) != cost {
					t.Fatal("fee/net conservation failed")
				}
			}
		}
	}
}

func TestZeroDiscountPreservesNonroundedSnapshotAmount(t *testing.T) {
	for _, mode := range []string{"fixed", "percent"} {
		final, err := DiscountedPrice(1249, 1000, mode, 0)
		if err != nil || final != 1249 {
			t.Fatalf("zero %s changed payable: %d,%v", mode, final, err)
		}
		res := ReferenceResolution{BaseCost: 1000, ReferenceCost: 1000, WholesaleCost: 1000, FinalPrice: 1249, SellerMargin: 249}
		got := ApplyCampaignDiscount(res, DiscountCampaignInput{Mode: mode, Value: 0})
		if got.DiscountAmount != 0 || got.Resolution.FinalPrice != 1249 || got.Resolution.SellerMargin != 249 {
			t.Fatalf("zero campaign changed money: %+v", got)
		}
	}
}
