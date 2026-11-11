package repository

import "testing"

func TestGranularReviewCorrections(t *testing.T) {
	data := map[string]interface{}{"title": "shirt", "supplierId": "private", "variants": []interface{}{map[string]interface{}{"stock": float64(5), "supplier_base_cost": float64(200), "attributes": map[string]interface{}{"color": "blue"}}}}
	fields := ReviewLeaves(data)
	if _, ok := fields["/supplierId"]; ok {
		t.Fatal("metadata exposed")
	}
	if _, ok := fields["/variants/0/stock"]; !ok {
		t.Fatal("SKU not granular")
	}
	old := ReviewValueHash(fields["/variants/0/stock"])
	if err := ApplyReviewCorrections(data, map[string]interface{}{"/variants/0/stock": float64(9)}, map[string]bool{"/variants/0/stock": true}); err != nil {
		t.Fatal(err)
	}
	if ReviewValueHash(ReviewLeaves(data)["/variants/0/stock"]) == old {
		t.Fatal("hash unchanged")
	}
	if ApplyReviewCorrections(data, map[string]interface{}{"/title": "hacked"}, map[string]bool{"/variants/0/stock": true}) == nil {
		t.Fatal("approved field changed")
	}
	if ApplyReviewCorrections(data, map[string]interface{}{"/supplierId": "hacked"}, map[string]bool{"/supplierId": true}) == nil {
		t.Fatal("ownership changed")
	}
}
func TestGranularReviewReasonRequired(t *testing.T) {
	if ValidateReviewDecision("rejected", "") == nil {
		t.Fatal("missing reason allowed")
	}
	if err := ValidateReviewDecision("rejected", "bad image"); err != nil {
		t.Fatal(err)
	}
	if ValidateReviewDecision("invalid", "x") == nil {
		t.Fatal("bad decision")
	}
}

func TestGranularCorrectionNumbersAndAttributeNames(t *testing.T) {
	if validateCorrectionValues(map[string]interface{}{"weightGrams": float64(0)}) == nil {
		t.Fatal("zero weight accepted")
	}
	if err := validateCorrectionValues(map[string]interface{}{"variants": []interface{}{map[string]interface{}{"attributes": map[string]interface{}{"stock": "cotton"}}}}); err != nil {
		t.Fatal("attribute name confused with SKU field", err)
	}
}

func TestGranularDerivedAndAliasFields(t *testing.T) {
	data := map[string]interface{}{"productType": "variable", "stock": float64(3), "supplierBaseCost": float64(100), "variants": []interface{}{map[string]interface{}{"variant_name": "derived", "stock": float64(3), "attributes": map[string]interface{}{"variant_name": "custom attribute"}}}}
	leaves := ReviewLeaves(data)
	for _, key := range []string{"/stock", "/supplierBaseCost", "/variants/0/variant_name"} {
		if _, ok := leaves[key]; ok {
			t.Fatal("derived field is editable", key)
		}
	}
	if _, ok := leaves["/variants/0/attributes/variant_name"]; !ok {
		t.Fatal("attribute accidentally hidden")
	}
	if NormalizeReviewAliases(map[string]interface{}{"supplierBaseCost": float64(100), "supplier_base_cost": float64(999)}) == nil {
		t.Fatal("conflicting price aliases accepted")
	}
	legacy := map[string]interface{}{"supplier_base_cost": float64(100)}
	if _, ok := ReviewLeaves(legacy)["/supplier_base_cost"]; !ok {
		t.Fatal("legacy price not reviewed")
	}
	if err := NormalizeReviewAliases(legacy); err != nil || legacy["supplierBaseCost"] != float64(100) {
		t.Fatal("legacy normalization", err)
	}
}

func TestGranularSimpleNameAndAliasCorrection(t *testing.T) {
	simple := map[string]interface{}{"productType": "simple", "variants": []interface{}{map[string]interface{}{"variant_name": "SKU label"}}}
	if _, ok := ReviewLeaves(simple)["/variants/0/variant_name"]; !ok {
		t.Fatal("simple name not reviewed")
	}
	images := map[string]interface{}{"imageUrl": "old", "image_url": "old"}
	if err := syncReviewAliasCorrections(images, map[string]interface{}{"/imageUrl": "new"}); err != nil {
		t.Fatal(err)
	}
	if images["image_url"] != "new" {
		t.Fatal("legacy mirror stale")
	}
	if err := NormalizeReviewAliases(images); err != nil {
		t.Fatal(err)
	}
}
