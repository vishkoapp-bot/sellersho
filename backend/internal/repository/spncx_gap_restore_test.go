package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestSpncxGapCaptureRestore(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	for _, withVariant := range []bool{true, false} {
		name := "variant-less"
		if withVariant {
			name = "legacy-variant"
		}
		t.Run(name, func(t *testing.T) {
			tx := skuTestTx(t, db, schema)
			defer tx.Rollback()
			ctx := context.Background()
			productID := "gap-restore-" + name
			lotID := productID + "-source"
			orderID := productID + "-order"
			if _, err := tx.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version,product_type,origin_supplier_warehouse_id) VALUES($1,'s',2,0,'legacy','sw1')`, productID); err != nil {
				t.Fatal(err)
			}
			variant := ""
			if withVariant {
				variant = uuid.NewString()
				if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,spnc,stock,inventory_mode) VALUES($1,$2,'GAPRESTORE',2,'legacy')`, variant, productID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES($1,$2,'s','supplier','supplier','supplier-warehouse:sw1','sellable',2,TRUE)`, lotID, productID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, orderID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,$3,$4,NULLIF($2,'')::uuid,1)`, uuid.NewString(), variant, orderID, productID); err != nil {
				t.Fatal(err)
			}
			if handled, err := captureOrderInventoryTx(ctx, tx, orderID); err != nil || !handled {
				t.Fatalf("capture=%t err=%v", handled, err)
			}

			const stockAndUnits = `SELECT stock,(SELECT COUNT(*) FROM product_units WHERE product_id=$1) FROM products WHERE id=$1`
			const coverageGaps = `SELECT COUNT(*),COALESCE(MIN(id),'') FROM inventory_movements WHERE product_id=$1 AND metadata->>'unit_coverage_gap'='true'`
			const captureAllocations = `SELECT allocations FROM order_inventory_captures WHERE order_id=$1`
			var stock, units, gaps int
			var gapID string
			if err := tx.QueryRow(stockAndUnits, productID).Scan(&stock, &units); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(coverageGaps, productID).Scan(&gaps, &gapID); err != nil {
				t.Fatal(err)
			}
			if stock != 1 || units != 0 || gaps == 0 || gapID == "" {
				t.Fatalf("capture stock=%d units=%d gaps=%d gap_id=%q", stock, units, gaps, gapID)
			}
			var allocations []byte
			if err := tx.QueryRow(captureAllocations, orderID).Scan(&allocations); err != nil {
				t.Fatal(err)
			}
			// A gap capture records a quantity-only allocation (unit_codes: []).
			// Restore must preserve that entry and leave the order without unit allocations.
			var captured []skuAllocation
			if err := json.Unmarshal(allocations, &captured); err != nil {
				t.Fatalf("decode capture allocations %q: %v", allocations, err)
			}
			if len(captured) != 1 || captured[0].Quantity != 1 || captured[0].BalanceID != lotID || len(captured[0].UnitCodes) != 0 {
				t.Fatalf("capture allocations=%q, want one allocation for balance %q with quantity=1 and no unit codes", allocations, lotID)
			}
			allocationsBefore := bytes.Clone(allocations)

			for attempt := 1; attempt <= 2; attempt++ {
				if handled, err := restoreOrderInventoryTx(ctx, tx, orderID); err != nil || !handled {
					t.Fatalf("restore %d handled=%t err=%v", attempt, handled, err)
				}
				if err := tx.QueryRow(stockAndUnits, productID).Scan(&stock, &units); err != nil {
					t.Fatal(err)
				}
				if stock != 2 || units != 0 {
					t.Fatalf("restore %d stock=%d units=%d, want stock=2 units=0", attempt, stock, units)
				}
				var restoredGapID string
				if err := tx.QueryRow(coverageGaps, productID).Scan(&gaps, &restoredGapID); err != nil {
					t.Fatal(err)
				}
				if gaps != 1 || restoredGapID != gapID {
					t.Fatalf("restore %d gaps=%d gap_id=%q, want gaps=1 gap_id=%q", attempt, gaps, restoredGapID, gapID)
				}
				if err := tx.QueryRow(captureAllocations, orderID).Scan(&allocations); err != nil {
					t.Fatal(err)
				}
				var orderItemUnits int
				if err := tx.QueryRow(`SELECT COUNT(*) FROM order_item_units WHERE order_id=$1`, orderID).Scan(&orderItemUnits); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(allocations, allocationsBefore) || orderItemUnits != 0 {
					t.Fatalf("restore %d allocations=%q order_item_units=%d, want allocations=%q order_item_units=0", attempt, allocations, orderItemUnits, allocationsBefore)
				}
			}
		})
	}
}
