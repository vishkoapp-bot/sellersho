package pricing

import (
	"encoding/json"
	"time"
)

// Timed-deals widget campaign discounts (owner feature, 2026-11-05).
//
// A dropshipper configures the storefront timed-deals widget
// (stores.seller_deals_widget_config) with a per-product DISCOUNT
// PERCENT. Unlike the percent-off-price campaigns in discount.go,
// a widget discount is expressed in margin
// PERCENTAGE POINTS and is carved out of the seller's own resolved
// margin:
//
//	effective margin % = resolved margin % - discount % (floored at 0)
//	discounted price   = RoundTo100(referenceCost * (1 + effective/100))
//
// so the price can fall to the reference cost R but never below it:
// wholesale cost C and the platform fee F are never touched, exactly
// like every other discount layer in this package. Confirmed example:
// R = 69,600,000 at a 35% margin prices at 93,960,000; a 10-point
// widget discount leaves a 25% margin and a price of 87,000,000.
//
// The discount applies only while the widget campaign is live and
// only to products listed in the widget's productIds. "Live" mirrors
// the storefront widget's own window rule (the TimedDealsSection in
// StoreSections.tsx): a parseable endsAt in the past ends the
// campaign; a missing or unparseable endsAt means no expiry. The
// save endpoint always stamps endsAt = save time + countdownHours,
// so any config written through the API carries a real deadline; the
// no-expiry branch is the frontend's legacy fallback, kept for
// parity so pricing and display never disagree.
//
// An admin-locked margin is never discounted: ApplyDealsWidgetDiscount
// refuses resolutions whose AdminLocked marker is set (product or
// variant admin_set locks and the legacy admin store lock) and, as a
// belt-and-braces check for resolutions built outside the resolvers,
// the "product-locked" tier.

// DealsWidgetConfig is the parsed shape of
// stores.seller_deals_widget_config relevant to pricing. Unknown
// fields in the stored JSON are ignored.
type DealsWidgetConfig struct {
	Title          string             `json:"title"`
	ProductIDs     []string           `json:"productIds"`
	CountdownHours int                `json:"countdownHours"`
	EndsAt         string             `json:"endsAt"`
	Discounts      map[string]float64 `json:"discounts,omitempty"`
}

// ParseDealsWidgetConfig parses a store's raw deals-widget config.
// It returns nil when the store has no config or the JSON cannot be
// parsed: a broken config must never break pricing or invent a
// discount.
func ParseDealsWidgetConfig(raw json.RawMessage) *DealsWidgetConfig {
	if len(raw) == 0 {
		return nil
	}
	var cfg DealsWidgetConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// IsLive reports whether the widget campaign is running at now,
// using the storefront widget's own window rule (see file header).
func (c *DealsWidgetConfig) IsLive(now time.Time) bool {
	if c == nil {
		return false
	}
	ends, err := time.Parse(time.RFC3339, c.EndsAt)
	if err != nil {
		return true // no parseable deadline = no expiry (frontend parity)
	}
	return now.Before(ends)
}

// DiscountFor returns the configured discount percent (margin
// percentage points) for one product, and whether it applies at now:
// the campaign must be live, the product must be listed in the
// widget's productIds, and it must carry a positive discounts entry.
func (c *DealsWidgetConfig) DiscountFor(productID string, now time.Time) (float64, bool) {
	if c == nil || len(c.Discounts) == 0 || !c.IsLive(now) {
		return 0, false
	}
	pct, ok := c.Discounts[productID]
	if _, err := ParsePercent(pct); !ok || err != nil || pct <= 0 || pct > 100 {
		return 0, false
	}
	for _, id := range c.ProductIDs {
		if id == productID {
			return pct, true
		}
	}
	return 0, false
}

// ApplyDealsWidgetDiscount carves discountPct margin percentage
// points out of a live percent-engine resolution (see file header).
// It returns the adjusted resolution and the discount amount given
// up (regular final price - discounted final price, >= 0).
//
// The resolution's OriginalPrice is deliberately NOT rewritten: the
// catalog-original semantics of that field feed order totals, and
// the existing discount layers leave it untouched too. Display
// callers that need a compare-at price set it from the pre-discount
// FinalPrice themselves (see cmd/server's display adapter).
//
// Locked resolutions (AdminLocked, or the "product-locked" tier) and
// resolutions with no margin to give up are returned unchanged with
// a zero amount.
func ApplyDealsWidgetDiscount(res ReferenceResolution, discountPct float64) (ReferenceResolution, int64) {
	if discountPct <= 0 || res.AdminLocked || res.ActiveTier == "product-locked" {
		return res, 0
	}
	if res.SellerMargin <= 0 || res.MarginPercent <= 0 {
		return res, 0
	}
	marginBps, err := ParsePercent(res.MarginPercent)
	discountBps, discountErr := ParsePercent(discountPct)
	if err != nil || discountErr != nil || res.PricingError != "" {
		return res, 0
	}
	effective := marginBps - discountBps
	if effective < 0 {
		effective = 0
	}
	final, err := markedUpPriceBPS(res.ReferenceCost, effective)
	if err != nil {
		return res, 0
	}
	if final < res.ReferenceCost {
		final = res.ReferenceCost
	}
	if final > res.FinalPrice {
		final = res.FinalPrice
	}
	out := res
	out.FinalPrice = final
	out.SellerMargin = final - res.ReferenceCost
	if out.SellerMargin < 0 {
		out.SellerMargin = 0
	}
	out.MarginPercent = discountedMarginPercent(out)
	amount := res.FinalPrice - final
	if amount < 0 {
		amount = 0
	}
	return out, amount
}
