package repository

import "testing"

func TestV2ReviewStableSKUPaths(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	other := "22222222-2222-4222-8222-222222222222"
	row := func(id string) map[string]interface{} {
		return map[string]interface{}{"id": id, "stock": float64(3), "supplier_base_cost": float64(100), "is_active": true, "selection": map[string]interface{}{"color": "red"}}
	}
	data := map[string]interface{}{"schemaVersion": float64(2), "productType": "variable", "productSchema": map[string]interface{}{"version": float64(2), "type": "variable", "attributes": []interface{}{map[string]interface{}{"id": "color", "name": "Color", "role": "axis", "source": "color", "values": []interface{}{map[string]interface{}{"id": "red", "label": "Red"}}}}}, "variants": []interface{}{row(id), row(other)}}
	before := ReviewLeaves(data)
	key := "/variants/" + id + "/stock"
	if before[key] != float64(3) {
		t.Fatal("not stable SKU pointer", before)
	}
	data["variants"] = []interface{}{row(other), row(id)}
	if ReviewValueHash(ReviewLeaves(data)["/variants"]) != ReviewValueHash(before["/variants"]) {
		t.Fatal("reorder changes structural identity")
	}
	if err := ApplyReviewCorrections(data, map[string]interface{}{key: float64(7)}, map[string]bool{key: true}); err != nil {
		t.Fatal(err)
	}
	if ReviewLeaves(data)[key] != float64(7) {
		t.Fatal("wrong SKU edited")
	}
	changed := []interface{}{row(other), row(id)}
	changed[0].(map[string]interface{})["stock"] = float64(99)
	if ApplyReviewCorrections(data, map[string]interface{}{"/variants": changed}, map[string]bool{"/variants": true}) == nil {
		t.Fatal("structural edit bypassed approved stock")
	}
}
func TestV2ReviewInactiveCombinationCorrection(t *testing.T) {
	data := map[string]interface{}{"schemaVersion": float64(2), "variants": []interface{}{map[string]interface{}{"id": "11111111-1111-4111-8111-111111111111", "is_active": false, "stock": float64(0), "supplier_base_cost": float64(0), "selection": map[string]interface{}{}}}}
	if err := validateCorrectionValues(data); err != nil {
		t.Fatalf("inactive V2 combinations must defer financial validation to typed validator: %v", err)
	}
}

func TestV2ReviewCartLimitsCannotBypassApprovalViaStructure(t *testing.T) {
	d := v2Fixture(t, 2, 1)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	first := d["variants"].([]interface{})[0].(map[string]interface{})
	id := first["id"].(string)
	first["min_cart_qty"] = float64(2)
	first["max_cart_qty"] = float64(4)
	minKey, maxKey := "/variants/"+id+"/min_cart_qty", "/variants/"+id+"/max_cart_qty"
	before := ReviewLeaves(d)
	if before[minKey] != float64(2) || before[maxKey] != float64(4) {
		t.Fatal("cart limits omitted from review")
	}
	changed := cloneReviewDocument(d)["variants"].([]interface{})
	changed[0].(map[string]interface{})["min_cart_qty"] = float64(3)
	if err := ApplyReviewCorrections(d, map[string]interface{}{"/variants": changed}, map[string]bool{"/variants": true}); err == nil {
		t.Fatal("structure rejection bypassed approved cart limit")
	}
	if err := ApplyReviewCorrections(d, map[string]interface{}{minKey: float64(3)}, map[string]bool{minKey: true}); err != nil {
		t.Fatal(err)
	}
	if ReviewLeaves(d)[minKey] != float64(3) || ReviewLeaves(d)[maxKey] != float64(4) {
		t.Fatal("wrong cart limit corrected")
	}
	if err := ApplyReviewCorrections(d, map[string]interface{}{maxKey: nil}, map[string]bool{maxKey: true}); err != nil {
		t.Fatal("rejected maximum cannot be corrected to unbounded", err)
	}
	if ReviewLeaves(d)[maxKey] != nil {
		t.Fatal("unbounded maximum correction lost")
	}
}

func TestV2ReviewIncludesActualDefaultCartLimits(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	id := d["variants"].([]interface{})[0].(map[string]interface{})["id"].(string)
	leaves := ReviewLeaves(d)
	if leaves["/variants/"+id+"/min_cart_qty"] != float64(1) {
		t.Fatal("actual default minimum is unreviewed")
	}
	if v, ok := leaves["/variants/"+id+"/max_cart_qty"]; !ok || v != nil {
		t.Fatal("unbounded maximum is unreviewed")
	}
}

func TestV2ReviewDoesNotReviewDerivedFixedSpecsTwice(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	d["specs"] = map[string]interface{}{"Color": "old mirror"}
	for key := range ReviewLeaves(d) {
		if key == "/specs" || len(key) > 7 && key[:7] == "/specs/" {
			t.Fatal("derived compatibility specs require duplicate review", key)
		}
	}
}
