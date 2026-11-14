package pricing

import "time"

// Per-variant sale price (P1(c)). The sale REPLACES the resolved final
// price as an absolute Toman ceiling; it never stacks with campaigns
// or codes: the caller applies campaigns first, then this helper takes
// min(final_after_campaigns, S). The §0 invariant holds everywhere:
// no discount mechanism may push a final price below R (BaseCost).
//
// Window semantics: [from, to) — start inclusive, end exclusive; a
// null from means "already started", a null to means "no end".
//
//   - inactive when salePrice == nil, or now < from (from set), or
//     now >= to (to set).
//   - active: final = min(res.FinalPrice, *salePrice); if *salePrice <
//     res.BaseCost (R) the sale is SUSPENDED (cost rose after the sale
//     was set) -> return res unchanged, onSale=false. It is never
//     clamped up to R: a suspended sale must not silently reprice.
//   - else res.FinalPrice = final; res.SellerMargin = final -
//     res.BaseCost (clamped >= 0); res.MarginPercent recomputed the
//     same way ApplyCampaignDiscount does; res.OriginalPrice UNCHANGED
//     (the storefront shows the true markdown).
//   - never raises the price: if *salePrice >= res.FinalPrice,
//     onSale=false (no-op).
//
// Display (priceVariantsForResponse) and charge (order creation +
// discount quote) MUST both call this helper with the same inputs, in
// the same order (after campaigns), or display != charge.
func ApplyVariantSale(res ReferenceResolution, salePrice *int64, now time.Time, from, to *time.Time) (ReferenceResolution, bool) {
	if salePrice == nil || *salePrice <= 0 || *salePrice > MaxTransportMoney || res.PricingError != "" {
		return res, false
	}
	if from != nil && now.Before(*from) {
		return res, false
	}
	if to != nil && !now.Before(*to) {
		return res, false
	}
	if *salePrice < res.BaseCost {
		// Suspended, not clamped: the sale was set when R was lower.
		return res, false
	}
	if *salePrice >= res.FinalPrice {
		return res, false
	}
	out := res
	out.FinalPrice = *salePrice
	out.SellerMargin = out.FinalPrice - out.BaseCost
	if out.SellerMargin < 0 {
		out.SellerMargin = 0
	}
	out.MarginPercent = discountedMarginPercent(out)
	return out, true
}
