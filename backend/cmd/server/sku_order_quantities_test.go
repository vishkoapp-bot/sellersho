package main

import "testing"

func TestOrderSKUQuantitiesAggregateAddonSeparatedLines(t *testing.T) {
	items := []CreateOrderItemReq{{ProductID: "p", VariantID: "red-small", Quantity: 2}, {ProductID: "p", VariantID: "red-small", Quantity: 2}, {ProductID: "p", VariantID: "blue-large", Quantity: 1}}
	products, skus, err := aggregateOrderQuantities(items)
	if err != nil {
		t.Fatal(err)
	}
	if products["p"] != 5 || skus["p/red-small"] != 4 || skus["p/blue-large"] != 1 {
		t.Fatalf("incorrect aggregate products=%v skus=%v", products, skus)
	}
}

func TestOrderSKUQuantitiesRejectInvalidAndOverflow(t *testing.T) {
	for _, items := range [][]CreateOrderItemReq{
		{{ProductID: "p", Quantity: 0}},
		{{ProductID: "p", Quantity: -1}},
		{{ProductID: "p", Quantity: int(^uint(0) >> 1)}, {ProductID: "p", Quantity: 1}},
	} {
		if _, _, err := aggregateOrderQuantities(items); err == nil {
			t.Fatalf("accepted invalid quantities: %v", items)
		}
	}
}
