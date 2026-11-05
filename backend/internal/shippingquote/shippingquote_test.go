package shippingquote

import "testing"

func TestLegFareToman(t *testing.T) {
	rates := Rates{BaseRateRial: 100000, PerKgRial: 20000, VolumetricDiv: 5000, DefaultWeightG: 500}
	cases := []struct {
		grams int64
		want  int64
	}{
		{500, 12000},   // ceil(0.5kg) = 1kg
		{1000, 12000},  // exactly 1kg
		{1001, 14000},  // spills into the 2kg band
		{2000, 14000},  // 2kg
		{3000, 16000},  // 3kg
		{10000, 30000}, // 10kg
	}
	for _, tc := range cases {
		if got := LegFareToman(tc.grams, rates); got != tc.want {
			t.Fatalf("LegFareToman(%d) = %d, want %d", tc.grams, got, tc.want)
		}
	}
	zero := Rates{VolumetricDiv: 5000, DefaultWeightG: 500}
	if got := LegFareToman(1000, zero); got != 0 {
		t.Fatalf("LegFareToman with zero rates = %d, want 0", got)
	}
}

func TestBillableWeightGrams(t *testing.T) {
	rates := Rates{VolumetricDiv: 5000, DefaultWeightG: 500}
	if got := BillableWeightGrams(0, 0, 0, 0, rates); got != 500 {
		t.Fatalf("unknown weight = %d, want the 500g default", got)
	}
	if got := BillableWeightGrams(300, 100, 100, 100, rates); got != 300 {
		t.Fatalf("actual over volumetric = %d, want 300", got)
	}
	// 500x500x500mm at divisor 5000 -> 25000g volumetric wins.
	if got := BillableWeightGrams(300, 500, 500, 500, rates); got != 25000 {
		t.Fatalf("volumetric = %d, want 25000", got)
	}
}
