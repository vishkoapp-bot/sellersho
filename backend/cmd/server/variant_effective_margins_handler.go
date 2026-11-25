package main

import (
	"net/http"
	"sort"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
	"github.com/drup/platform/internal/repository"
)

type sellerVariantEffectiveMargin struct {
	ProductID     string  `json:"productId"`
	VariantID     string  `json:"variantId"`
	MarginPct     float64 `json:"marginPct"`
	Tier          string  `json:"tier"`
	Locked        bool    `json:"locked"`
	CeilingPct    float64 `json:"ceilingPct"`
	ReferenceCost int64   `json:"referenceCost"`
	FinalPrice    int64   `json:"finalPrice"`
}

func handleSellerVariantEffectiveMargins(w http.ResponseWriter, r *http.Request, pricingRepo *repository.PricingRepository, storeRepo *repository.StoreRepository, catalogRepo *repository.CatalogRepository) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	unavailable := func() {
		http.Error(w, `{"error":"Variant pricing unavailable"}`, http.StatusInternalServerError)
	}
	if pricingRepo == nil || storeRepo == nil || catalogRepo == nil {
		unavailable()
		return
	}

	query := r.URL.Query()
	storeID := query.Get("storeId")
	workspace := query.Get("workspace")
	stores, err := storeRepo.GetStoresByOwner(ctx, claims.UserID)
	if err != nil {
		unavailable()
		return
	}
	var store *domain.Store
	for i := range stores {
		if storeID != "" && stores[i].ID == storeID {
			store = &stores[i]
			break
		}
	}
	if store == nil {
		http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
		return
	}

	// Keyset pagination by product ID. Include inactive assortment products
	// and store-hidden variants so the seller's pricing editor can show them.
	const pageSize = 50
	ids := store.ActiveProductIDs
	sort.Strings(ids)
	cursor := query.Get("cursor")
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > cursor })
	ids = ids[start:]
	page := struct {
		Items      []sellerVariantEffectiveMargin `json:"items"`
		NextCursor string                         `json:"nextCursor,omitempty"`
	}{Items: []sellerVariantEffectiveMargin{}}
	workPage := struct {
		WorkItems      []sellerVariantWorkProduct `json:"workItems,omitempty"`
		NextCursor     string                     `json:"nextCursor,omitempty"`
		AttentionCount *int                       `json:"attentionCount,omitempty"`
	}{}
	attentionCount := 0
	if workspace == "count" {
		workPage.AttentionCount = &attentionCount
	}
	if len(ids) > pageSize {
		ids = ids[:pageSize]
		page.NextCursor = ids[pageSize-1]
		workPage.NextCursor = page.NextCursor
	}
	if len(ids) == 0 {
		if workspace == "1" || workspace == "count" {
			writePricingJSON(w, workPage)
		} else {
			writePricingJSON(w, page)
		}
		return
	}

	products, err := catalogRepo.GetProductsByIDs(ctx, ids)
	if err != nil {
		unavailable()
		return
	}
	variants, err := catalogRepo.GetVariantsByProductIDs(ctx, ids)
	if err != nil {
		unavailable()
		return
	}
	settings, err := pricingRepo.GetPlatformPricingSettings(ctx)
	if err != nil {
		unavailable()
		return
	}
	locks, err := pricingRepo.ListStoreMarginLocks(ctx, store.ID)
	if err != nil {
		unavailable()
		return
	}
	overrides, err := pricingRepo.ListStoreVariantOverrides(ctx, store.ID)
	if err != nil {
		unavailable()
		return
	}
	type variantKey struct{ productID, variantID string }
	byVariant := make(map[variantKey]*pricing.StoreVariantOverride, len(overrides))
	for i := range overrides {
		o := &overrides[i]
		byVariant[variantKey{o.ProductID, o.VariantID}] = o
	}

	if workspace == "1" || workspace == "count" {
		hidden, err := storeRepo.HiddenWorkspaceVariants(ctx, store.ID, ids)
		if err != nil {
			unavailable()
			return
		}
		campaigns, err := storeRepo.ListActiveDiscountCampaignsForStore(ctx, store.ID)
		if err != nil {
			unavailable()
			return
		}
		workspaceOverrides := make(map[string]pricing.StoreVariantOverride, len(overrides))
		for _, override := range overrides {
			workspaceOverrides[override.ProductID+"/"+override.VariantID] = override
		}
		asOf := time.Now()
		for _, product := range products {
			row, err := buildSellerWorkspaceProduct(product, variants[product.ID], *store, settings, locks, workspaceOverrides, hidden, campaigns, asOf)
			if err != nil {
				unavailable()
				return
			}
			if workspace == "1" {
				workPage.WorkItems = append(workPage.WorkItems, row)
			} else if row.NeedsAttention {
				attentionCount++
			}
		}
		writePricingJSON(w, workPage)
		return
	}

	for _, product := range products {
		for _, variant := range variants[product.ID] {
			cost := product.SupplierBaseCost
			if variant.SupplierBaseCost != nil {
				cost = *variant.SupplierBaseCost
			}
			override := byVariant[variantKey{product.ID, variant.ID}]
			resolved := pricing.ResolveReferencePricingForStoreProductVariantWithCost(product, cost, variant.ID, *store, settings, locks, override)
			if resolved.PricingError != "" {
				continue
			}
			page.Items = append(page.Items, sellerVariantEffectiveMargin{
				ProductID: product.ID, VariantID: variant.ID, MarginPct: resolved.MarginPercent,
				Tier: resolved.ActiveTier, Locked: resolved.AdminLocked,
				CeilingPct:    pricing.EffectiveMarginCeiling(product.ID, settings),
				ReferenceCost: resolved.ReferenceCost, FinalPrice: resolved.FinalPrice,
			})
		}
	}
	writePricingJSON(w, page)
}
