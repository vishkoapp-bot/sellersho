package repository

import (
	"strings"
	"testing"
)

// P0 variant physical/image validation (spec §7.2 cases 4-7).
func TestValidateVariantPhysical(t *testing.T) {
	cases := []struct {
		name string
		v    map[string]interface{}
		want string // "" = ok, else substring of the error
	}{
		{"no physicals", map[string]interface{}{}, ""},
		{"weight ok", map[string]interface{}{"weightGrams": float64(1200)}, ""},
		{"weight snake ok", map[string]interface{}{"weight_grams": float64(1200)}, ""},
		{"weight zero", map[string]interface{}{"weightGrams": float64(0)}, "invalid SKU weight"},
		{"weight negative", map[string]interface{}{"weightGrams": float64(-5)}, "invalid SKU weight"},
		{"weight fractional", map[string]interface{}{"weightGrams": 1.5}, "invalid SKU weight"},
		{"weight string", map[string]interface{}{"weightGrams": "1200"}, "invalid SKU weight"},
		{"weight null", map[string]interface{}{"weightGrams": nil}, ""},
		{"dims all", map[string]interface{}{"lengthMm": float64(300), "widthMm": float64(200), "heightMm": float64(150)}, ""},
		{"dims none", map[string]interface{}{}, ""},
		{"dims partial length only", map[string]interface{}{"lengthMm": float64(300)}, "incomplete SKU dimensions"},
		{"dims partial two", map[string]interface{}{"lengthMm": float64(300), "widthMm": float64(200)}, "incomplete SKU dimensions"},
		{"dims bad value", map[string]interface{}{"lengthMm": float64(300), "widthMm": float64(0), "heightMm": float64(150)}, "invalid SKU dimensions"},
		{"images 4 ok", map[string]interface{}{"images": []interface{}{"/static/uploads/products/a.jpg", "/static/uploads/products/b.jpg", "/static/uploads/products/c.jpg", "/static/uploads/products/d.jpg"}}, ""},
		{"images 5", map[string]interface{}{"images": []interface{}{"/static/uploads/products/a.jpg", "/static/uploads/products/b.jpg", "/static/uploads/products/c.jpg", "/static/uploads/products/d.jpg", "/static/uploads/products/e.jpg"}}, "invalid SKU images"},
		{"images external hotlink", map[string]interface{}{"images": []interface{}{"https://cdn.example.com/a.jpg"}}, "invalid SKU images"},
		{"images empty", map[string]interface{}{"images": []interface{}{}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateVariantPhysical(tc.v)
			if tc.want == "" && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("expected error containing %q, got %v", tc.want, err)
				}
			}
		})
	}
}

func TestValidateVariantPhysicalValues(t *testing.T) {
	i := func(n int) *int { return &n }
	if err := ValidateVariantPhysicalValues(nil, nil, nil, nil); err != nil {
		t.Fatalf("all nil should be ok: %v", err)
	}
	if err := ValidateVariantPhysicalValues(i(0), nil, nil, nil); err == nil {
		t.Fatalf("weight 0 should fail")
	}
	if err := ValidateVariantPhysicalValues(nil, i(10), nil, nil); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("partial dims should fail incomplete, got %v", err)
	}
	if err := ValidateVariantPhysicalValues(nil, i(10), i(20), i(30)); err != nil {
		t.Fatalf("full dims should be ok: %v", err)
	}
}
