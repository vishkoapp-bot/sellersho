package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Rehearses the Run A part of §9.4: an old variant-less order reaches the
// sole default SKU after conversion, preserves identity on cancel and resale.
func TestSpncxVariantlessCaptureWritesResolvedIdentity(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	variant := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version,product_type,origin_supplier_warehouse_id) VALUES('resolved','s',0,0,'legacy','sw1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,spnc,stock,inventory_mode,is_default) VALUES($1,'resolved','RESOLVED',2,'tracked',TRUE)`, variant); err != nil {
		t.Fatal(err)
	}
	if err := SeedTrackedSKUInventoryTx(ctx, tx, "resolved", "s"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	resolved, err := ResolveLegacyPurchaseVariant(ctx, scoped, "resolved", "")
	if err != nil || resolved != variant {
		t.Fatalf("resolution=%q err=%v", resolved, err)
	}
	origins, err := ResolveInventoryOrigins(ctx, scoped, "resolved", resolved, "supplier_direct", 1)
	if err != nil || len(origins) != 1 || origins[0].SupplierWarehouseID != "sw1" {
		t.Fatalf("origins=%+v err=%v", origins, err)
	}
	tx = skuTestTx(t, db, schema)
	defer tx.Rollback()
	for _, order := range []string{"old-cart", "resale"} {
		item := uuid.NewString()
		if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, order); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,quantity) VALUES($1,$2,'resolved',1)`, item, order); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if handled, err := captureOrderInventoryTx(ctx, tx, order); err != nil || !handled {
				t.Fatalf("capture=%t err=%v", handled, err)
			}
		}
		var itemVariant, captureVariant, code string
		var stock, count int
		if err := tx.QueryRow(`SELECT oi.variant_id::text,c.variant_id::text,u.code,p.stock,(SELECT COUNT(*) FROM order_item_units WHERE order_id=$1)
   FROM order_items oi JOIN order_inventory_captures c ON c.order_item_id=oi.id::text
   JOIN order_item_units u ON u.order_item_id=oi.id JOIN products p ON p.id=oi.product_id WHERE oi.id=$2`, order, item).Scan(&itemVariant, &captureVariant, &code, &stock, &count); err != nil {
			t.Fatal(err)
		}
		if itemVariant != variant || captureVariant != variant || code != "RESOLVED-X0001" || stock != 1 || count != 1 {
			t.Fatalf("identity=%s/%s code=%s stock=%d count=%d", itemVariant, captureVariant, code, stock, count)
		}
		if order == "old-cart" {
			for i := 0; i < 2; i++ {
				if _, err := restoreOrderInventoryTx(ctx, tx, order); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// The resolution pin is deliberate: a sole non-default or V2/variable SKU
	// must never be inferred from an empty client selection.
	for _, q := range []string{
		`UPDATE product_variants SET is_default=FALSE WHERE product_id='resolved'`,
		`UPDATE product_variants SET is_default=TRUE WHERE product_id='resolved'; UPDATE products SET schema_version=2 WHERE id='resolved'`,
		`UPDATE products SET schema_version=0,product_type='variable' WHERE id='resolved'`,
	} {
		if _, err := tx.Exec(q); err != nil {
			t.Fatal(err)
		}
		if got, err := ResolveLegacyPurchaseVariant(ctx, tx, "resolved", ""); err != nil || got != "" {
			t.Fatalf("must not infer variant: %q %v", got, err)
		}
	}
}

func TestSpncxPreBackfillLegacyCapture(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	for _, withVariant := range []bool{false, true} {
		name := "variantless"
		if withVariant {
			name = "legacy-variant"
		}
		t.Run(name, func(t *testing.T) {
			tx := skuTestTx(t, db, schema)
			defer tx.Rollback()
			ctx := context.Background()
			if _, err := tx.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version,product_type,origin_supplier_warehouse_id) VALUES('legacy-sale','s',2,0,'legacy','sw1')`); err != nil {
				t.Fatal(err)
			}
			variant := ""
			if withVariant {
				variant = uuid.NewString()
				if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,spnc,stock,inventory_mode) VALUES($1,'legacy-sale','LEGACYSALE',2,'legacy')`, variant); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES('legacy-source','legacy-sale','s','supplier','supplier','supplier-warehouse:sw1','sellable',2,TRUE)`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO orders(id) VALUES('legacy-sale-order')`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,'legacy-sale-order','legacy-sale',NULLIF($2,'')::uuid,1)`, uuid.NewString(), variant); err != nil {
				t.Fatal(err)
			}
			if handled, err := captureOrderInventoryTx(ctx, tx, "legacy-sale-order"); err != nil || !handled {
				t.Fatalf("capture=%t err=%v", handled, err)
			}
			var stock, units, gaps int
			if err := tx.QueryRow(`SELECT stock,(SELECT COUNT(*) FROM product_units WHERE product_id='legacy-sale'),(SELECT COUNT(*) FROM inventory_movements WHERE product_id='legacy-sale' AND metadata->>'unit_coverage_gap'='true') FROM products WHERE id='legacy-sale'`).Scan(&stock, &units, &gaps); err != nil {
				t.Fatal(err)
			}
			if stock != 1 || units != 0 || gaps == 0 {
				t.Fatalf("legacy stock=%d units=%d gaps=%d", stock, units, gaps)
			}
			if _, err := restoreOrderInventoryTx(ctx, tx, "legacy-sale-order"); err != nil {
				t.Fatal(err)
			}
			if err := tx.QueryRow(`SELECT stock FROM products WHERE id='legacy-sale'`).Scan(&stock); err != nil || stock != 2 {
				t.Fatalf("restore stock=%d err=%v", stock, err)
			}
		})
	}
}

func TestSpncxQuantityOnlyReturnCaptureBoundary(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	for _, mode := range []string{"legacy", "tracked", "pre-cutover"} {
		t.Run(mode, func(t *testing.T) {
			tx := skuTestTx(t, db, schema)
			defer tx.Rollback()
			ctx := context.Background()
			variant, item, ret, returned := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
			if _, err := tx.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version,product_type,origin_supplier_warehouse_id) VALUES('return-boundary','s',2,0,'legacy','sw1')`); err != nil {
				t.Fatal(err)
			}
			inventoryMode := "legacy"
			if mode == "tracked" {
				inventoryMode = "tracked"
			}
			if _, err := tx.Exec(`INSERT INTO product_variants(id,product_id,spnc,stock,inventory_mode) VALUES($1,'return-boundary','RETURNBOUND',2,$2)`, variant, inventoryMode); err != nil {
				t.Fatal(err)
			}
			lotVariant := ""
			if mode == "tracked" {
				lotVariant = variant
			}
			if _, err := tx.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES('return-source','return-boundary','s',NULLIF($1,'')::uuid,'supplier','supplier','supplier-warehouse:sw1','sellable',2,TRUE)`, lotVariant); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO orders(id) VALUES('return-boundary-order')`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,'return-boundary-order','return-boundary',$2,2)`, item, variant); err != nil {
				t.Fatal(err)
			}
			if mode != "pre-cutover" {
				if _, err := captureOrderInventoryTx(ctx, tx, "return-boundary-order"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(`INSERT INTO customer_returns(id,order_id) VALUES($1,'return-boundary-order')`, ret); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO return_items(id,return_id,order_item_id,qty,qc_status) VALUES($1,$2,$3,2,'ok')`, returned, ret, item); err != nil {
				t.Fatal(err)
			}
			for repeat := 0; repeat < 2; repeat++ {
				if handled, err := restoreCapturedReturnTx(ctx, tx, returned, item, "return-boundary", "s", 2, "ok", "warehouse:w1", ""); err != nil || !handled {
					t.Fatalf("return handled=%t err=%v", handled, err)
				}
			}
			var stock, units, holding, restored, gaps int
			if err := tx.QueryRow(`SELECT stock,
    (SELECT COUNT(*) FROM product_units WHERE product_id='return-boundary'),
    (SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id='return-boundary' AND location_key=$1),
    (SELECT COALESCE(SUM(restored_quantity),0) FROM order_inventory_captures WHERE order_item_id=$2),
    (SELECT COUNT(*) FROM inventory_movements WHERE product_id='return-boundary' AND metadata->>'unit_coverage_gap'='true')
    FROM products WHERE id='return-boundary'`, "return:"+ret, item).Scan(&stock, &units, &holding, &restored, &gaps); err != nil {
				t.Fatal(err)
			}
			wantHolding, wantRestored := 0, 2
			if mode == "pre-cutover" {
				wantHolding, wantRestored = 2, 0
			}
			if stock != 2 || units != 0 || holding != wantHolding || restored != wantRestored || gaps == 0 {
				t.Fatalf("stock=%d units=%d holding=%d restored=%d gaps=%d", stock, units, holding, restored, gaps)
			}
		})
	}
}
