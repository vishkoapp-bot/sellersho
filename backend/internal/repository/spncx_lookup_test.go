package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// lookupMintUnits mints n units of variant into its sellable lot for product
// and returns their codes in sequence order.
func lookupMintUnits(t *testing.T, tx *sql.Tx, variant, product string, n int) []string {
	t.Helper()
	var lot string
	if err := tx.QueryRow(`SELECT id FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, variant).Scan(&lot); err != nil {
		t.Fatal(err)
	}
	units, err := mintUnitsTx(context.Background(), tx, variant, product, lot, n, "in_stock", "fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	codes := make([]string, len(units))
	for i, unit := range units {
		codes[i] = unit.Code
	}
	return codes
}

// lookupSecondSupplier stages a second supplier's product/variant/lot in the
// same scratch schema and returns the variant id. The lot is sellable with
// quantity 2.
func lookupSecondSupplier(t *testing.T, tx *sql.Tx) string {
	t.Helper()
	variant := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version,product_type,origin_supplier_warehouse_id)
		VALUES('p2','s2',0,2,'variable','sw1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,stock,inventory_mode,spnc,variant_name)
		VALUES($1,'p2',2,'tracked','B0C00001','Second')`, variant); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_name)
		VALUES('lot-p2','p2','s2',$1,'supplier','supplier','supplier-warehouse:sw1','sellable',2,'Second warehouse')`, variant); err != nil {
		t.Fatal(err)
	}
	return variant
}

func lookupSellableLot(t *testing.T, db *sql.DB, variant string) string {
	t.Helper()
	var lot string
	if err := db.QueryRow(`SELECT id FROM inventory_balances WHERE variant_id=$1 AND status='sellable'`, variant).Scan(&lot); err != nil {
		t.Fatal(err)
	}
	return lot
}

func TestSPNCXLookupByCode(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	codes := lookupMintUnits(t, setup, red, "p", 2)
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()

	unit, err := GetUnitByCode(ctx, db, "", codes[0])
	if err != nil {
		t.Fatal(err)
	}
	if unit.Code != codes[0] || unit.Status != "in_stock" {
		t.Fatalf("lookup: code=%q status=%q", unit.Code, unit.Status)
	}
	if unit.Variant.ID != red || unit.Variant.SPNC != "A0C00001" || unit.Variant.Name == "" {
		t.Fatalf("lookup variant: %+v", unit.Variant)
	}
	if unit.Product.ID != "p" || unit.Product.Title != "product" {
		t.Fatalf("lookup product: %+v", unit.Product)
	}
	if unit.Location == nil || unit.Location.BalanceID != lookupSellableLot(t, db, red) ||
		unit.Location.LotStatus != "sellable" || unit.Location.LocationKey == "" {
		t.Fatalf("lookup location: %+v", unit.Location)
	}
	if unit.Printed.PrintedAt != nil || unit.Printed.PrintCount != 0 || unit.Printed.LastPrintedAt != nil {
		t.Fatalf("never-printed unit must have null print fields: %+v", unit.Printed)
	}
	if unit.Order != nil {
		t.Fatalf("in-stock unit must have no order link: %+v", unit.Order)
	}
	if len(unit.History) != 1 || unit.History[0].EventType != "minted" || unit.History[0].ToStatus == nil ||
		*unit.History[0].ToStatus != "in_stock" || unit.History[0].ReferenceType != "fixture" {
		t.Fatalf("lookup history: %+v", unit.History)
	}

	// Supplier scoping: the owning supplier resolves it, any other supplier
	// gets the same not-found as an unknown code.
	if _, err := GetUnitByCode(ctx, db, "s", codes[1]); err != nil {
		t.Fatalf("owning supplier lookup: %v", err)
	}
	if _, err := GetUnitByCode(ctx, db, "s2", codes[1]); !errors.Is(err, ErrUnitNotFound) {
		t.Fatalf("cross-supplier lookup: err=%v, want ErrUnitNotFound", err)
	}
	if _, err := GetUnitByCode(ctx, db, "", "A0C00001-X9999"); !errors.Is(err, ErrUnitNotFound) {
		t.Fatalf("unknown code lookup: err=%v, want ErrUnitNotFound", err)
	}
}

func TestSPNCXLookupSoldOrderLink(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	code := lookupMintUnits(t, setup, red, "p", 1)[0]
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()

	var unitID string
	if err := db.QueryRow(`SELECT id FROM product_units WHERE code=$1`, code).Scan(&unitID); err != nil {
		t.Fatal(err)
	}
	itemID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO orders(id,order_number) VALUES('o1','ORD-1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity)
		VALUES($1,'o1','p',$2,1)`, itemID, red); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE product_units SET status='sold' WHERE id=$1`, unitID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO order_item_units(order_item_id,unit_id,code,order_id,captured_at)
		VALUES($1,$2,$3,'o1',NOW())`, itemID, unitID, code); err != nil {
		t.Fatal(err)
	}

	unit, err := GetUnitByCode(ctx, db, "", code)
	if err != nil {
		t.Fatal(err)
	}
	if unit.Order == nil || unit.Order.OrderID != "o1" || unit.Order.OrderNumber != "ORD-1" ||
		unit.Order.RestoredAt != nil {
		t.Fatalf("sold order link: %+v", unit.Order)
	}
	if unit.Location == nil {
		t.Fatalf("sold unit must still carry its lot: %+v", unit.Location)
	}

	// Restored capture: the link carries restored_at while sold; once the
	// unit is back in stock the link disappears entirely. (The sku
	// fixture's inventory_balances status CHECK has no 'sold' value, so the
	// sold unit keeps its sellable lot here; the order link is keyed on
	// unit status, not on the lot, and the lot is not asserted.)
	if _, err := db.Exec(`UPDATE order_item_units SET restored_at=NOW() WHERE unit_id=$1`, unitID); err != nil {
		t.Fatal(err)
	}
	unit, err = GetUnitByCode(ctx, db, "", code)
	if err != nil {
		t.Fatal(err)
	}
	if unit.Order == nil || unit.Order.RestoredAt == nil {
		t.Fatalf("restored order link must carry restored_at: %+v", unit.Order)
	}
	if _, err := db.Exec(`UPDATE product_units SET status='in_stock',balance_id=$1 WHERE id=$2`,
		lookupSellableLot(t, db, red), unitID); err != nil {
		t.Fatal(err)
	}
	unit, err = GetUnitByCode(ctx, db, "", code)
	if err != nil {
		t.Fatal(err)
	}
	if unit.Order != nil {
		t.Fatalf("back-in-stock unit must have no order link: %+v", unit.Order)
	}
}

func TestSPNCXLookupRemoved(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	code := lookupMintUnits(t, setup, red, "p", 1)[0]
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()

	var unitID string
	if err := db.QueryRow(`SELECT id FROM product_units WHERE code=$1`, code).Scan(&unitID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE product_units SET status='removed',balance_id=NULL WHERE id=$1`, unitID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO product_unit_events(unit_id,code,event_type,from_status,to_status,reference_type,reference_id,actor_user_id,created_at)
		VALUES($1,$2,'removed','in_stock','removed','manual_remove','fixture',NULL,NOW() + INTERVAL '1 second')`, unitID, code); err != nil {
		t.Fatal(err)
	}

	unit, err := GetUnitByCode(ctx, db, "", code)
	if err != nil {
		t.Fatal(err)
	}
	if unit.Status != "removed" || unit.Location != nil {
		t.Fatalf("removed unit: status=%q location=%+v", unit.Status, unit.Location)
	}
	if len(unit.History) != 2 || unit.History[0].EventType != "removed" || unit.History[1].EventType != "minted" {
		t.Fatalf("removed history must lead with the removal event: %+v", unit.History)
	}
}

func TestSPNCXLookupHistoryNewestFirstCapped(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	code := lookupMintUnits(t, setup, red, "p", 1)[0]
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()

	var unitID string
	if err := db.QueryRow(`SELECT id FROM product_units WHERE code=$1`, code).Scan(&unitID); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 101; i++ {
		if _, err := db.Exec(`INSERT INTO product_unit_events(unit_id,code,event_type,from_status,to_status,reference_type,reference_id,actor_user_id,created_at)
			VALUES($1,$2,'printed','in_stock','in_stock','label_print',$3,NULL,NOW() + ($4::text || ' seconds')::interval)`,
			unitID, code, fmt.Sprintf("ev-%03d", i), i); err != nil {
			t.Fatal(err)
		}
	}

	unit, err := GetUnitByCode(ctx, db, "", code)
	if err != nil {
		t.Fatal(err)
	}
	if len(unit.History) != 100 {
		t.Fatalf("history len=%d, want 100 (capped)", len(unit.History))
	}
	if unit.History[0].ReferenceID == nil || *unit.History[0].ReferenceID != "ev-101" {
		t.Fatalf("newest history entry: %+v, want ev-101", unit.History[0])
	}
	if unit.History[99].ReferenceID == nil || *unit.History[99].ReferenceID != "ev-002" {
		t.Fatalf("oldest kept history entry: %+v, want ev-002", unit.History[99])
	}
}

// lookupCodes returns every unit code of a variant in sequence order.
func lookupCodes(t *testing.T, db *sql.DB, variant string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT code FROM product_units WHERE variant_id=$1 ORDER BY sequence`, variant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatal(err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return codes
}

func TestSPNCXLookupListFilters(t *testing.T) {
	db, schema, red, blue := skuInventoryFixture(t)
	setup := skuTestTx(t, db, schema)
	defer setup.Rollback()
	// The fixture seeds one unit per variant stock (1 red, 3 blue) via
	// SeedTrackedSKUInventoryTx, so the expected sets are read back from
	// the DB rather than assumed from the mint counts below.
	lookupMintUnits(t, setup, red, "p", 3)
	lookupMintUnits(t, setup, blue, "p", 2)
	otherVariant := lookupSecondSupplier(t, setup)
	lookupMintUnits(t, setup, otherVariant, "p2", 1)
	if err := setup.Commit(); err != nil {
		t.Fatal(err)
	}
	db = skuScopedDB(t, schema)
	ctx := context.Background()
	redCodes := lookupCodes(t, db, red)
	blueCodes := lookupCodes(t, db, blue)
	otherCodes := lookupCodes(t, db, otherVariant)
	total := len(redCodes) + len(blueCodes) + len(otherCodes)
	redLot := lookupSellableLot(t, db, red)

	// Capture one red unit into an order for the order/order-item filters.
	var redUnitID string
	if err := db.QueryRow(`SELECT id FROM product_units WHERE code=$1`, redCodes[0]).Scan(&redUnitID); err != nil {
		t.Fatal(err)
	}
	itemID := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO orders(id,order_number) VALUES('o1','ORD-1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity)
		VALUES($1,'o1','p',$2,1)`, itemID, red); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO order_item_units(order_item_id,unit_id,code,order_id,captured_at)
		VALUES($1,$2,$3,'o1',NOW())`, itemID, redUnitID, redCodes[0]); err != nil {
		t.Fatal(err)
	}

	// Multi-variant pages order variants by (created_at, id); red and blue
	// share one created_at timestamp, so the inter-variant order is by
	// random UUID — compare code sets, not sequences.
	codesOf := func(units []UnitLabelRow) []string {
		out := make([]string, len(units))
		for i, u := range units {
			out[i] = u.Code
		}
		slices.Sort(out)
		return out
	}
	sorted := func(codes []string) []string {
		out := append([]string{}, codes...)
		slices.Sort(out)
		return out
	}
	cases := []struct {
		name string
		opts ListUnitsOptions
		want []string
	}{
		{"no filter", ListUnitsOptions{}, append(append(append([]string{}, redCodes...), blueCodes...), otherCodes...)},
		{"variant red", ListUnitsOptions{VariantID: red}, redCodes},
		{"variant blue", ListUnitsOptions{VariantID: blue}, blueCodes},
		{"status in_stock", ListUnitsOptions{Status: "in_stock"}, append(append(append([]string{}, redCodes...), blueCodes...), otherCodes...)},
		{"status sold empty", ListUnitsOptions{Status: "sold"}, []string{}},
		{"status unknown empty", ListUnitsOptions{Status: "bogus"}, []string{}},
		{"q red prefix", ListUnitsOptions{Q: "A0C00001-X00"}, redCodes},
		{"q literal percent", ListUnitsOptions{Q: "A0C00001%"}, []string{}},
		{"order", ListUnitsOptions{OrderID: "o1"}, []string{redCodes[0]}},
		{"order and other variant ANDs to empty", ListUnitsOptions{OrderID: "o1", VariantID: blue}, []string{}},
		{"balance lot", ListUnitsOptions{BalanceID: redLot}, redCodes},
		{"order item", ListUnitsOptions{OrderItemID: itemID}, []string{redCodes[0]}},
		{"unknown variant", ListUnitsOptions{VariantID: uuid.NewString()}, []string{}},
		{"unknown order", ListUnitsOptions{OrderID: "no-such-order"}, []string{}},
		{"unknown balance", ListUnitsOptions{BalanceID: "no-such-lot"}, []string{}},
		{"unknown order item", ListUnitsOptions{OrderItemID: uuid.NewString()}, []string{}},
		{"unparsable variant uuid", ListUnitsOptions{VariantID: "not-a-uuid"}, []string{}},
		{"unparsable order item uuid", ListUnitsOptions{OrderItemID: "not-a-uuid"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ListUnits(ctx, db, "", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got.NextCursor != nil {
				t.Fatalf("next_cursor=%v, want nil (all results fit one page)", *got.NextCursor)
			}
			if !slices.Equal(codesOf(got.Units), sorted(tc.want)) {
				t.Fatalf("codes=%v, want %v", codesOf(got.Units), sorted(tc.want))
			}
		})
	}

	// Supplier scoping on the list: s sees only p's units, s2 only p2's.
	got, err := ListUnits(ctx, db, "s", ListUnitsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(codesOf(got.Units), sorted(append(append([]string{}, redCodes...), blueCodes...))) {
		t.Fatalf("supplier s list=%v", codesOf(got.Units))
	}
	got, err = ListUnits(ctx, db, "s2", ListUnitsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(codesOf(got.Units), otherCodes) {
		t.Fatalf("supplier s2 list=%v, want %v", codesOf(got.Units), otherCodes)
	}
	got, err = ListUnits(ctx, db, "s2", ListUnitsOptions{OrderID: "o1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Units) != 0 {
		t.Fatalf("supplier s2 listing another supplier's order must be empty, got %v", codesOf(got.Units))
	}

	// Limit / cursor errors.
	if _, err := ListUnits(ctx, db, "", ListUnitsOptions{Limit: -1}); !errors.Is(err, ErrUnitInvalidLimit) {
		t.Fatalf("negative limit: err=%v, want ErrUnitInvalidLimit", err)
	}
	if _, err := ListUnits(ctx, db, "", ListUnitsOptions{Cursor: "!!!not-base64"}); !errors.Is(err, ErrUnitInvalidCursor) {
		t.Fatalf("bad cursor: err=%v, want ErrUnitInvalidCursor", err)
	}

	// Paging: limit=2 over all units pages evenly with no repeats/skips.
	seen := map[string]bool{}
	var cursor string
	pages := 0
	for {
		page, err := ListUnits(ctx, db, "", ListUnitsOptions{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, u := range page.Units {
			if seen[u.Code] {
				t.Fatalf("cursor repeated code %q", u.Code)
			}
			seen[u.Code] = true
		}
		if page.NextCursor == nil {
			if len(page.Units) != 2 || total%2 != 0 {
				t.Fatalf("last page size=%d, want 2 (total %d, limit 2)", len(page.Units), total)
			}
			break
		}
		cursor = *page.NextCursor
		if pages > 10 {
			t.Fatal("paging did not terminate")
		}
	}
	if pages != total/2 || len(seen) != total {
		t.Fatalf("paging: pages=%d seen=%d, want %d/%d", pages, len(seen), total/2, total)
	}

	// Limit clamps to 200 rather than erroring.
	got, err = ListUnits(ctx, db, "", ListUnitsOptions{Limit: 500})
	if err != nil || len(got.Units) != total {
		t.Fatalf("limit 500: units=%d err=%v, want %d clamped", len(got.Units), err, total)
	}
}
