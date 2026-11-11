package repository

import (
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/domain"
	"testing"
)

// These tests exercise only JSON validation. They never open a database.
func v2Fixture(t *testing.T, colors, sizes int) map[string]interface{} {
	t.Helper()
	attrs := []interface{}{}
	colorValues, sizeValues := []interface{}{}, []interface{}{}
	for i := 1; i <= colors; i++ {
		colorValues = append(colorValues, map[string]interface{}{"id": fmt.Sprintf("color:%d", i), "label": fmt.Sprintf("Color %d", i)})
	}
	for i := 1; i <= sizes; i++ {
		sizeValues = append(sizeValues, map[string]interface{}{"id": fmt.Sprintf("dkv:-1:%d", -i), "label": fmt.Sprintf("Size %d", i)})
	}
	attrs = append(attrs, map[string]interface{}{"id": "color", "name": "Color", "role": "axis", "source": "color", "values": colorValues})
	attrs = append(attrs, map[string]interface{}{"id": "dk:-1", "registryId": float64(-1), "name": "Size", "role": "axis", "source": "dk", "values": sizeValues})
	variants := []interface{}{}
	for c := 1; c <= colors; c++ {
		for s := 1; s <= sizes; s++ {
			variants = append(variants, map[string]interface{}{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", len(variants)+1), "selection": map[string]interface{}{"color": fmt.Sprintf("color:%d", c), "dk:-1": fmt.Sprintf("dkv:-1:%d", -s)}, "supplier_base_cost": float64(100 + c + s), "stock": float64(2), "is_active": true})
		}
	}
	data := map[string]interface{}{"schemaVersion": float64(2), "productType": "variable", "productSchema": map[string]interface{}{"version": float64(2), "type": "variable", "attributes": attrs}, "variants": variants}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestProductV2AcceptsTwentyFiveSparseStableSKUs(t *testing.T) {
	d := v2Fixture(t, 5, 5)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	if d["stock"] != float64(50) || d["supplierBaseCost"] != float64(102) {
		t.Fatalf("wrong aggregates: %v", d)
	}
	vs := d["variants"].([]interface{})
	first := vs[0].(map[string]interface{})
	if first["id"] != "00000000-0000-4000-8000-000000000001" || first["variant_name"] != "Color: Color 1 / Size: Size 1" {
		t.Fatalf("identity/display: %v", first)
	}
	if first["combination_key"] != `[["color","color:1"],["dk:-1","dkv:-1:-1"]]` {
		t.Fatalf("ambiguous combination key: %v", first)
	}
	// Exclusion and sold-out are distinct: excluded rows do not contribute totals.
	first["is_active"] = false
	first["supplier_base_cost"] = float64(0)
	vs[1].(map[string]interface{})["stock"] = float64(0)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	if d["stock"] != float64(46) {
		t.Fatalf("inactive/sold-out total: %v", d["stock"])
	}
}

func TestProductV2RegistryRejectsWrongCategoryAndSpoofedLabels(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	schema, err := productSchemaFromData(d)
	if err != nil {
		t.Fatal(err)
	}
	available := append([]domain.ProductAttributeDefinition{}, schema.Attributes...)
	if err = validateV2Registry(schema, available); err != nil {
		t.Fatal(err)
	}
	if err = validateV2Registry(schema, available[:1]); err == nil {
		t.Fatal("foreign category definition accepted")
	}
	available[1].Name = "Different registry title"
	if err = validateV2Registry(schema, available); err == nil {
		t.Fatal("unreviewed display label substitution accepted")
	}
}

func TestProductV2AllowsRequestedOptionOnExistingDefinition(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	schema, _ := productSchemaFromData(d)
	available := append([]domain.ProductAttributeDefinition{}, schema.Attributes...)
	schema.Attributes[0].Values = []domain.ProductAttributeOption{{ID: "requestedv:00000000-0000-4000-8000-000000000097", Label: "New color"}}
	b, _ := json.Marshal(schema)
	var mapped interface{}
	_ = json.Unmarshal(b, &mapped)
	d["productSchema"] = mapped
	d["variants"].([]interface{})[0].(map[string]interface{})["selection"].(map[string]interface{})["color"] = "requestedv:00000000-0000-4000-8000-000000000097"
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	if err := validateV2Registry(schema, available); err != nil {
		t.Fatal(err)
	}
	schema.Attributes[0].Values[0].Label = " Color 1 "
	if err := validateV2Registry(schema, available); err == nil {
		t.Fatal("duplicate option label accepted with a new UUID")
	}
}

func TestProductV2RegistryRejectsRequestedOptionOwnedByAnotherAttribute(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	schema, _ := productSchemaFromData(d)
	available := append([]domain.ProductAttributeDefinition{}, schema.Attributes...)
	id := "requestedv:00000000-0000-4000-8000-000000000096"
	available = append(available, domain.ProductAttributeDefinition{ID: "requested:00000000-0000-4000-8000-000000000095", Name: "Fit", Source: "local", Values: []domain.ProductAttributeOption{{ID: id, Label: "Loose"}}})
	schema.Attributes[0].Values = []domain.ProductAttributeOption{{ID: id, Label: "New color"}}
	if err := validateV2Registry(schema, available); err == nil {
		t.Fatal("existing option borrowed across attributes")
	}
}

func TestProductV2RejectsAmbiguousOrIncompleteStructure(t *testing.T) {
	cases := map[string]func(map[string]interface{}){
		"mixed axes": func(d map[string]interface{}) {
			delete(d["variants"].([]interface{})[0].(map[string]interface{})["selection"].(map[string]interface{}), "color")
		},
		"duplicate combination": func(d map[string]interface{}) {
			vs := d["variants"].([]interface{})
			vs[1].(map[string]interface{})["selection"] = vs[0].(map[string]interface{})["selection"]
		},
		"foreign option": func(d map[string]interface{}) {
			d["variants"].([]interface{})[0].(map[string]interface{})["selection"].(map[string]interface{})["color"] = "color:99"
		},
		"duplicate id": func(d map[string]interface{}) {
			vs := d["variants"].([]interface{})
			vs[1].(map[string]interface{})["id"] = vs[0].(map[string]interface{})["id"]
		},
		"non UUID id": func(d map[string]interface{}) {
			d["variants"].([]interface{})[0].(map[string]interface{})["id"] = "draft-1"
		},
		"type mismatch":   func(d map[string]interface{}) { d["productType"] = "simple" },
		"unknown version": func(d map[string]interface{}) { d["schemaVersion"] = float64(3) },
		"missing schema":  func(d map[string]interface{}) { delete(d, "productSchema") },
		"fractional stock": func(d map[string]interface{}) {
			d["variants"].([]interface{})[0].(map[string]interface{})["stock"] = 1.5
		},
		"duplicate semantic definition": func(d map[string]interface{}) {
			a := d["productSchema"].(map[string]interface{})["attributes"].([]interface{})
			a[1].(map[string]interface{})["name"] = " COLOR "
		},
		"fixed alias duplicated": func(d map[string]interface{}) {
			a := d["productSchema"].(map[string]interface{})["attributes"].([]interface{})
			d["productSchema"].(map[string]interface{})["attributes"] = append(a, map[string]interface{}{"id": "dk:44", "name": " Color ", "source": "dk", "registryId": float64(44), "role": "fixed", "values": []interface{}{map[string]interface{}{"id": "dkv:44:9", "label": "red"}}})
		},
		"legacy color mirror": func(d map[string]interface{}) {
			d["categoryAttributes"] = map[string]interface{}{"colorId": float64(1)}
		},
		"stock exceeds product integer": func(d map[string]interface{}) {
			for _, v := range d["variants"].([]interface{}) {
				v.(map[string]interface{})["stock"] = float64(1000000000)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			d := v2Fixture(t, 2, 2)
			change(d)
			if err := ValidateProductVariationProposal(d); err == nil {
				t.Fatal("invalid V2 configuration accepted")
			}
		})
	}
}

func TestProductV2SimpleAndRequestedDefinitions(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	d["productType"] = "simple"
	schema := d["productSchema"].(map[string]interface{})
	schema["type"] = "simple"
	attrs := schema["attributes"].([]interface{})
	for _, a := range attrs {
		a.(map[string]interface{})["role"] = "fixed"
	}
	v := d["variants"].([]interface{})[0].(map[string]interface{})
	v["selection"] = map[string]interface{}{}
	v["stock"] = float64(0)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	if v["variant_name"] != "پیش‌فرض" || d["stock"] != float64(0) {
		t.Fatal(d)
	}
	// Pending requests carry stable identities, never registry name keys.
	d = v2Fixture(t, 1, 1)
	a := d["productSchema"].(map[string]interface{})["attributes"].([]interface{})[1].(map[string]interface{})
	a["id"] = "requested:00000000-0000-4000-8000-000000000099"
	a["source"] = "requested"
	delete(a, "registryId")
	a["values"] = []interface{}{map[string]interface{}{"id": "requestedv:00000000-0000-4000-8000-000000000098", "label": "Large"}}
	sel := d["variants"].([]interface{})[0].(map[string]interface{})["selection"].(map[string]interface{})
	delete(sel, "dk:-1")
	sel[a["id"].(string)] = "requestedv:00000000-0000-4000-8000-000000000098"
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
}

func TestProductV2EnforcesBoundedMatrix(t *testing.T) {
	d := v2Fixture(t, 20, 25)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	d = v2Fixture(t, 21, 25)
	if err := ValidateProductVariationProposal(d); err == nil {
		t.Fatal("unbounded matrix accepted")
	}
}

func TestProductV2CanonicalizesRepositoryIntegerInputs(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	v := d["variants"].([]interface{})[0].(map[string]interface{})
	v["supplier_base_cost"] = int64(20)
	v["stock"] = int(2)
	v["min_cart_qty"] = int(1)
	v["max_cart_qty"] = int64(3)
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	if d["supplierBaseCost"] != float64(20) || d["stock"] != float64(2) {
		t.Fatal(d)
	}
}

func TestProductV2DerivesFixedSpecsFromCanonicalDefinition(t *testing.T) {
	d := v2Fixture(t, 1, 1)
	d["productType"] = "simple"
	schema := d["productSchema"].(map[string]interface{})
	schema["type"] = "simple"
	for _, raw := range schema["attributes"].([]interface{}) {
		raw.(map[string]interface{})["role"] = "fixed"
	}
	d["variants"].([]interface{})[0].(map[string]interface{})["selection"] = map[string]interface{}{}
	d["specs"] = map[string]interface{}{"Color": "obsolete", "unreviewed": "spoofed"}
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	specs := d["specs"].(map[string]interface{})
	if len(specs) != 2 || specs["Color"] != "Color 1" || specs["Size"] != "Size 1" {
		t.Fatal("static mirrors contradict canonical fixed attributes", specs)
	}
	// A fixed-value correction must update the compatibility projection too.
	a := d["productSchema"].(map[string]interface{})["attributes"].([]interface{})[0].(map[string]interface{})
	a["values"] = []interface{}{map[string]interface{}{"id": "color:2", "label": "Color 2"}}
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	if d["specs"].(map[string]interface{})["Color"] != "Color 2" {
		t.Fatal("fixed correction left obsolete static mirror")
	}
	d = v2Fixture(t, 1, 1)
	d["specs"] = map[string]interface{}{"Color": "duplicate static color"}
	if err := ValidateProductVariationProposal(d); err != nil {
		t.Fatal(err)
	}
	if len(d["specs"].(map[string]interface{})) != 0 {
		t.Fatal("variation axes duplicated as fixed specs")
	}
}
