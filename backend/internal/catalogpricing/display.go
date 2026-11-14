package catalogpricing

import (
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

func ApplyWidgetAt(store *domain.Store, cfg *pricing.DealsWidgetConfig, productID string, res pricing.ReferenceResolution, asOf time.Time) (pricing.ReferenceResolution, int64) {
	if store == nil || cfg == nil || store.OwnerType != "dropshipper" {
		return res, 0
	}
	pct, ok := cfg.DiscountFor(productID, asOf)
	if !ok {
		return res, 0
	}
	return pricing.ApplyDealsWidgetDiscount(res, pct)
}

func ApplyWidgetForDisplayAt(store *domain.Store, cfg *pricing.DealsWidgetConfig, productID string, res pricing.ReferenceResolution, asOf time.Time) pricing.ReferenceResolution {
	out, amount := ApplyWidgetAt(store, cfg, productID, res, asOf)
	if amount > 0 {
		out.OriginalPrice = res.FinalPrice
	}
	return out
}

func CampaignLocked(store domain.Store, locks []pricing.MarginLock, p domain.Product, variantID string) bool {
	if store.IsMarginLockedByAdmin {
		return true
	}
	locked := func(scope, id string) bool {
		for _, lock := range locks {
			if lock.Scope == scope && lock.ScopeID == id {
				return true
			}
		}
		return false
	}
	if variantID != "" && locked("variant", variantID) {
		return true
	}
	if locked("product", p.ID) {
		return true
	}
	for _, id := range p.NicheIDs {
		if locked("niche", id) {
			return true
		}
	}
	return false
}

// PickCampaign preserves established specificity/niche order and creation-time
// precedence over an already active list; it never loads/guesses a window.
func PickCampaign(active []domain.SellerDiscountCampaign, p domain.Product, variantID string) *domain.SellerDiscountCampaign {
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
			for i, id := range p.NicheIDs {
				if c.TargetID == id {
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
	for i := range active {
		c := active[i]
		r, ok := rank(c)
		if !ok {
			continue
		}
		if best == nil || r < bestRank || (r == bestRank && c.CreatedAt.After(best.CreatedAt)) {
			best = &active[i]
			bestRank = r
		}
	}
	return best
}

func ApplyCampaign(res pricing.ReferenceResolution, c *domain.SellerDiscountCampaign) pricing.DiscountResult {
	if c == nil {
		return pricing.DiscountResult{Resolution: res}
	}
	return pricing.ApplyCampaignDiscount(res, pricing.DiscountCampaignInput{ID: c.ID, Scope: c.Scope, Name: c.Name, Mode: c.Mode, Value: c.Value})
}
