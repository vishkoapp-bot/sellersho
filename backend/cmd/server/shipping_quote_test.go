package main

import "testing"

// FIX 5: pure quote-formula tests (no DB). Rates are Rials in storage
// and Tomans out: base 100000 Rial + 20000 Rial/kg over a 500g
// default-weight item bills 1 kg -> 12000 Toman.
func TestComputeShippingQuoteSingleOrigin(t *testing.T) {
	rates := shippingRates{BaseRateRial: 100000, PerKgRial: 20000, VolumetricDiv: 5000, DefaultWeightG: 500}
	q := computeShippingQuote([]shippingQuoteLine{
		{ProductID: "p1", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
	}, rates, "تهران|تهران|خیابان|1234567890")
	if len(q.Legs) != 1 {
		t.Fatalf("legs = %d, want 1", len(q.Legs))
	}
	if q.Legs[0].WeightGrams != 1000 {
		t.Fatalf("weight = %d, want 1000", q.Legs[0].WeightGrams)
	}
	if q.TotalShipping != 12000 {
		t.Fatalf("total = %d, want 12000", q.TotalShipping)
	}
	if q.QuoteID == "" || len(q.Allocations) != 1 || q.Allocations[0].LegIndex != 0 {
		t.Fatalf("bad quote id/allocations: %+v", q)
	}
	// Same input recomputes the same id (order-time revalidation).
	q2 := computeShippingQuote([]shippingQuoteLine{
		{ProductID: "p1", Quantity: 2, Fulfillment: "warehouse", Warehouse: "انبار مرکزی"},
	}, rates, "تهران|تهران|خیابان|1234567890")
	if q.QuoteID != q2.QuoteID {
		t.Fatalf("quote id not deterministic: %s vs %s", q.QuoteID, q2.QuoteID)
	}
}

func TestComputeShippingQuoteVolumetricAndMultiOrigin(t *testing.T) {
	rates := shippingRates{BaseRateRial: 100000, PerKgRial: 20000, VolumetricDiv: 5000, DefaultWeightG: 500}
	// 200x200x200mm = 8,000,000 mm^3 / 5000 = 1600g volumetric > 100g actual.
	q := computeShippingQuote([]shippingQuoteLine{
		{ProductID: "p1", Quantity: 1, Fulfillment: "warehouse", WeightG: 100, LengthMM: 200, WidthMM: 200, HeightMM: 200},
		{ProductID: "p2", Quantity: 1, Fulfillment: "supplier_direct", SupplierID: "s1"},
	}, rates, "اصفهان|اصفهان|خیابان|1234567890")
	if len(q.Legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(q.Legs))
	}
	// Leg fares: 100000 + 20000*ceil(kg). Totals are order-independent.
	if q.TotalShipping != (100000+20000*2+5)/10+(100000+20000*1+5)/10 {
		t.Fatalf("total = %d, want volumetric+default legs", q.TotalShipping)
	}
}

func TestBillableWeightDefault500(t *testing.T) {
	rates := shippingRates{VolumetricDiv: 5000, DefaultWeightG: 500}
	if got := billableWeightGrams(shippingQuoteLine{}, rates); got != 500 {
		t.Fatalf("default weight = %d, want 500", got)
	}
	if got := billableWeightGrams(shippingQuoteLine{WeightG: 300, LengthMM: 100, WidthMM: 100, HeightMM: 100}, rates); got != 300 {
		t.Fatalf("weight = %d, want 300 (volumetric 200g loses)", got)
	}
}
