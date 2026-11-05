package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
)

// applyStoreDiscounts resolves the active campaign for one product
// and applies it, then the validated code, in campaign-then-code
// order, on top of a live percent-engine resolution (pricing.
// ResolveReferencePricing). Only dropshipper stores get discounts;
// central stores never do, so their prices are returned unchanged.
// A campaign-lookup error applies no campaign (fail-safe: the
// customer pays the undiscounted price rather than a discount that
// may bypass an admin lock the lookup could not read).
// A code obeys the same admin margin lock as a campaign: when
// the margin is locked (or the lock cannot be read) the code is
// dropped, so a locked margin cannot be eroded to zero by a code.
func applyStoreDiscounts(ctx context.Context, storeRepo *repository.StoreRepository, store *domain.Store, p *domain.Product, res pricing.ReferenceResolution, code *repository.DiscountCode) pricing.DiscountResult {
	if store == nil || p == nil || store.OwnerType != "dropshipper" {
		return pricing.DiscountResult{Resolution: res}
	}
	var campaign *pricing.DiscountCampaignInput
	if storeRepo != nil {
		if c, err := storeRepo.GetActiveDiscountCampaign(ctx, store.ID, p.ID, p.NicheIDs); err == nil && c != nil {
			campaign = &pricing.DiscountCampaignInput{ID: c.ID, Mode: c.Mode, Value: c.Value}
		}
	}
	var codeInput *pricing.DiscountCodeInput
	if code != nil && storeRepo != nil {
		// Codes obey the same admin margin lock as campaigns
		// (DiscountApplicationLocked mirrors
		// GetActiveDiscountCampaign): a locked margin is not eroded
		// by a code, and a lock-query error drops the code fail-safe,
		// exactly as a campaign-lookup error drops the campaign above.
		if locked, err := storeRepo.DiscountApplicationLocked(ctx, store.ID, p.ID, p.NicheIDs); err == nil && !locked {
			codeInput = &pricing.DiscountCodeInput{Code: code.Code, Mode: code.Mode, Value: code.Value}
		}
	}
	return pricing.ApplySellerDiscounts(res, campaign, codeInput)
}

// Public discount quote: same server-side math as order creation, but
// read-only. It never creates an order and never consumes a code use.
// An invalid/expired code returns discountCodeValid=false with the
// campaign-only totals, so checkout can show a clear error; placing
// the order with that code then fails with 400 (see
// handleCreateOrder), never silently charging the full price.
// Variant lines are priced with the same variant resolver order
// creation uses (ResolveReferencePricingForStoreProductVariantWithCost
// on the variant cost, with the SAME store's variant overrides), so a
// quoted variant line matches the charged variant line instead of
// falling back to the product-level price.
func handleDiscountQuote(w http.ResponseWriter, r *http.Request, catalogRepo *repository.CatalogRepository, storeRepo *repository.StoreRepository, pricingRepo *repository.PricingRepository, slug string) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if catalogRepo == nil || storeRepo == nil {
		http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
		return
	}
	var req struct {
		Items []struct {
			ProductID string `json:"productId"`
			VariantID string `json:"variantId,omitempty"`
			Quantity  int    `json:"quantity"`
		} `json:"items"`
		DiscountCode string `json:"discountCode"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || len(req.Items) == 0 {
		http.Error(w, `{"error":"Invalid quote request"}`, http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	store, err := storeRepo.GetStoreBySlug(ctx, slug)
	if err != nil || store == nil {
		// ISO-C: fail closed on unknown slugs - same rule as order
		// creation: an unknown slug must not silently price as central.
		// Empty/"central" still resolve to the central storefront.
		if strings.TrimSpace(slug) != "" && strings.TrimSpace(slug) != "central" {
			http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
			return
		}
		store, _ = storeRepo.GetStoreBySlug(ctx, "central")
	}
	if store == nil {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return
	}
	settings, _ := loadPricingSettings(ctx, pricingRepo)
	locks := loadStoreMarginLocks(ctx, pricingRepo, store.ID)
	// Same per-store variant overrides order creation loads: a variant
	// override of any other store must never price this store's quote.
	variantOverrides := loadVariantOverridesForStore(ctx, pricingRepo, store.ID)
	widgetCfg := loadDealsWidgetConfig(store)
	var code *repository.DiscountCode
	if req.DiscountCode != "" {
		if c, err := storeRepo.ValidateDiscountCode(ctx, store.ID, req.DiscountCode); err == nil {
			code = c
		}
	}
	var originalPayable, payable, sellerDiscount int64
	codeApplied := false
	for _, itemReq := range req.Items {
		if itemReq.Quantity <= 0 {
			continue
		}
		p, err := catalogRepo.GetProductByID(ctx, itemReq.ProductID)
		if err != nil || p == nil {
			http.Error(w, fmt.Sprintf(`{"error":"Product %s not found"}`, itemReq.ProductID), http.StatusBadRequest)
			return
		}
		// Same assortment gate as order creation (handleCreateOrder):
		// a dropshipper quote must never price a product the store
		// does not publish, so the quote stays a faithful predictor
		// of what checkout will accept.
		if store.OwnerType == "dropshipper" {
			active, activeErr := storeRepo.StoreProductActive(ctx, store.ID, p.ID)
			if activeErr != nil {
				http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
				return
			}
			if !active {
				http.Error(w, fmt.Sprintf(`{"error":"Product %s is not published by this store"}`, p.ID), http.StatusBadRequest)
				return
			}
		}
		var res pricing.ReferenceResolution
		if variantID := strings.TrimSpace(itemReq.VariantID); variantID != "" {
			// Same variant resolution as handleCreateOrder: the
			// variant must belong to this product, its own cost
			// wins when set, and the override comes from this
			// store's map only. An unknown variant fails the
			// quote, exactly as it fails the order, rather than
			// silently quoting the product-level price.
			variants, verr := catalogRepo.GetProductVariants(ctx, p.ID)
			if verr != nil {
				http.Error(w, `{"error":"Store catalog is unavailable"}`, http.StatusInternalServerError)
				return
			}
			var chosen *domain.ProductVariant
			for _, v := range variants {
				if v.ID == variantID {
					vv := v
					chosen = &vv
					break
				}
			}
			if chosen == nil {
				http.Error(w, `{"error":"Variant not found for product"}`, http.StatusBadRequest)
				return
			}
			cost := p.SupplierBaseCost
			if chosen.SupplierBaseCost != nil {
				cost = *chosen.SupplierBaseCost
			}
			var ov *pricing.StoreVariantOverride
			if o, ok := variantOverrides[p.ID+"/"+chosen.ID]; ok {
				oc := o
				ov = &oc
			}
			res = pricing.ResolveReferencePricingForStoreProductVariantWithCost(*p, cost, chosen.ID, *store, settings, locks, ov)
		} else {
			res = pricing.ResolveReferencePricing(*p, *store, settings, locks)
		}
		originalPayable += res.FinalPrice * int64(itemReq.Quantity)
		// Same first layer as order creation: the timed-deals
		// widget discount, its amount folded into the line's
		// discount amount so the quote's sellerDiscount matches
		// the order's.
		res, widgetDiscountAmount := applyDealsWidgetDiscount(store, widgetCfg, p.ID, res)
		d := applyStoreDiscounts(ctx, storeRepo, store, p, res, code)
		d.DiscountAmount += widgetDiscountAmount
		if d.Code != "" {
			codeApplied = true
		}
		payable += d.Resolution.FinalPrice * int64(itemReq.Quantity)
		sellerDiscount += d.DiscountAmount * int64(itemReq.Quantity)
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"payableAmount":     payable,
		"originalPayable":   originalPayable,
		"sellerDiscount":    sellerDiscount,
		"discountCode":      req.DiscountCode,
		"discountCodeValid": codeApplied,
	})
}
