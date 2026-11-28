package repository

import (
	"context"
	"reflect"
	"testing"

	"github.com/drup/platform/internal/catalogpage"
)

func TestStorefrontStockUsesVisiblePurchasableQuantities(t *testing.T) {
	fx := perf01FixtureSetup(t)
	ctx := context.Background()
	updates := []string{
		`UPDATE products SET product_type='variable', schema_version=2, attribute_schema='[]'::jsonb WHERE id IN ('perf01-prod-001','perf01-prod-002')`,
		`UPDATE products SET stock=0 WHERE id='perf01-prod-002'`,
		`UPDATE product_variants SET stock=1,min_cart_qty=2 WHERE id='11111111-1111-1111-1111-111111111001'`,
		`UPDATE product_variants SET stock=20 WHERE id='11111111-1111-1111-1111-111111111002'`,
		`INSERT INTO store_variant_visibility(store_id,product_id,variant_id,is_visible)
			VALUES('perf01-A','perf01-prod-001','11111111-1111-1111-1111-111111111002',FALSE)`,
		`UPDATE product_variants SET stock=7, variant_name='پیش‌فرض' WHERE id='11111111-1111-1111-1111-111111111003'`,
		`UPDATE product_variants SET stock=2,min_cart_qty=2 WHERE id='11111111-1111-1111-1111-111111111004'`,
	}
	for _, update := range updates {
		if _, err := fx.tx.ExecContext(ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	scope := catalogpage.Scope{ID: "perf01-A", OwnerType: "dropshipper", Revision: 1}
	filter := catalogpage.Filter{Sort: "featured", StorePosition: true, InStockOnly: true, VisibleStockOnly: true}
	ids, total := b1aAllKeys(t, fx, scope, filter)
	if want := []string{perf01ProdID(5), perf01ProdID(2)}; total != len(want) || !reflect.DeepEqual(ids, want) {
		t.Fatalf("catalogue with available variants: total=%d, got=%v want=%v", total, ids, want)
	}

	if _, err := fx.tx.ExecContext(ctx, `UPDATE product_variants SET stock=1
		WHERE id='11111111-1111-1111-1111-111111111004'`); err != nil {
		t.Fatal(err)
	}
	ids, total = b1aAllKeys(t, fx, scope, filter)
	if want := []string{perf01ProdID(5)}; total != len(want) || !reflect.DeepEqual(ids, want) {
		t.Fatalf("placeholder cannot supply a variable product: total=%d, got=%v want=%v", total, ids, want)
	}

	filter.VisibleStockOnly = false
	ids, total = b1aAllKeys(t, fx, scope, filter)
	if want := []string{perf01ProdID(5), perf01ProdID(1)}; total != len(want) || !reflect.DeepEqual(ids, want) {
		t.Fatalf("legacy stock flag: total=%d, got=%v want=%v", total, ids, want)
	}
}
