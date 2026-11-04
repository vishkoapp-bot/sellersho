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
// User rules (non-negotiable, 2026-11-03):
//
//   - A discount comes ONLY out of the dropshipper's own margin D.
//     Wholesale cost C and platform fee F are never touched: this
//     file copies WholesaleCost, PlatformFee, ReferenceCost and
//     BaseCost unchanged and only lowers FinalPrice/SellerMargin.
//   - Margin floor is zero: the final price never goes below R
//     (= C + F). With pct = 0 that floor is C itself.
//   - Campaign: scope product > niche > store, most recent wins,
//     admin margin lock blocks (enforced by the repository lookup,
//     not here). A campaign sets an ABSOLUTE TARGET MARGIN: mode
//     'percent' means that percent of base cost B (= R), mode
//     'fixed' means that many Tomans. The campaign REPLACES the
//     normal margin.
//   - Code: percent of the (possibly campaign-discounted) final
//     price, or a fixed Toman amount; capped at the remaining
//     margin. Stacking order is campaign first, then code, so the
//     total discount can never exceed the original margin.
//
// Adaptation notes vs the old feature/dropshipper-discounts branch
// (which assumed price = C + fixed fee + margin, B = C + F):
//
//   - The campaign-percent formula is unchanged in form
//     (target = B * value / 100) because B is now R by definition.
//     What changed is what B means, not the arithmetic shape.
//   - The old branch's code discount subtracted an unrounded amount
//     from the final price; here the discounted final price is
//     rounded with RoundTo100 like every other live price, then
//     clamped to the [R, original final] interval, so a discounted
//     price is always a multiple of 100 and never below the floor.
//   - The old branch let a campaign whose target margin EXCEEDED the
//     current margin RAISE the price (DiscountAmount was floored at
//     0 but FinalPrice still rose). A "discount" must never raise
//     the price, so the target margin is clamped at the original
//     margin here: such a campaign is a no-op, not a price hike.
//   - Settlement pays the reduced profit because the order handler
//     snapshots the discounted SellerMargin/FinalUnitPrice plus the
//     per-item and per-order discount amounts (see order_handlers.go
//     and the seller_discount_codes migration).
//
// The returned MarginPercent is recomputed from the discounted
// margin (SellerMargin / BaseCost * 100) so the resolution stays
// self-consistent after discounting.

// DiscountCampaignInput is the minimal campaign data the pricing
// layer needs. The repository resolves scope priority and windows.
type DiscountCampaignInput struct {
	ID    string
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
	Code           string
}

func discountedMarginPercent(res ReferenceResolution) float64 {
	if res.BaseCost <= 0 {
		return 0
	}
	return float64(res.SellerMargin) / float64(res.BaseCost) * 100.0
}

// ApplyCampaignDiscount replaces the resolution's margin with the
// campaign's absolute target margin, clamped to [0, original margin].
func ApplyCampaignDiscount(res ReferenceResolution, c DiscountCampaignInput) DiscountResult {
	if c.Mode != "percent" && c.Mode != "fixed" {
		return DiscountResult{Resolution: res}
	}
	var targetMargin float64
	if c.Mode == "percent" {
		// Percent of base cost B, where B = R (reference cost after
		// the platform percent) in percent-pricing terms.
		targetMargin = float64(res.BaseCost) * c.Value / 100.0
	} else {
		targetMargin = c.Value
	}
	if targetMargin < 0 {
		targetMargin = 0
	}
	if targetMargin > float64(res.SellerMargin) {
		// A discount never raises the price (see header note).
		targetMargin = float64(res.SellerMargin)
	}
	originalFinal := res.FinalPrice
	final := RoundTo100(float64(res.BaseCost) + targetMargin)
	if final < res.BaseCost {
		final = res.BaseCost
	}
	if final > originalFinal {
		final = originalFinal
	}
	out := res
	out.FinalPrice = final
	out.SellerMargin = final - out.BaseCost
	if out.SellerMargin < 0 {
		out.SellerMargin = 0
	}
	out.MarginPercent = discountedMarginPercent(out)
	discount := originalFinal - final
	if discount < 0 {
		discount = 0
	}
	return DiscountResult{Resolution: out, DiscountAmount: discount, CampaignID: c.ID}
}

// ApplyCodeDiscount subtracts a code discount from the final price,
// capped at the remaining seller margin. The discounted price is
// rounded to a multiple of 100 and floored at the base cost R.
func ApplyCodeDiscount(res ReferenceResolution, c DiscountCodeInput) DiscountResult {
	if c.Mode != "percent" && c.Mode != "fixed" {
		return DiscountResult{Resolution: res}
	}
	if res.SellerMargin <= 0 {
		// No margin left to discount from; the code was still
		// validated, so it is recorded even though it gives 0.
		return DiscountResult{Resolution: res, Code: c.Code}
	}
	var discount float64
	if c.Mode == "percent" {
		discount = float64(res.FinalPrice) * c.Value / 100.0
	} else {
		discount = c.Value
	}
	if discount > float64(res.SellerMargin) {
		discount = float64(res.SellerMargin)
	}
	if discount < 0 {
		discount = 0
	}
	originalFinal := res.FinalPrice
	final := RoundTo100(float64(originalFinal) - discount)
	if final < res.BaseCost {
		final = res.BaseCost
	}
	if final > originalFinal {
		final = originalFinal
	}
	out := res
	out.FinalPrice = final
	out.SellerMargin = final - out.BaseCost
	if out.SellerMargin < 0 {
		out.SellerMargin = 0
	}
	out.MarginPercent = discountedMarginPercent(out)
	actual := originalFinal - final
	if actual < 0 {
		actual = 0
	}
	return DiscountResult{Resolution: out, DiscountAmount: actual, Code: c.Code}
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
