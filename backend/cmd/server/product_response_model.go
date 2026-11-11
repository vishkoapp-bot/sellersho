package main

import "github.com/drup/platform/internal/domain"

func productTypeForResponse(p domain.Product) string {
	if p.ProductType == "" {
		return "legacy"
	}
	return p.ProductType
}

// Invoke only after the existing store/product relationship authorization gate.
func exposeScopedVariantCosts(response *ProductResponse, variants []domain.ProductVariant, fallback int64) {
	costs := map[string]int64{}
	for _, v := range variants {
		cost := fallback
		if v.SupplierBaseCost != nil {
			cost = *v.SupplierBaseCost
		}
		costs[v.ID] = cost
	}
	for i := range response.Variants {
		cost := costs[response.Variants[i].ID]
		response.Variants[i].SupplierBaseCost = &cost
	}
}
