package shippingquote

import (
	"math"
	"testing"
)

func TestFareAvoidsFloatKilogramAndRialOverflow(t *testing.T) {
	// float64 loses the last gram and underprices by one kilogram band.
	if got := LegFareToman(9007199254741001, Rates{PerKgRial: 10}); got != 9007199254742 {
		t.Fatalf("last gram lost: %d", got)
	}
	if got := LegFareToman(1000, Rates{BaseRateRial: math.MaxInt64}); got != 922337203685477581 {
		t.Fatalf("half-up rial conversion overflowed: %d", got)
	}
}

func TestCheckedShippingMoneyBounds(t *testing.T) {
	for _, tc := range []struct {
		weight int64
		rates  Rates
	}{{-1, Rates{}}, {1, Rates{BaseRateRial: -1}}, {1, Rates{PerKgRial: -1}}, {math.MaxInt64, Rates{PerKgRial: math.MaxInt64}}} {
		if _, err := CheckedLegFareToman(tc.weight, tc.rates); err == nil {
			t.Fatalf("invalid fare accepted: %+v", tc)
		}
	}
	for _, tc := range []struct{ rial, want int64 }{{0, 0}, {4, 0}, {5, 1}, {14, 1}, {15, 2}, {math.MaxInt64, 922337203685477581}} {
		got, err := CheckedLegFareToman(0, Rates{BaseRateRial: tc.rial})
		if err != nil || got != tc.want {
			t.Fatalf("rial %d=%d,%v", tc.rial, got, err)
		}
	}
	if got, err := CheckedBillableWeightGrams(1, 10000000, 10000000, 10000000, Rates{VolumetricDiv: 1000}); err != nil || got != 1000000000000000000 {
		t.Fatalf("intermediate volume overflow: %d,%v", got, err)
	}
	if _, err := CheckedBillableWeightGrams(1, math.MaxInt, math.MaxInt, math.MaxInt, Rates{VolumetricDiv: 1}); err == nil {
		t.Fatal("out-of-range weight accepted")
	}
}
