package pricing

import (
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
)

// TEST-02: property test for the FIN-012 discount policy (owner decision
// 2026-11-06, documented in discount.go): a campaign/code discount is carved
// ONLY from the seller's own margin, never below the reference cost R
// (BaseCost), never above the original final price, with exact integer math
// and a single round-to-nearest-100 (half up) of the discounted price.
//
// Invariants, for every generated (base, margin, mode, value):
//   - a valid discount never errors
//   - base <= price <= final (discount never exceeds the margin)
//   - a non-zero discount yields a price that is a multiple of 100;
//     a zero discount preserves the exact existing final price
//   - the price is monotone non-increasing in the discount value
//   - campaign-then-code composition keeps the same bounds and the
//     bookkeeping exact: DiscountAmount == original final - result final,
//     SellerMargin == result final - BaseCost.

type discountCase struct {
	Base    int64   // R, reference cost (>= 100)
	Margin  int64   // D = P - R (>= 0)
	Percent float64 // campaign/code percent, 0..100
	Fixed   int64   // campaign/code fixed toman, 0..2*margin
}

func (discountCase) Generate(r *rand.Rand, _ int) reflect.Value {
	base := int64(r.Intn(100000)+1) * 100 // 100 .. 10,000,000, multiple of 100
	margin := int64(r.Intn(50000)) * 100  // 0 .. 5,000,000, multiple of 100
	return reflect.ValueOf(discountCase{
		Base:    base,
		Margin:  margin,
		Percent: float64(r.Intn(101)), // 0..100 whole percent
		Fixed:   int64(r.Intn(100001)) * 100,
	})
}

func TestDiscountedPriceProperty(t *testing.T) {
	// Percent mode.
	if err := quick.Check(func(c discountCase) bool {
		final := c.Base + c.Margin
		price, err := DiscountedPrice(final, c.Base, "percent", c.Percent)
		if err != nil {
			t.Logf("percent %+v final=%d: unexpected error %v", c, final, err)
			return false
		}
		if price < c.Base || price > final {
			t.Logf("percent %+v final=%d: price %d outside [base,final]", c, final, price)
			return false
		}
		if c.Percent == 0 {
			return price == final // zero discount preserves the exact price
		}
		return price%100 == 0
	}, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
	// Fixed mode.
	if err := quick.Check(func(c discountCase) bool {
		final := c.Base + c.Margin
		price, err := DiscountedPrice(final, c.Base, "fixed", float64(c.Fixed))
		if err != nil {
			t.Logf("fixed %+v final=%d: unexpected error %v", c, final, err)
			return false
		}
		if price < c.Base || price > final {
			t.Logf("fixed %+v final=%d: price %d outside [base,final]", c, final, price)
			return false
		}
		if c.Fixed == 0 {
			return price == final
		}
		return price%100 == 0
	}, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
	// Monotonicity: a bigger discount never raises the price.
	if err := quick.Check(func(c discountCase) bool {
		final := c.Base + c.Margin
		if c.Percent >= 100 {
			// 100% is the cap: both calls take the full margin.
			a, err1 := DiscountedPrice(final, c.Base, "percent", 100)
			b, err2 := DiscountedPrice(final, c.Base, "percent", 100)
			return err1 == nil && err2 == nil && a == b
		}
		lo, err1 := DiscountedPrice(final, c.Base, "percent", c.Percent)
		hi, err2 := DiscountedPrice(final, c.Base, "percent", c.Percent+1)
		if err1 != nil || err2 != nil {
			return false
		}
		return hi <= lo
	}, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestApplySellerDiscountsProperty(t *testing.T) {
	if err := quick.Check(func(c discountCase) bool {
		res := ReferenceResolution{
			BaseCost:     c.Base,
			FinalPrice:   c.Base + c.Margin,
			SellerMargin: c.Margin,
		}
		campaign := &DiscountCampaignInput{ID: "prop-camp", Mode: "percent", Value: c.Percent}
		code := &DiscountCodeInput{Code: "PROP", Mode: "fixed", Value: float64(c.Fixed)}
		out := ApplySellerDiscounts(res, campaign, code)
		final := out.Resolution.FinalPrice
		if final < c.Base || final > res.FinalPrice {
			t.Logf("%+v: composed final %d outside [base,original]", c, final)
			return false
		}
		if out.DiscountAmount != res.FinalPrice-final {
			t.Logf("%+v: DiscountAmount %d != original-final %d", c, out.DiscountAmount, res.FinalPrice-final)
			return false
		}
		if out.Resolution.SellerMargin != final-c.Base {
			t.Logf("%+v: SellerMargin %d != final-base %d", c, out.Resolution.SellerMargin, final-c.Base)
			return false
		}
		return true
	}, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

// Invalid discount inputs change nothing: the resolution is returned
// unchanged instead of inventing a price.
func TestDiscountInvalidInputPreservesPrice(t *testing.T) {
	res := ReferenceResolution{BaseCost: 100000, FinalPrice: 130000, SellerMargin: 30000}
	for _, tc := range []struct {
		mode  string
		value float64
	}{
		{"percent", -1}, {"percent", 101}, {"fixed", -100}, {"bogus", 10},
	} {
		out := ApplyCampaignDiscount(res, DiscountCampaignInput{Mode: tc.mode, Value: tc.value})
		if out.Resolution.FinalPrice != res.FinalPrice || out.DiscountAmount != 0 {
			t.Fatalf("mode=%s value=%v: price changed to %d (discount %d)", tc.mode, tc.value, out.Resolution.FinalPrice, out.DiscountAmount)
		}
	}
}
