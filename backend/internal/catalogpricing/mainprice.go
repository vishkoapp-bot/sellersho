// ResolveMainPrice computes the single displayed (card) price for one product
// from the same engine the list handler uses — never a second pricing
// implementation. It mirrors, in order:
//   - the product-level resolution (pricing.ResolveReferencePricing) with the
//     deals-widget discount and the winning display campaign applied exactly
//     as handleProductsList applies them;
//   - the per-variant prices (ResolveVariantsAt, same inputs);
//   - the card pick order from src/utils/cardPrice.ts: the store's default
//     variant, then the product isDefault variant, then the first active
//     priced variant; otherwise the product-level price.
//
// PriceMin/PriceMax mirror buildProductResponse: over active priced variants
// (post-sale prices); a zero-variant product reports the product price as
// both ends; a product with variants but no active priced variant reports
// no range. ValidUntil is the earliest upcoming time-windowed boundary
// affecting this product's displayed price (campaign start/end, widget
// endsAt, variant sale from/to), or nil when no boundary applies; the
// price index treats a row as stale once ValidUntil passes, so time alone
// can expire a cached price without any table row changing.
//
// AllCampaigns must already be restricted to campaigns that can affect this
// product (store scope, this product, or one of its niches); ActiveCampaigns
// is the in-window subset at AsOf. A variant whose resolution carries a
// pricing error is never the card price: the pick falls through to the
// product-level price, exactly as a card with no priced variant renders.
package catalogpricing

import (
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

// MainPriceInput is trusted source data, not a public payload. See
// VariantInput: settings and AsOf are required, campaigns must come from
// the caller's consistent snapshot.
type MainPriceInput struct {
	Product               domain.Product
	Variants              []domain.ProductVariant // repo order: default first, then created_at, id
	Store                 domain.Store
	Settings              *pricing.PlatformPricingSettings
	Locks                 []pricing.MarginLock
	Overrides             map[string]pricing.StoreVariantOverride
	ActiveCampaigns       []domain.SellerDiscountCampaign
	AllCampaigns          []domain.SellerDiscountCampaign
	Widget                *pricing.DealsWidgetConfig
	StoreDefaultVariantID string
	AsOf                  time.Time
}

// MainPrice carries public price data only, never reference/supplier costs.
type MainPrice struct {
	Price           int64
	OriginalPrice   int64
	DiscountPercent int
	PriceMin        *int64
	PriceMax        *int64
	ValidUntil      *time.Time
}

func ResolveMainPrice(input MainPriceInput) (MainPrice, error) {
	if input.Settings == nil || input.AsOf.IsZero() {
		return MainPrice{}, ErrSourceRequired
	}
	res := pricing.ResolveReferencePricing(input.Product, input.Store, input.Settings, input.Locks)
	if res.PricingError != "" {
		return MainPrice{}, ErrSourceRequired
	}
	res = ApplyWidgetForDisplayAt(&input.Store, input.Widget, input.Product.ID, res, input.AsOf)
	if campaign := PickCampaign(input.ActiveCampaigns, input.Product, ""); campaign != nil && !CampaignLocked(input.Store, input.Locks, input.Product, "") {
		res = ApplyCampaign(res, campaign).Resolution
	}
	out := MainPrice{Price: res.FinalPrice, OriginalPrice: res.OriginalPrice}

	variantPrices, err := ResolveVariantsAt(VariantInput{
		Product: input.Product, Variants: input.Variants, Store: input.Store,
		Settings: input.Settings, Locks: input.Locks, Overrides: input.Overrides,
		ActiveCampaigns: input.ActiveCampaigns, Widget: input.Widget, AsOf: input.AsOf,
	})
	if err != nil {
		return MainPrice{}, err
	}
	priced := func(v domain.ProductVariant) (VariantPrice, bool) {
		vp, ok := variantPrices[v.ID]
		if !ok || vp.PricingError != "" {
			return VariantPrice{}, false
		}
		return vp, true
	}
	// Rendered-active mirrors buildProductResponse: a variant with no
	// inventory mode is legacy and counts as active regardless of the
	// stored flag; otherwise IsActive decides. cardPrice.ts applies the
	// same rule to the DTO (`isActive !== false` after this mapping).
	active := make([]domain.ProductVariant, 0, len(input.Variants))
	for _, v := range input.Variants {
		if v.InventoryMode == "" || v.IsActive {
			active = append(active, v)
		}
	}
	var picked *VariantPrice
	if input.StoreDefaultVariantID != "" {
		for _, v := range active {
			if v.ID == input.StoreDefaultVariantID {
				if vp, ok := priced(v); ok {
					picked = &vp
				}
				break
			}
		}
	}
	if picked == nil {
		for _, v := range active {
			if v.IsDefault {
				if vp, ok := priced(v); ok {
					picked = &vp
					break
				}
			}
		}
	}
	if picked == nil {
		for _, v := range active {
			if vp, ok := priced(v); ok {
				picked = &vp
				break
			}
		}
	}
	if picked != nil {
		out.Price = picked.Price
		out.OriginalPrice = picked.OriginalPrice
	}
	out.DiscountPercent = int(pricing.DiscountPercent(out.OriginalPrice, out.Price))

	var priceMin, priceMax *int64
	for _, v := range active {
		vp, ok := variantPrices[v.ID]
		if !ok || vp.Price <= 0 {
			continue
		}
		p := vp.Price
		if priceMin == nil || p < *priceMin {
			priceMin = &p
		}
		if priceMax == nil || p > *priceMax {
			priceMax = &p
		}
	}
	if priceMin == nil && len(input.Variants) == 0 {
		v := res.FinalPrice
		priceMin, priceMax = &v, &v
	}
	out.PriceMin, out.PriceMax = priceMin, priceMax

	out.ValidUntil = earliestBoundary(input, input.AsOf)
	return out, nil
}

// earliestBoundary returns the nearest future instant at which the
// displayed price for this product can change without any row changing:
// a campaign entering or leaving its window, the widget deadline, or a
// variant sale window opening or closing. Campaigns outside this
// product's scope must not be passed in (see MainPriceInput).
func earliestBoundary(input MainPriceInput, asOf time.Time) *time.Time {
	var best *time.Time
	consider := func(t time.Time) {
		if t.After(asOf) && (best == nil || t.Before(*best)) {
			c := t
			best = &c
		}
	}
	for _, c := range input.AllCampaigns {
		consider(c.StartsAt)
		consider(c.EndsAt)
	}
	if input.Widget != nil && input.Widget.EndsAt != "" {
		if ends, err := time.Parse(time.RFC3339, input.Widget.EndsAt); err == nil {
			consider(ends)
		}
	}
	for _, v := range input.Variants {
		if v.SaleFrom != nil {
			consider(*v.SaleFrom)
		}
		if v.SaleTo != nil {
			consider(*v.SaleTo)
		}
	}
	return best
}
