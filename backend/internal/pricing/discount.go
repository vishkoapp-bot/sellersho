package pricing

// Dropshipper discounts on top of the live percent engine (Phase 3,
// Team Discounts). This file LAYERS on ResolveReferencePricing; it
// never reimplements it and never touches its core resolution: every
// function here takes a ReferenceResolution produced by that engine
// and only shrinks the dropshipper margin.
//
// Percent-pricing definitions (live-parity reference_pricing.go):
//
//	C = WholesaleCost (supplier base cost)
//	pct = platform percent ladder; R = RoundTo100(C * (1 + pct/100))
//	    = ReferenceCost = live baseCostB ("B" in the old fixed-fee
//	    vocabulary). B is therefore DEFINED here as R, the reference
//	    cost AFTER the platform percent, not as C + an absolute fee.
//	F = R - C (platform fee portion; 0 while platform_pct = 0)
//	P = final price; D = P - R (dropshipper margin)
//
// Current policy (owner decision 2026-11-06): campaigns and codes subtract
// a percentage of the CURRENT final price, or fixed integer toman. Campaigns
// apply before codes; each is capped at remaining seller margin. Wholesale,
// reference cost and platform fee remain unchanged. Round the discounted final
// price once to nearest100 (half up), then clamp to [BaseCost, original final].
// Older target-margin comments/tests predate that decision and are obsolete.
// Display MarginPercent is derived and never reused as an exact money input.

// DiscountCampaignInput is the minimal campaign data the pricing
// layer needs. The repository resolves scope priority and windows.
type DiscountCampaignInput struct {
	ID    string
	Scope string // 'variant' | 'product' | 'niche' | 'store'
	Name  string // campaign title shown on the storefront
	Mode  string // 'percent' | 'fixed'
	Value float64
}

// DiscountCodeInput is the minimal code data the pricing layer needs.
// The repository validates activity, window and usage limits.
type DiscountCodeInput struct {
	Code  string
	Mode  string // 'percent' | 'fixed'
	Value float64
}

// DiscountResult is a ReferenceResolution after discounting, plus
// the discount bookkeeping the order snapshot needs. Resolution
// carries the discounted FinalPrice/SellerMargin; DiscountAmount is
// the total margin given up (original final - discounted final).
type DiscountResult struct {
	Resolution     ReferenceResolution
	DiscountAmount int64
	CampaignID     string
	CampaignName   string
	Code           string
}

func discountedMarginPercent(res ReferenceResolution) float64 {
	if res.BaseCost <= 0 {
		return 0
	}
	return float64(res.SellerMargin) / float64(res.BaseCost) * 100.0
}

// ApplyCampaignDiscount subtracts current-price percent-off or fixed money.
func ApplyCampaignDiscount(res ReferenceResolution, c DiscountCampaignInput) DiscountResult {
	final, err := DiscountedPrice(res.FinalPrice, res.BaseCost, c.Mode, c.Value)
	if err != nil || res.PricingError != "" {
		return DiscountResult{Resolution: res}
	}
	out := discountedResolution(res, final)
	return DiscountResult{Resolution: out, DiscountAmount: res.FinalPrice - final, CampaignID: c.ID, CampaignName: c.Name}
}

func ApplyCodeDiscount(res ReferenceResolution, c DiscountCodeInput) DiscountResult {
	final, err := DiscountedPrice(res.FinalPrice, res.BaseCost, c.Mode, c.Value)
	if err != nil || res.PricingError != "" {
		return DiscountResult{Resolution: res}
	}
	out := discountedResolution(res, final)
	return DiscountResult{Resolution: out, DiscountAmount: res.FinalPrice - final, Code: c.Code}
}

func discountedResolution(res ReferenceResolution, final int64) ReferenceResolution {
	out := res
	out.FinalPrice = final
	out.SellerMargin = final - out.BaseCost
	out.MarginPercent = discountedMarginPercent(out)
	return out
}

// ApplySellerDiscounts applies a campaign first, then a code, in that
// order. Passing nil for either means that discount does not apply.
func ApplySellerDiscounts(res ReferenceResolution, campaign *DiscountCampaignInput, code *DiscountCodeInput) DiscountResult {
	total := DiscountResult{Resolution: res}
	if campaign != nil {
		total = ApplyCampaignDiscount(res, *campaign)
	}
	if code != nil {
		afterCode := ApplyCodeDiscount(total.Resolution, *code)
		afterCode.DiscountAmount += total.DiscountAmount
		afterCode.CampaignID = total.CampaignID
		total = afterCode
	}
	return total
}
