package main

import "fmt"

func aggregateOrderQuantities(items []CreateOrderItemReq) (map[string]int, map[string]int, error) {
	products, skus := map[string]int{}, map[string]int{}
	maxInt := int(^uint(0) >> 1)
	for _, it := range items {
		key := it.ProductID + "/" + it.VariantID
		if it.Quantity <= 0 || products[it.ProductID] > maxInt-it.Quantity || skus[key] > maxInt-it.Quantity {
			return nil, nil, fmt.Errorf("invalid order quantity")
		}
		products[it.ProductID] += it.Quantity
		skus[key] += it.Quantity
	}
	return products, skus, nil
}
