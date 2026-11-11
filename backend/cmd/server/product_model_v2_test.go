package main

import (
	"encoding/json"
	"testing"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/pricing"
)

func TestProductV2CatalogCarriesSchemaSelectionAndActualLimits(t *testing.T) {
	max := 7
	cost := int64(12345)
	p := domain.Product{ID: "p", ProductType: "variable", SchemaVersion: 2, ProductSchema: &domain.ProductSchema{Version: 2, Type: "variable"}}
	v := domain.ProductVariant{ID: "v", Selection: map[string]string{"color": "color:1"}, InventoryMode: "tracked", IsActive: false, CombinationKey: `[["color","color:1"]]`, MinCartQty: 3, MaxCartQty: &max, SupplierBaseCost: &cost}
	response := buildProductResponse(p, pricing.ReferenceResolution{}, []domain.ProductVariant{v}, nil)
	b, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err = json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["productType"] != "variable" || payload["schemaVersion"] != float64(2) || payload["productSchema"] == nil {
		t.Fatalf("model omitted: %s", b)
	}
	sku := payload["variants"].([]interface{})[0].(map[string]interface{})
	if sku["inventoryMode"] != "tracked" || sku["isActive"] != false || sku["minCartQty"] != float64(3) || sku["maxCartQty"] != float64(7) || sku["selection"].(map[string]interface{})["color"] != "color:1" {
		t.Fatalf("SKU contract lost: %s", b)
	}
	if _, ok := sku["supplierBaseCost"]; ok {
		t.Fatalf("private cost exposed in public response: %s", b)
	}
}

func TestProductV2DefaultDoesNotSelectExcludedSKU(t *testing.T) {
	vs := []domain.ProductVariant{{ID: "excluded", InventoryMode: "tracked", IsActive: false, IsDefault: true}, {ID: "active", InventoryMode: "tracked", IsActive: true}}
	if v := DefaultVariantForResponse(vs); v == nil || v.ID != "active" {
		t.Fatal(v)
	}
	if v := DefaultVariantForResponse(vs[:1]); v != nil {
		t.Fatal("excluded SKU selected")
	}
}
