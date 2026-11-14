package pricing

import "testing"

// Percent-pricing base case (adapted from the old branch's fixed-fee
// example; the old numbers assumed a fixed fee and MUST NOT be reused
// here). C = 800,000, platform_pct = 0 so R = B = 800,000 and F = 0,
// original margin D = 170,000, final P = 970,000. The old branch's
// base was C = 800,000, F = 50,000, B = 850,000, P = 1,020,000 —
// every assertion below differs from it on purpose.
func testPercentRes() ReferenceResolution {
	return ReferenceResolution{
		WholesaleCost: 800000,
		ReferenceCost: 800000,
		PlatformFee:   0,
		BaseCost:      800000,
		SellerMargin:  170000,
		FinalPrice:    970000,
		MarginPercent: 21.25,
	}
}

func TestCampaignDiscountOnlyFromMargin(t *testing.T) {
	r := ApplyCampaignDiscount(testPercentRes(), DiscountCampaignInput{ID: "c1", Mode: "percent", Value: 10})
	// Current policy: 10% off 970,000 = 97,000, within seller margin.
	if r.Resolution.WholesaleCost != 800000 || r.Resolution.PlatformFee != 0 || r.Resolution.BaseCost != 800000 {
		t.Fatalf("wholesale/fee/base changed: %+v", r.Resolution)
	}
	if r.Resolution.FinalPrice != 873000 || r.Resolution.SellerMargin != 73000 || r.DiscountAmount != 97000 {
		t.Fatalf("campaign percent: %+v", r)
	}
	if r.CampaignID != "c1" {
		t.Fatalf("campaign id = %q; want c1", r.CampaignID)
	}
}

func TestCampaignZeroFixedDiscountIsNoop(t *testing.T) {
	r := ApplyCampaignDiscount(testPercentRes(), DiscountCampaignInput{ID: "c1", Mode: "fixed", Value: 0})
	if r.Resolution.FinalPrice != 970000 || r.Resolution.SellerMargin != 170000 || r.DiscountAmount != 0 {
		t.Fatalf("campaign zero discount: %+v", r)
	}
}

func TestCampaignNeverRaisesPrice(t *testing.T) {
	// 30% off current price exceeds margin: cap at 170,000 and floor at R.
	r := ApplyCampaignDiscount(testPercentRes(), DiscountCampaignInput{ID: "c1", Mode: "percent", Value: 30})
	if r.Resolution.FinalPrice != 800000 || r.Resolution.SellerMargin != 0 || r.DiscountAmount != 170000 {
		t.Fatalf("campaign above margin: %+v", r)
	}
}

func TestCodeDiscountCappedAtMargin(t *testing.T) {
	r := ApplyCodeDiscount(testPercentRes(), DiscountCodeInput{Code: "SAVE50", Mode: "fixed", Value: 99999999})
	if r.Resolution.FinalPrice != 800000 || r.Resolution.SellerMargin != 0 || r.DiscountAmount != 170000 {
		t.Fatalf("code cap: %+v", r)
	}
}

func TestCampaignThenCodeStackingNeverBelowBase(t *testing.T) {
	r := ApplySellerDiscounts(testPercentRes(),
		&DiscountCampaignInput{ID: "c1", Mode: "fixed", Value: 50000},
		&DiscountCodeInput{Code: "SAVE", Mode: "fixed", Value: 999999})
	if r.Resolution.FinalPrice != 800000 || r.Resolution.SellerMargin != 0 {
		t.Fatalf("stacking floor: %+v", r)
	}
	if r.DiscountAmount != 170000 {
		t.Fatalf("stacking total discount: %+v", r)
	}
	if r.CampaignID != "c1" || r.Code != "SAVE" {
		t.Fatalf("stacking ids: campaign %q code %q", r.CampaignID, r.Code)
	}
}

func TestCodePercentOfFinalPrice(t *testing.T) {
	r := ApplyCodeDiscount(testPercentRes(), DiscountCodeInput{Code: "OFF10", Mode: "percent", Value: 10})
	// 10% of 970,000 = 97,000, within the 170,000 margin.
	if r.Resolution.FinalPrice != 873000 || r.Resolution.SellerMargin != 73000 || r.DiscountAmount != 97000 {
		t.Fatalf("code percent: %+v", r)
	}
}

func TestDiscountWithNonZeroPlatformPctFeeUntouched(t *testing.T) {
	// C = 800,000, platform_pct = 10% so R = 880,000 and F = 80,000;
	// margin 15% of R = 132,000, final = 1,012,000. An oversized fixed
	// discount must floor at R (880,000); the wholesale cost and fee
	// portion survive untouched.
	res := ReferenceResolution{
		WholesaleCost: 800000,
		ReferenceCost: 880000,
		PlatformFee:   80000,
		BaseCost:      880000,
		SellerMargin:  132000,
		FinalPrice:    1012000,
		MarginPercent: 15,
	}
	r := ApplyCampaignDiscount(res, DiscountCampaignInput{ID: "c1", Mode: "fixed", Value: 99999999})
	if r.Resolution.FinalPrice != 880000 || r.Resolution.SellerMargin != 0 || r.DiscountAmount != 132000 {
		t.Fatalf("non-zero pct campaign: %+v", r)
	}
	if r.Resolution.WholesaleCost != 800000 || r.Resolution.PlatformFee != 80000 || r.Resolution.BaseCost != 880000 {
		t.Fatalf("fee/cost changed under non-zero pct: %+v", r.Resolution)
	}
}

func TestCodeOnZeroMarginIsNoop(t *testing.T) {
	res := testPercentRes()
	res.SellerMargin = 0
	res.FinalPrice = 800000
	r := ApplyCodeDiscount(res, DiscountCodeInput{Code: "LATE", Mode: "fixed", Value: 10000})
	if r.Resolution.FinalPrice != 800000 || r.DiscountAmount != 0 || r.Code != "LATE" {
		t.Fatalf("code on zero margin: %+v", r)
	}
}

func TestInvalidDiscountModeIsNoop(t *testing.T) {
	r := ApplyCampaignDiscount(testPercentRes(), DiscountCampaignInput{ID: "c1", Mode: "bogus", Value: 10})
	if r.Resolution.FinalPrice != 970000 || r.DiscountAmount != 0 || r.CampaignID != "" {
		t.Fatalf("invalid campaign mode: %+v", r)
	}
}
