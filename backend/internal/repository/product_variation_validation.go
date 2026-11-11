package repository

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Legacy proposals retain their existing contract; typed proposals are atomic.
func ValidateProductVariationProposal(data map[string]interface{}) error {
	if _, exists := data["schemaVersion"]; exists {
		return validateProductV2(data)
	}
	if _, exists := data["productSchema"]; exists {
		return fmt.Errorf("product schema version is required")
	}
	kind, _ := data["productType"].(string)
	if kind == "" {
		return nil
	}
	if kind != "simple" && kind != "variable" {
		return fmt.Errorf("invalid product type")
	}
	raw, ok := data["variants"].([]interface{})
	if !ok || len(raw) == 0 || len(raw) > 24 {
		return fmt.Errorf("product variants are required")
	}
	if kind == "simple" && len(raw) != 1 {
		return fmt.Errorf("simple product requires exactly one SKU")
	}
	seen := map[string]bool{}
	total := 0.
	minCost := math.MaxFloat64
	for _, entry := range raw {
		v, ok := entry.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid variant")
		}
		attrs, _ := v["attributes"].(map[string]interface{})
		keys := []string{}
		for k, value := range attrs {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(text) == "" {
				return fmt.Errorf("invalid variant attributes")
			}
			keys = append(keys, k)
		}
		if kind == "variable" && len(keys) == 0 {
			return fmt.Errorf("variable SKU needs attributes")
		}
		if kind == "simple" && len(keys) != 0 {
			return fmt.Errorf("simple product cannot have variable attributes")
		}
		sort.Strings(keys)
		parts := []string{}
		for _, k := range keys {
			parts = append(parts, k+": "+attrs[k].(string))
		}
		signature := strings.Join(parts, " / ")
		if seen[signature] {
			return fmt.Errorf("duplicate variant combination")
		}
		seen[signature] = true
		if kind == "variable" {
			v["variant_name"] = signature
		}
		cost, cok := v["supplier_base_cost"].(float64)
		stock, sok := v["stock"].(float64)
		if !cok || cost <= 0 || cost > 1e12 || cost != math.Trunc(cost) || !sok || stock < 0 || stock > 1e9 || stock != math.Trunc(stock) {
			return fmt.Errorf("invalid SKU price or stock")
		}
		total += stock
		minCost = math.Min(minCost, cost)
	}
	data["stock"] = total
	data["supplierBaseCost"] = minCost
	return nil
}
