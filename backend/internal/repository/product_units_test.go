package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestProductUnitMintCap(t *testing.T) {
	db, schema, variant, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	var lot string
	if err := tx.QueryRow(`SELECT id FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, variant).Scan(&lot); err != nil {
		t.Fatal(err)
	}
	if err := lockSKUProductTx(ctx, tx, "p", variant); err != nil {
		t.Fatal(err)
	}
	first, err := mintUnitsTx(ctx, tx, variant, "p", lot, 2, "in_stock", "fixture", "first", "")
	if err != nil || len(first) != 2 || first[0].Sequence != 2 || first[1].Sequence != 3 {
		t.Fatalf("unexpected continuation: %+v, %v", first, err)
	}
	if _, err := tx.Exec(`UPDATE product_variant_unit_counters SET next_sequence=9999 WHERE variant_id=$1`, variant); err != nil {
		t.Fatal(err)
	}
	unit, err := mintUnitsTx(ctx, tx, variant, "p", lot, 1, "in_stock", "fixture", "last", "")
	if err != nil || len(unit) != 1 || unit[0].Sequence != 9999 {
		t.Fatalf("final sequence: %+v, %v", unit, err)
	}
	if _, err = mintUnitsTx(ctx, tx, variant, "p", lot, 1, "in_stock", "fixture", "over-cap", ""); !errors.Is(err, ErrUnitCapExceeded) {
		t.Fatalf("expected cap error, got %v", err)
	}
	var quantity, units, next int
	if err := tx.QueryRow(`SELECT b.quantity,(SELECT COUNT(*) FROM product_units WHERE balance_id=b.id),c.next_sequence
		FROM inventory_balances b JOIN product_variant_unit_counters c ON c.variant_id=b.variant_id WHERE b.id=$1`, lot).
		Scan(&quantity, &units, &next); err != nil || quantity != 4 || units != 4 || next != 10000 {
		t.Fatalf("cap changed persisted state: quantity=%d units=%d next=%d err=%v", quantity, units, next, err)
	}
}

func TestProductUnitCode(t *testing.T) {
	for _, prefix := range []string{"D0F91A60", "SPNC-T19A"} {
		a := formatUnitCode(prefix, 1)
		b := formatUnitCode(prefix, 10)
		c := formatUnitCode(prefix, 200)
		if a != prefix+"-X0001" || b != prefix+"-X0010" || c != prefix+"-X0200" || !(a < b && b < c) {
			t.Fatalf("invalid format or lexical order: %q %q %q", a, b, c)
		}
	}
}

func TestProductUnitTransitionMatrix(t *testing.T) {
	allowed := map[string]string{
		"defined":     "in_stock quarantined damaged removed",
		"in_stock":    "reserved sold quarantined damaged removed",
		"reserved":    "in_stock quarantined damaged removed",
		"sold":        "in_stock returned",
		"returned":    "in_stock quarantined damaged",
		"quarantined": "in_stock damaged removed",
		"damaged":     "removed",
		"removed":     "",
	}
	for from, targets := range allowed {
		for to := range allowed {
			want := strings.Contains(" "+targets+" ", " "+to+" ")
			if got := unitTransitionAllowed(from, to); got != want {
				t.Errorf("%s -> %s: got %t, want %t", from, to, got, want)
			}
		}
	}
}

func TestProductUnitLotMapping(t *testing.T) {
	for _, tc := range []struct{ status, location, want string }{
		{"quarantine", "return:123", "returned"},
		{"quarantine", "supplier-return:123", "quarantined"},
		{"qc_accepted", "inbound-line:123", "defined"},
		{"physical", "warehouse:123", ""},
	} {
		if got := unitStatusForLot(tc.status, tc.location); got != tc.want {
			t.Errorf("%+v: got %q", tc, got)
		}
	}
}
