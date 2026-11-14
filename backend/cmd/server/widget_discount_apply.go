package main

import (
	"time"

	"github.com/drup/platform/internal/catalogpricing"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

// Timed-deals widget discounts: the wiring between a store's
// sellerDealsWidgetConfig and the pricing package's
// ApplyDealsWidgetDiscount. The widget discount is the FIRST discount
// layer everywhere it applies — it is the price the storefront
// advertises — and the DB campaign/code layer (applyStoreDiscounts)
// composes on top of it in the order and quote paths, exactly as it
// composes on an undiscounted resolution today.
//
// Only dropshipper stores get widget discounts (the discount is
// carved out of the seller's own margin), mirroring
// applyStoreDiscounts' gate. A store's config is parsed once per
// request and reused for every line.

// loadDealsWidgetConfig parses the store's raw widget config, or
// returns nil when the store carries none (or it does not parse).
func loadDealsWidgetConfig(store *domain.Store) *pricing.DealsWidgetConfig {
	if store == nil {
		return nil
	}
	return pricing.ParseDealsWidgetConfig(store.SellerDealsWidgetConfig)
}

// applyDealsWidgetDiscount applies the widget discount configured
// for productID to one resolution and returns the adjusted
// resolution plus the discount amount given up (0 when no discount
// applies). The resolution's OriginalPrice is left untouched, so
// order totals keep their catalog-original semantics, exactly as
// with the DB discount layers.
func applyDealsWidgetDiscount(store *domain.Store, cfg *pricing.DealsWidgetConfig, productID string, res pricing.ReferenceResolution) (pricing.ReferenceResolution, int64) {
	return catalogpricing.ApplyWidgetAt(store, cfg, productID, res, time.Now())
}

// applyDealsWidgetDiscountForDisplay is the storefront variant: when
// a widget discount applies, the DTO's compare-at price
// (originalPrice) becomes the pre-discount REGULAR price, so the
// storefront strikes it through next to the campaign price — even
// when the catalog OriginalPrice sits elsewhere. When no discount
// applies, the resolution is returned exactly as resolved.
func applyDealsWidgetDiscountForDisplay(store *domain.Store, cfg *pricing.DealsWidgetConfig, productID string, res pricing.ReferenceResolution) pricing.ReferenceResolution {
	return catalogpricing.ApplyWidgetForDisplayAt(store, cfg, productID, res, time.Now())
}
