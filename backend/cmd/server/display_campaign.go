package main

import (
	"context"

	"github.com/drup/platform/internal/catalogpricing"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
)

// Display-path helpers for seller discount campaigns (option A,
// 2026-11-06): the storefront must SHOW the campaign price, not only
// charge it at checkout. These helpers load a store's active
// campaigns once per request and resolve the winning campaign in
// memory, mirroring GetActiveDiscountCampaign exactly — same
// specificity order (variant > product > niche > store, most recent
// first), same admin margin lock semantics (legacy store boolean +
// store_margin_locks rows), same fail-safe: any doubt means the
// undiscounted price is displayed.

// loadActiveDiscountCampaignsForDisplay loads the store's active,
// in-window discount campaigns once per request. It returns nil for
// anything but a non-central dropshipper store — central stores
// never get discounts, matching applyStoreDiscounts.
func loadActiveDiscountCampaignsForDisplay(ctx context.Context, storeRepo *repository.StoreRepository, store *domain.Store) []domain.SellerDiscountCampaign {
	if storeRepo == nil || store == nil || store.Slug == "central" || store.OwnerType != "dropshipper" {
		return nil
	}
	campaigns, err := storeRepo.ListActiveDiscountCampaignsForStore(ctx, store.ID)
	if err != nil {
		return nil // fail-safe: display the undiscounted price
	}
	return campaigns
}

// displayCampaignLocked mirrors the admin margin lock semantics of
// GetActiveDiscountCampaign for the display path: the legacy store
// boolean, or a store_margin_locks row covering the priced line — a
// variant-scoped campaign is blocked by a lock on the variant, the
// product or any of its niches; product/niche/store-scoped campaigns
// by a lock on the product or any niche.
func displayCampaignLocked(store domain.Store, locks []pricing.MarginLock, p domain.Product, variantID string) bool {
	return catalogpricing.CampaignLocked(store, locks, p, variantID)
}

// pickDisplayCampaign resolves the winning campaign for one priced
// line in memory: variant (only when variantID is set) > product >
// niche (in the product's niche order) > store, then the most
// recently created. It mirrors the SQL ordering of
// GetActiveDiscountCampaign so the displayed price and the charged
// price always come from the same campaign.
func pickDisplayCampaign(campaigns []domain.SellerDiscountCampaign, p domain.Product, variantID string) *domain.SellerDiscountCampaign {
	return catalogpricing.PickCampaign(campaigns, p, variantID)
}

// applyDisplayCampaign applies one campaign to a display resolution.
// The math is the exact checkout math (pricing.ApplyCampaignDiscount:
// absolute target margin on the line's base cost, floor at the
// reference cost, rounded to 100, never raising the price), so the
// displayed price IS the charged price.
func applyDisplayCampaign(res pricing.ReferenceResolution, c *domain.SellerDiscountCampaign) pricing.DiscountResult {
	return catalogpricing.ApplyCampaign(res, c)
}
