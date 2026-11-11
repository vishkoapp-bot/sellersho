package repository

import "testing"

func TestSKUSelectionRequiredOnlyForTypedProducts(t *testing.T) {
	for _, tc := range []struct {
		name, typ, selected string
		version             int
		reject              bool
	}{
		{"legacy unallocated remains readable", "legacy", "", 0, false},
		{"variable cannot bypass SKU", "variable", "", 2, true},
		{"simple V2 uses its real SKU", "simple", "", 2, true},
		{"chosen variable SKU", "variable", "sku", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateProductSKUSelection(tc.version, tc.typ, tc.selected); (err != nil) != tc.reject {
				t.Fatalf("error = %v, reject = %v", err, tc.reject)
			}
		})
	}
}

func TestTrackedSKUQuantityHasNoZeroStockEscape(t *testing.T) {
	max := 3
	for _, tc := range []struct {
		name, mode      string
		active          bool
		stock, min, qty int
		max             *int
		reject          bool
	}{
		{"sold out", "tracked", true, 0, 1, 1, nil, true},
		{"inactive", "tracked", false, 10, 1, 1, nil, true},
		{"insufficient", "tracked", true, 2, 1, 3, nil, true},
		{"valid tracked", "tracked", true, 2, 1, 2, nil, false},
		{"below minimum", "tracked", true, 10, 2, 1, nil, true},
		{"aggregate exceeds maximum", "tracked", true, 10, 1, 4, &max, true},
		{"legacy explicit untracked", "legacy", true, 0, 1, 2, nil, false},
		{"legacy positive cap preserved", "legacy", true, 1, 1, 2, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateSKUQuantity(tc.mode, tc.active, tc.stock, tc.min, tc.max, tc.qty); (err != nil) != tc.reject {
				t.Fatalf("error = %v, reject = %v", err, tc.reject)
			}
		})
	}
}
