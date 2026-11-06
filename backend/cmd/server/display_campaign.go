package main

import (
	"context"

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
	if store.IsMarginLockedByAdmin {
		return true
	}
	lockedBy := func(scope, scopeID string) bool {
		for _, l := range locks {
			if l.Scope == scope && l.ScopeID == scopeID {
				return true
			}
		}
		return false
	}
	if variantID != "" && lockedBy("variant", variantID) {
		return true
	}
	if lockedBy("product", p.ID) {
		return true
	}
	for _, n := range p.NicheIDs {
		if lockedBy("niche", n) {
			return true
		}
	}
	return false
}

// pickDisplayCampaign resolves the winning campaign for one priced
// line in memory: variant (only when variantID is set) > product >
// niche (in the product's niche order) > store, then the most
// recently created. It mirrors the SQL ordering of
// GetActiveDiscountCampaign so the displayed price and the charged
// price always come from the same campaign.
func pickDisplayCampaign(campaigns []domain.SellerDiscountCampaign, p domain.Product, variantID string) *domain.SellerDiscountCampaign {
	if len(campaigns) == 0 {
		return nil
	}
	rank := func(c domain.SellerDiscountCampaign) (int, bool) {
		switch c.Scope {
		case "variant":
			if variantID != "" && c.TargetID == variantID {
				return 0, true
			}
		case "product":
			if c.TargetID == p.ID {
				return 1, true
			}
		case "niche":
			for i, n := range p.NicheIDs {
				if c.TargetID == n {
					return 2 + i, true
				}
			}
		case "store":
			return 1000000, true
		}
		return 0, false
	}
	var best *domain.SellerDiscountCampaign
	bestRank := 0
	for i := range campaigns {
		c := campaigns[i]
		r, ok := rank(c)
		if !ok {
			continue
		}
		if best == nil || r < bestRank || (r == bestRank && c.CreatedAt.After(best.CreatedAt)) {
			best = &campaigns[i]
			bestRank = r
		}
	}
	return best
}

// applyDisplayCampaign applies one campaign to a display resolution.
// The math is the exact checkout math (pricing.ApplyCampaignDiscount:
// absolute target margin on the line's base cost, floor at the
// reference cost, rounded to 100, never raising the price), so the
// displayed price IS the charged price.
func applyDisplayCampaign(res pricing.ReferenceResolution, c *domain.SellerDiscountCampaign) pricing.DiscountResult {
	if c == nil {
		return pricing.DiscountResult{Resolution: res}
	}
	return pricing.ApplyCampaignDiscount(res, pricing.DiscountCampaignInput{ID: c.ID, Scope: c.Scope, Name: c.Name, Mode: c.Mode, Value: c.Value})
}
