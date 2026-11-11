package repository

import "testing"

func TestVariableProductFiveColors(t *testing.T) {
	variants := []interface{}{}
	for i, color := range []string{"red", "blue", "black", "white", "green"} {
		variants = append(variants, map[string]interface{}{"variant_name": "ignored", "attributes": map[string]interface{}{"color": color}, "supplier_base_cost": float64(100 + i), "stock": float64(i + 1)})
	}
	data := map[string]interface{}{"productType": "variable", "variants": variants}
	if err := ValidateProductVariationProposal(data); err != nil {
		t.Fatal(err)
	}
	if data["stock"] != float64(15) || data["supplierBaseCost"] != float64(100) {
		t.Fatal(data)
	}
	for _, v := range variants {
		if v.(map[string]interface{})["variant_name"] == "ignored" {
			t.Fatal("name not derived")
		}
	}
	variants[1].(map[string]interface{})["attributes"] = variants[0].(map[string]interface{})["attributes"]
	if ValidateProductVariationProposal(data) == nil {
		t.Fatal("duplicate combination accepted")
	}
}
func TestProductVariationTypeGuards(t *testing.T) {
	for _, kind := range []string{"simple", "variable"} {
		if ValidateProductVariationProposal(map[string]interface{}{"productType": kind, "variants": []interface{}{}}) == nil {
			t.Fatal("empty accepted")
		}
	}
	sku := map[string]interface{}{"variant_name": "main", "attributes": map[string]interface{}{}, "supplier_base_cost": float64(300), "stock": float64(0)}
	data := map[string]interface{}{"productType": "simple", "variants": []interface{}{sku}}
	if err := ValidateProductVariationProposal(data); err != nil {
		t.Fatal(err)
	}
	data["productType"] = "variable"
	if ValidateProductVariationProposal(data) == nil {
		t.Fatal("attribute-free variable accepted")
	}
	data["productType"] = "simple"
	sku["stock"] = 1.5
	if ValidateProductVariationProposal(data) == nil {
		t.Fatal("fractional stock accepted")
	}
}
