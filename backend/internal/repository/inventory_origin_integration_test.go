package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"testing"
)

func TestAuditSupplyWarehouseOriginCannotCaptureSupplierOnlyLot(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	id := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'origin-order','p',$2,1)`, id, red); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE order_item_origins SET origin_type='warehouse',warehouse_id='w1',supplier_warehouse_id=NULL WHERE order_item_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	_, err := captureOrderInventoryTx(context.Background(), tx, "origin-order")
	if !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("warehouse origin captured supplier-only lot: %v", err)
	}
}

func TestInventoryOriginExactAllocationAndRestore(t *testing.T) {
	for _, tc := range []struct {
		name, kind, warehouse, swh, lotKind, lotWarehouse, lotSwh string
		inactive, wantErr                                         bool
	}{
		{"warehouse to supplier rejects", "supplier", "", "sw1", "warehouse", "w1", "", false, true},
		{"other warehouse rejects", "warehouse", "w2", "", "warehouse", "w1", "", false, true},
		{"other supplier warehouse rejects", "supplier", "", "sw2", "supplier", "", "sw1", false, true},
		{"unknown warehouse rejects", "warehouse", "w1", "", "warehouse", "", "", false, true},
		{"inactive warehouse rejects", "warehouse", "w1", "", "warehouse", "w1", "", true, true},
		{"inactive supplier warehouse rejects", "supplier", "", "sw1", "supplier", "", "sw1", true, true},
		{"warehouse exact", "warehouse", "w1", "", "warehouse", "w1", "", false, false},
		{"supplier exact", "supplier", "", "sw1", "supplier", "", "sw1", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, schema, red, _ := skuInventoryFixture(t)
			tx := skuTestTx(t, db, schema)
			defer tx.Rollback()
			ctx := context.Background()
			id := uuid.NewString()
			if _, err := tx.Exec(`UPDATE inventory_balances SET location_type=$1,warehouse_id=NULLIF($2,''),supplier_warehouse_id=NULLIF($3,'') WHERE variant_id=$4`, tc.lotKind, tc.lotWarehouse, tc.lotSwh, red); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'exact','p',$2,1)`, id, red); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`UPDATE order_item_origins SET origin_type=$2,warehouse_id=NULLIF($3,''),supplier_warehouse_id=NULLIF($4,'') WHERE order_item_id=$1`, id, tc.kind, tc.warehouse, tc.swh); err != nil {
				t.Fatal(err)
			}
			if tc.inactive {
				if _, err := tx.Exec(`UPDATE warehouses SET is_active=FALSE;UPDATE supplier_warehouses SET is_active=FALSE`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(`UPDATE products SET origin_supplier_warehouse_id='sw2' WHERE id='p'`); err != nil {
				t.Fatal(err)
			}
			_, err := captureOrderInventoryTx(ctx, tx, "exact")
			if tc.wantErr {
				if !errors.Is(err, ErrInsufficientInventory) {
					t.Fatalf("expected shortage: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// Catalog origin edits after checkout do not relocate the captured lot.
			if _, err := tx.Exec(`UPDATE products SET origin_supplier_warehouse_id='sw2' WHERE id='p'`); err != nil {
				t.Fatal(err)
			}
			if _, err := captureOrderInventoryTx(ctx, tx, "exact"); err != nil {
				t.Fatal(err)
			}
			var qty int
			if err := tx.QueryRow(`SELECT SUM(quantity) FROM inventory_balances WHERE variant_id=$1`, red).Scan(&qty); err != nil || qty != 0 {
				t.Fatalf("replay debited: %d %v", qty, err)
			}
			for i := 0; i < 2; i++ {
				if _, err := restoreOrderInventoryTx(ctx, tx, "exact"); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.QueryRow(`SELECT SUM(quantity) FROM inventory_balances WHERE variant_id=$1 AND COALESCE(warehouse_id,'')=$2 AND COALESCE(supplier_warehouse_id,'')=$3`, red, tc.lotWarehouse, tc.lotSwh).Scan(&qty); err != nil || qty != 1 {
				t.Fatalf("wrong restored origin: %d %v", qty, err)
			}
		})
	}
}

func TestInventoryOriginMultiLegAndSplitConservation(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	ctx := context.Background()
	id := uuid.NewString()
	for _, q := range []string{`UPDATE inventory_balances SET location_type='warehouse',warehouse_id='w1',supplier_warehouse_id=NULL WHERE variant_id='` + red + `'`,
		`INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,reconciled,warehouse_id) VALUES('other','p','s','` + red + `','supplier','warehouse','w2','sellable',1,TRUE,'w2')`} {
		if _, err := tx.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'split','p',$2,2)`, id, red); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM order_item_origins WHERE order_item_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_item_origins VALUES('leg1','split',$1,0,'warehouse','s','w1',NULL,1),('leg2','split',$1,1,'warehouse','s','w2',NULL,1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(ctx, tx, "split"); err != nil {
		t.Fatal(err)
	}
	var legs int
	if err := tx.QueryRow(`SELECT COUNT(DISTINCT a->>'originId') FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a WHERE c.order_item_id=$1`, id).Scan(&legs); err != nil || legs != 2 {
		t.Fatalf("split allocations %d %v", legs, err)
	}
	if _, err := restoreOrderInventoryTx(ctx, tx, "split"); err != nil {
		t.Fatal(err)
	}
	var restored int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM inventory_balances WHERE variant_id=$1 AND quantity=1`, red).Scan(&restored); err != nil || restored != 2 {
		t.Fatalf("split restore %d %v", restored, err)
	}
}

func TestInventoryOriginCheckoutSplitAndFutureDeclaration(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	if _, err := tx.Exec(`UPDATE products SET origin_supplier_warehouse_id='sw2' WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	if err := SetTrackedSKUStockTx(context.Background(), tx, "p", "s", red, 2, "future declared stock", ""); err != nil {
		t.Fatal(err)
	}
	var old, newQty int
	if err := tx.QueryRow(`SELECT SUM(quantity) FILTER(WHERE supplier_warehouse_id='sw1'),SUM(quantity) FILTER(WHERE supplier_warehouse_id='sw2') FROM inventory_balances WHERE variant_id=$1`, red).Scan(&old, &newQty); err != nil || old != 1 || newQty != 1 {
		t.Fatalf("origin edit relocated history old=%d new=%d err=%v", old, newQty, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	plan, err := ResolveInventoryOrigins(context.Background(), scoped, "p", red, "supplier_direct", 2)
	if err != nil || len(plan) != 2 || plan[0].Quantity != 1 || plan[1].Quantity != 1 {
		t.Fatalf("checkout origin split: %+v %v", plan, err)
	}
}

func TestInventoryOriginRejectsMalformedSplits(t *testing.T) {
	for _, kind := range []string{"missing", "short", "excess", "legacy", "foreign-supplier"} {
		t.Run(kind, func(t *testing.T) {
			db, schema, red, _ := skuInventoryFixture(t)
			tx := skuTestTx(t, db, schema)
			defer tx.Rollback()
			id := uuid.NewString()
			if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,'bad-split','p',$2,1)`, id, red); err != nil {
				t.Fatal(err)
			}
			q := map[string]string{"missing": `DELETE FROM order_item_origins WHERE order_item_id=$1`, "short": `UPDATE order_item_origins SET quantity=0 WHERE order_item_id=$1`, "excess": `UPDATE order_item_origins SET quantity=2 WHERE order_item_id=$1`, "legacy": `UPDATE order_item_origins SET origin_type='legacy' WHERE order_item_id=$1`, "foreign-supplier": `UPDATE order_item_origins SET supplier_id='foreign' WHERE order_item_id=$1`}[kind]
			if _, err := tx.Exec(q, id); err != nil {
				t.Fatal(err)
			}
			if _, err := captureOrderInventoryTx(context.Background(), tx, "bad-split"); !errors.Is(err, ErrInsufficientInventory) {
				t.Fatalf("bad origin accepted: %v", err)
			}
		})
	}
}

func TestInventoryOriginShippingConsumesEveryWarehouseSplit(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			db, schema, red, _ := skuInventoryFixture(t)
			tx := skuTestTx(t, db, schema)
			defer tx.Rollback()
			id := uuid.NewString()
			if _, err := tx.Exec(`INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,qty) VALUES('pos1','p','s','w1',1),('pos2','p','s','w2',1)`); err != nil {
				t.Fatal(err)
			}
			if short {
				if _, err := tx.Exec(`DELETE FROM warehouse_storage_positions WHERE warehouse_id='w2'`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,'ship-split','p',$2,2)`, id, red); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`DELETE FROM order_item_origins WHERE order_item_id=$1`, id); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`INSERT INTO order_item_origins VALUES('ship1','ship-split',$1,0,'warehouse','s','w1',NULL,1),('ship2','ship-split',$1,1,'warehouse','s','w2',NULL,1)`, id); err != nil {
				t.Fatal(err)
			}
			err := consumeShippedOrderPositionsTx(context.Background(), tx, "ship-split")
			if short {
				if !errors.Is(err, ErrInsufficientInventory) {
					t.Fatalf("missing split position accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var remaining int
			if err := tx.QueryRow(`SELECT SUM(qty) FROM warehouse_storage_positions`).Scan(&remaining); err != nil || remaining != 0 {
				t.Fatalf("ignored second warehouse remaining=%d err=%v", remaining, err)
			}
		})
	}
}

func TestInventoryOriginReceiptUsesDeclaredCartonSource(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	shipment := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO shipments(id,warehouse_id,supplier_warehouse_id) VALUES($1,'w1','sw1')`, shipment); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,1)`, shipment, red); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE products SET origin_supplier_warehouse_id='sw2' WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	if err := SetTrackedSKUStockTx(context.Background(), tx, "p", "s", red, 2, "new origin declaration", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveTrackedSKUShipmentItemTx(context.Background(), tx, shipment, red, 1, "passed"); err != nil {
		t.Fatal(err)
	}
	var oldQty, newQty int
	if err := tx.QueryRow(`SELECT SUM(quantity) FILTER(WHERE supplier_warehouse_id='sw1'),SUM(quantity) FILTER(WHERE supplier_warehouse_id='sw2') FROM inventory_balances WHERE variant_id=$1`, red).Scan(&oldQty, &newQty); err != nil || oldQty != 0 || newQty != 1 {
		t.Fatalf("receipt used today's origin: old=%d new=%d err=%v", oldQty, newQty, err)
	}
}

func TestInventoryOriginLegacyDeclarationUsesNamedFutureLot(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO products(id,supplier_id,stock,schema_version,origin_supplier_warehouse_id) VALUES('legacy','s',0,0,'sw2');INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity) VALUES('old-unknown','legacy','s','supplier','supplier','supplier:s','sellable',3)`); err != nil {
		t.Fatal(err)
	}
	if err := setSupplierSellableTotalTx(context.Background(), tx, "legacy", "s", 2); err != nil {
		t.Fatal(err)
	}
	var oldQty, newQty int
	if err := tx.QueryRow(`SELECT SUM(quantity) FILTER(WHERE supplier_warehouse_id IS NULL),COALESCE(SUM(quantity) FILTER(WHERE supplier_warehouse_id='sw2'),0) FROM inventory_balances WHERE product_id='legacy'`).Scan(&oldQty, &newQty); err != nil || oldQty != 3 || newQty != 2 {
		t.Fatalf("legacy future declaration old=%d named=%d err=%v", oldQty, newQty, err)
	}
}

func TestInventoryOriginTapinRegistrationReadsPersistedSplits(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	id := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,'tapin-split','p',$2,2)`, id, red); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE order_item_origins SET quantity=1 WHERE order_item_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_item_origins VALUES('tapin2','tapin-split',$1,1,'supplier','s',NULL,'sw2',1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE products SET origin_supplier_warehouse_id='sw2' WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	lines, err := NewTapinRepository(skuScopedDB(t, schema)).ListSupplierOrderLines(context.Background(), "tapin-split", "s")
	if err != nil || len(lines) != 2 || lines[0].OriginWarehouseID != "sw1" || lines[1].OriginWarehouseID != "sw2" || lines[0].Quantity != 1 || lines[1].Quantity != 1 {
		t.Fatalf("registration changed persisted split: %+v %v", lines, err)
	}
}

func TestInventoryOriginPutawayCreatesShippingCoverage(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	shipment := uuid.NewString()

	if _, err := tx.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,1)`, shipment, red); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveTrackedSKUShipmentItemTx(context.Background(), tx, shipment, red, 1, "passed"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := putawayTrackedSKUInventoryTx(context.Background(), tx, "p", "s", red, 1, "shipment:"+shipment, "A-1", "coverage-task", ""); err != nil {
			t.Fatal(err)
		}
	}
	var qty int
	if err := tx.QueryRow(`SELECT COALESCE(SUM(qty),0) FROM warehouse_storage_positions WHERE warehouse_id='w1'`).Scan(&qty); err != nil || qty != 1 {
		t.Fatalf("putaway shipping coverage=%d err=%v", qty, err)
	}
}

func TestInventoryOriginReceiptRejectsUnknownSource(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	shipment := uuid.NewString()
	if _, err := tx.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty) VALUES($1,$2,1)`, shipment, red); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE shipments SET supplier_warehouse_id=NULL WHERE id=$1`, shipment); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveTrackedSKUShipmentItemTx(context.Background(), tx, shipment, red, 1, "passed"); err == nil {
		t.Fatal("unknown source invented physical provenance")
	}
}

func TestInventoryOriginProviderRequiresOnePersistedPickup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lines   []SupplierOrderLine
		wantErr bool
	}{
		{"single", []SupplierOrderLine{{OriginWarehouseID: "sw1", Quantity: 1}, {OriginWarehouseID: "sw1", Quantity: 2}}, false},
		{"multiple", []SupplierOrderLine{{OriginWarehouseID: "sw1", Quantity: 1}, {OriginWarehouseID: "sw2", Quantity: 1}}, true},
		{"unknown", []SupplierOrderLine{{Quantity: 1}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SingleSupplierPickupOrigin(tc.lines)
			if (err != nil) != tc.wantErr {
				t.Fatalf("pickup validation error=%v", err)
			}
		})
	}
}

func TestInventoryOriginCartonCreationPersistsSourceThroughReceipt(t *testing.T) {
	db, schema, red, _ := skuInventoryFixture(t)
	scoped := skuScopedDB(t, schema)
	ctx := context.Background()
	carton, err := NewSupplierRepository(scoped).CreateCartonShipment(ctx, "s", []CartonShipmentItem{{VariantID: red, ExpectedQty: 1}}, "w1")
	if err != nil {
		t.Fatal(err)
	}
	var source, destination string
	if err := scoped.QueryRow(`SELECT supplier_warehouse_id,warehouse_id FROM shipments WHERE id=$1`, carton.ID).Scan(&source, &destination); err != nil || source != "sw1" || destination != "w1" {
		t.Fatalf("carton source=%s destination=%s err=%v", source, destination, err)
	}
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE products SET origin_supplier_warehouse_id='sw2' WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	if err := SetTrackedSKUStockTx(ctx, tx, "p", "s", red, 2, "future source change", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ReceiveTrackedSKUShipmentItemTx(ctx, tx, carton.ID, red, 1, "passed"); err != nil {
		t.Fatal(err)
	}
	var oldQty, newQty int
	if err := tx.QueryRow(`SELECT COALESCE(SUM(quantity) FILTER(WHERE supplier_warehouse_id='sw1'),0),COALESCE(SUM(quantity) FILTER(WHERE supplier_warehouse_id='sw2'),0) FROM inventory_balances WHERE variant_id=$1`, red).Scan(&oldQty, &newQty); err != nil || oldQty != 0 || newQty != 1 {
		t.Fatalf("created carton moved today's source old=%d new=%d err=%v", oldQty, newQty, err)
	}
}

func TestInventoryOriginLegacyInboundPreservesBirthSource(t *testing.T) {
	for _, mode := range []string{"record", "complete", "unknown", "short"} {
		t.Run(mode, func(t *testing.T) {
			db := operationsDB(t)
			f := opsFixtureNew(t, db)
			ctx := context.Background()
			sup := NewSupplierRepository(db)
			wh := NewWarehouseRepository(db)
			warehouse, old, next := "origin-w-"+uuid.NewString(), "origin-sw-"+uuid.NewString(), "origin-next-"+uuid.NewString()
			opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, warehouse)
			opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$3,$1,'p','c'),($2,$3,$2,'p','c')`, old, next, f.supplier)
			opsExec(t, db, `UPDATE products SET origin_supplier_warehouse_id=$1,stock=0 WHERE id=$2`, old, f.product)
			if err := sup.UpdateProductStock(ctx, f.supplier, f.product, 2, "declared birth source"); err != nil {
				t.Fatal(err)
			}
			inbound, err := sup.CreateInbound(ctx, f.supplier, map[string]interface{}{"warehouseId": warehouse, "items": []interface{}{map[string]interface{}{"product_id": f.product, "quantity": float64(2), "carton_count": float64(1)}}})
			if err != nil {
				t.Fatal(err)
			}
			var bornSource string
			if err := db.QueryRow(`SELECT supplier_warehouse_id FROM inbound_shipments WHERE id=$1`, inbound).Scan(&bornSource); err != nil || bornSource != old {
				t.Fatalf("inbound source snapshot=%s err=%v", bornSource, err)
			}
			if err := sup.UpdateProductOrigin(ctx, f.supplier, f.product, next); err != nil {
				t.Fatal(err)
			}
			if err := sup.UpdateProductStock(ctx, f.supplier, f.product, 3, "future source declaration"); err != nil {
				t.Fatal(err)
			}
			if err := sup.DispatchInbound(ctx, f.supplier, inbound); err != nil {
				t.Fatal(err)
			}
			if err := wh.StartReceiving(ctx, inbound, f.user); err != nil {
				t.Fatal(err)
			}
			if _, err := wh.ScanItem(ctx, inbound, map[string]interface{}{"productId": f.product, "quantity": float64(2)}, f.user); err != nil {
				t.Fatal(err)
			}
			if err := wh.CompleteInbound(ctx, inbound, f.user); err != nil {
				t.Fatal(err)
			}
			if mode == "unknown" {
				opsExec(t, db, `UPDATE inbound_shipments SET supplier_warehouse_id=NULL WHERE id=$1`, inbound)
			}
			if mode == "short" {
				opsExec(t, db, `UPDATE inventory_balances SET quantity=0 WHERE product_id=$1 AND supplier_warehouse_id=$2`, f.product, old)
			}
			qc := func() error {
				if mode == "complete" {
					return wh.CompleteQC(ctx, f.product, inbound, f.user, 2, 0, 0, "exact birth source")
				}
				return wh.RecordQCPass(ctx, f.product, inbound, f.user, 2)
			}
			err = qc()
			if mode == "unknown" || mode == "short" {
				if !errors.Is(err, ErrInsufficientInventory) {
					t.Fatalf("unresolved inbound source accepted: %v", err)
				}
				var sellable, physical, qcRows int
				if err := db.QueryRow(`SELECT COALESCE(SUM(quantity) FILTER(WHERE warehouse_id=$2 AND status='sellable'),0),COALESCE(SUM(quantity) FILTER(WHERE status='physical'),0) FROM inventory_balances WHERE product_id=$1`, f.product, warehouse).Scan(&sellable, &physical); err != nil || sellable != 0 || physical != 2 {
					t.Fatalf("failed QC mutated physical=%d sellable=%d err=%v", physical, sellable, err)
				}
				if err := db.QueryRow(`SELECT COUNT(*) FROM qc_results WHERE inbound_id=$1`, inbound).Scan(&qcRows); err != nil || qcRows != 0 {
					t.Fatalf("failed QC committed row=%d err=%v", qcRows, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var oldQty, nextQty, warehouseQty, total int
			if err := db.QueryRow(`SELECT COALESCE(SUM(quantity) FILTER(WHERE supplier_warehouse_id=$2),0),COALESCE(SUM(quantity) FILTER(WHERE supplier_warehouse_id=$3),0),COALESCE(SUM(quantity) FILTER(WHERE warehouse_id=$4 AND status='sellable'),0),COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1`, f.product, old, next, warehouse).Scan(&oldQty, &nextQty, &warehouseQty, &total); err != nil || oldQty != 0 || nextQty != 3 || warehouseQty != 2 || total != 5 {
				t.Fatalf("inbound duplicated or redirected stock old=%d next=%d warehouse=%d total=%d err=%v", oldQty, nextQty, warehouseQty, total, err)
			}
			if err := qc(); err != nil {
				t.Fatal(err)
			}
			var replayTotal int
			if err := db.QueryRow(`SELECT SUM(quantity) FROM inventory_balances WHERE product_id=$1`, f.product).Scan(&replayTotal); err != nil || replayTotal != 5 {
				t.Fatalf("QC replay minted stock=%d err=%v", replayTotal, err)
			}
		})
	}

}
