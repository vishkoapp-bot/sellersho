package main

import (
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/drup/platform/internal/catalogpricing"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

func (h *SupplierHandlers) HandleVariantWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	supplierID, ok := supplierLiveParitySupplierID(w, r, h)
	if !ok {
		return
	}
	_, canStock := supplierAccess(r, h.supplierRepo, "inventory.manage")
	_, canPrice := supplierAccess(r, h.supplierRepo, "products.manage")
	items, err := h.supplierRepo.ListWorkspaceVariants(r.Context(), supplierID, canStock, canPrice)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}

type sellerVariantWorkProduct struct {
	ProductID      string                   `json:"productId"`
	Title          string                   `json:"title"`
	NeedsAttention bool                     `json:"needsAttention"`
	Variants       []sellerWorkspaceVariant `json:"variants"`
}

type sellerWorkspaceVariant struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	SPNC          string   `json:"spnc"`
	Visible       bool     `json:"visible"`
	Active        bool     `json:"active"`
	Available     bool     `json:"available"`
	Price         *int64   `json:"price"`
	Effective     *float64 `json:"effectiveMargin"`
	Margin        *float64 `json:"margin"`
	MinimumMargin float64  `json:"minimumMargin"`
	MaximumMargin float64  `json:"maximumMargin"`
	MarginLocked  bool     `json:"marginLocked"`
}

func buildSellerWorkspaceProduct(p domain.Product, variants []domain.ProductVariant, store domain.Store,
	settings *pricing.PlatformPricingSettings, locks []pricing.MarginLock, overrides map[string]pricing.StoreVariantOverride,
	hidden map[string]bool, campaigns []domain.SellerDiscountCampaign, asOf time.Time) (sellerVariantWorkProduct, error) {
	out := sellerVariantWorkProduct{ProductID: p.ID, Title: p.Title, Variants: []sellerWorkspaceVariant{}}
	prices, err := catalogpricing.ResolveVariantsAt(catalogpricing.VariantInput{
		Product: p, Variants: variants, Store: store, Settings: settings, Locks: locks, Overrides: overrides,
		ActiveCampaigns: campaigns, Widget: loadDealsWidgetConfig(&store), AsOf: asOf,
	})
	if err != nil {
		return out, err
	}
	eligibleVariants := 0
	for _, variant := range variants {
		if !catalogpricing.DisplayWorkspaceVariant(p, variant, len(variants)) {
			continue
		}
		cost := p.SupplierBaseCost
		if variant.SupplierBaseCost != nil {
			cost = *variant.SupplierBaseCost
		}
		row := sellerWorkspaceVariant{
			ID: variant.ID, Name: catalogpricing.DisplayVariantName(p, variant, len(variants)), SPNC: variant.Spnc,
			Visible: !hidden[variant.ID], Active: p.IsActive && variant.IsActive,
			MinimumMargin: math.Max(0, settings.MinProfitMargin), MaximumMargin: pricing.EffectiveMarginCeiling(p.ID, settings),
		}
		var override *pricing.StoreVariantOverride
		if saved, ok := overrides[p.ID+"/"+variant.ID]; ok {
			override = &saved
			if saved.Mode == "percent" {
				row.Margin = &saved.Value
			}
		}
		resolved := pricing.ResolveReferencePricingForStoreProductVariantWithCost(p, cost, variant.ID, store, settings, locks, override)
		row.MarginLocked = resolved.AdminLocked
		if resolved.PricingError == "" {
			row.Effective = &resolved.MarginPercent
		}
		if price, ok := prices[variant.ID]; ok && price.PricingError == "" && price.Price > 0 {
			row.Price = &price.Price
			row.Available = catalogpricing.WorkspaceVariantPurchasable(p, variant)
		}
		if variant.IsActive {
			eligibleVariants++
			out.NeedsAttention = out.NeedsAttention || !row.Visible || !row.Available
		}
		out.Variants = append(out.Variants, row)
	}
	if eligibleVariants == 0 {
		if len(variants) > 0 || p.SchemaVersion == 2 || p.ProductType == "variable" {
			out.NeedsAttention = true
		} else {
			resolved := pricing.ResolveReferencePricing(p, store, settings, locks)
			out.NeedsAttention = !p.IsActive || p.Stock <= 0 || resolved.PricingError != "" || resolved.FinalPrice <= 0
		}
	}
	return out, nil
}
