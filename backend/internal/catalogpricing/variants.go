// Package catalogpricing shares display-price composition with callers that
// build a transaction-consistent catalog price index. It has no DB or wall clock.
package catalogpricing

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

// VariantPrice contains public price data only, never reference/supplier costs.
type VariantPrice struct {
	PricingError    string     `json:"pricingError,omitempty"`
	Price           int64      `json:"price"`
	OriginalPrice   int64      `json:"originalPrice"`
	DiscountPercent int        `json:"discountPercent"`
	CampaignName    string     `json:"campaignName,omitempty"`
	SalePrice       *int64     `json:"salePrice,omitempty"`
	SaleFrom        *time.Time `json:"saleFrom,omitempty"`
	SaleTo          *time.Time `json:"saleTo,omitempty"`
	OnSale          bool       `json:"onSale,omitempty"`
}

// MarshalJSON preserves authoritative negative sale decisions, while retaining
// the legacy shape for products that have no configured sale.
func (price VariantPrice) MarshalJSON() ([]byte, error) {
	type wirePrice VariantPrice
	if price.SalePrice == nil || price.OnSale {
		return json.Marshal(wirePrice(price))
	}
	return json.Marshal(struct {
		wirePrice
		OnSale bool `json:"onSale"`
	}{wirePrice: wirePrice(price), OnSale: false})
}

// VariantInput is trusted source data, not a public payload. ActiveCampaigns
// must already be selected for AsOf from the caller's consistent source snapshot.
// This primitive cannot turn a failed loader/default into authoritative data.
type VariantInput struct {
	Product         domain.Product
	Variants        []domain.ProductVariant
	Store           domain.Store
	Settings        *pricing.PlatformPricingSettings
	Locks           []pricing.MarginLock
	Overrides       map[string]pricing.StoreVariantOverride
	ActiveCampaigns []domain.SellerDiscountCampaign
	Widget          *pricing.DealsWidgetConfig
	AsOf            time.Time
}

var ErrSourceRequired = errors.New("catalog pricing requires explicit source settings and asOf")

// ResolveVariantsAt composes the existing reference/widget/campaign/sale engine
// with one explicit timestamp for every SKU. It does not read config or select
// active campaigns; those are transaction-snapshot responsibilities of callers.
func ResolveVariantsAt(input VariantInput) (map[string]VariantPrice, error) {
	if input.Settings == nil || input.AsOf.IsZero() {
		return nil, ErrSourceRequired
	}
	out := make(map[string]VariantPrice, len(input.Variants))
	for _, v := range input.Variants {
		if v.InventoryMode == "tracked" && !v.IsActive {
			continue
		}
		cost := input.Product.SupplierBaseCost
		if v.SupplierBaseCost != nil {
			cost = *v.SupplierBaseCost
		}
		var override *pricing.StoreVariantOverride
		if value, ok := input.Overrides[input.Product.ID+"/"+v.ID]; ok {
			copyValue := value
			override = &copyValue
		}
		res := pricing.ResolveReferencePricingForStoreProductVariantWithCost(input.Product, cost, v.ID, input.Store, input.Settings, input.Locks, override)
		res = ApplyWidgetForDisplayAt(&input.Store, input.Widget, input.Product.ID, res, input.AsOf)
		name := ""
		if campaign := PickCampaign(input.ActiveCampaigns, input.Product, v.ID); campaign != nil && !CampaignLocked(input.Store, input.Locks, input.Product, v.ID) {
			discount := ApplyCampaign(res, campaign)
			res = discount.Resolution
			name = discount.CampaignName
		}
		onSale := false
		if v.SalePrice != nil {
			res, onSale = pricing.ApplyVariantSale(res, v.SalePrice, input.AsOf, v.SaleFrom, v.SaleTo)
			if onSale {
				name = ""
			}
		}
		out[v.ID] = VariantPrice{PricingError: res.PricingError, Price: res.FinalPrice, OriginalPrice: res.OriginalPrice, DiscountPercent: int(pricing.DiscountPercent(res.OriginalPrice, res.FinalPrice)), CampaignName: name, SalePrice: v.SalePrice, SaleFrom: v.SaleFrom, SaleTo: v.SaleTo, OnSale: onSale}
	}
	return out, nil
}
