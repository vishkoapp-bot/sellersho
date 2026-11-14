package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/drup/platform/internal/warehouseopt"
	"github.com/google/uuid"
)

func inv09ReceiveLegacy(t *testing.T, db *sql.DB, f opsFixture, warehouse string, quantity int) string {
	t.Helper()
	ctx := context.Background()
	source := "sw-" + uuid.NewString()
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c') ON CONFLICT(id) DO NOTHING`, warehouse)
	opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'p','c')`, source, f.supplier)
	opsExec(t, db, `UPDATE products SET origin_supplier_warehouse_id=$1 WHERE id=$2`, source, f.product)
	opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,'supplier','supplier',$4,'sellable',$5,$6)`, uuid.NewString(), f.product, f.supplier, "supplier-warehouse:"+source, quantity, source)
	sup, wh := NewSupplierRepository(db), NewWarehouseRepository(db)
	inbound, err := sup.CreateInbound(ctx, f.supplier, map[string]interface{}{"warehouseId": warehouse, "items": []interface{}{map[string]interface{}{"product_id": f.product, "quantity": float64(quantity), "carton_count": float64(1)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = sup.DispatchInbound(ctx, f.supplier, inbound); err != nil {
		t.Fatal(err)
	}
	if err = wh.StartReceiving(ctx, inbound, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err = wh.ScanItem(ctx, inbound, map[string]interface{}{"productId": f.product, "quantity": float64(quantity)}, f.user); err != nil {
		t.Fatal(err)
	}
	if err = wh.CompleteInbound(ctx, inbound, f.user); err != nil {
		t.Fatal(err)
	}
	return inbound
}

func TestINV09PartialAcceptanceSourceGuardAndConcurrentReplay(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	warehouse := "w-" + uuid.NewString()
	inbound := inv09ReceiveLegacy(t, db, f, warehouse, 10)
	wh := NewWarehouseRepository(db)
	if err := wh.CompleteQC(ctx, f.product, inbound, f.user, 3, 2, 0, "partial counted"); err != nil {
		t.Fatal(err)
	}
	var task, source string
	if err := db.QueryRow(`SELECT task_id,source_location_key FROM inbound_putaway_allocations WHERE shipment_id=$1`, inbound).Scan(&task, &source); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE inbound_putaway_allocations SET quantity=quantity+1 WHERE task_id=$1`, `DELETE FROM inbound_putaway_allocations WHERE task_id=$1`, `UPDATE putaway_tasks SET quantity=quantity+1 WHERE id=$1`, `DELETE FROM putaway_tasks WHERE id=$1`, `UPDATE putaway_tasks SET status='completed' WHERE id=$1`} {
		if _, err := db.Exec(q, task); err == nil {
			t.Fatalf("allocation identity guard bypassed: %s", q)
		}
	}
	bin := "BC-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,quantity,warehouse_id,warehouse_name,capacity_volume_cm3,max_weight_grams,capacity_status) VALUES($1,'test','1','1',0,$2,$3,100000,100000,'verified')`, bin, warehouse, warehouse)
	if err := wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE warehouse_bins SET max_weight_grams=1 WHERE bin_code=$1`, bin)
	if err := wh.ConfirmPutaway(ctx, task, f.user); err == nil {
		t.Fatal("reduced bin capacity accepted stale reservation")
	}
	opsExec(t, db, `UPDATE warehouse_bins SET max_weight_grams=100000 WHERE bin_code=$1`, bin)
	opsExec(t, db, `UPDATE inventory_balances SET quantity=2 WHERE location_key=$1`, source)
	if err := wh.ConfirmPutaway(ctx, task, f.user); !errors.Is(err, ErrInboundProvenance) {
		t.Fatalf("short acceptance source consumed: %v", err)
	}
	var binQty, stock int
	var status string
	if err := db.QueryRow(`SELECT quantity FROM warehouse_bins WHERE bin_code=$1`, bin).Scan(&binQty); err != nil || binQty != 0 {
		t.Fatalf("failed placement mutated bin=%d err=%v", binQty, err)
	}
	if err := db.QueryRow(`SELECT status FROM putaway_tasks WHERE id=$1`, task).Scan(&status); err != nil || status != "assigned" {
		t.Fatalf("failed placement status=%s err=%v", status, err)
	}
	opsExec(t, db, `UPDATE inventory_balances SET quantity=3 WHERE location_key=$1`, source)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- wh.ConfirmPutaway(ctx, task, f.user) }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var moves int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE idempotency_key=$1`, "putaway:"+task).Scan(&moves); err != nil || moves != 1 {
		t.Fatalf("concurrent placement movements=%d err=%v", moves, err)
	}
	if err := db.QueryRow(`SELECT quantity FROM warehouse_bins WHERE bin_code=$1`, bin).Scan(&binQty); err != nil || binQty != 3 {
		t.Fatalf("concurrent bin quantity=%d err=%v", binQty, err)
	}
	if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock); err != nil || stock != 3 {
		t.Fatalf("concurrent replay stock=%d err=%v", stock, err)
	}
	if err := wh.CompleteQC(ctx, f.product, inbound, f.user, 8, 2, 0, "remaining counted"); err != nil {
		t.Fatal(err)
	}
	var tasks, total int
	if err := db.QueryRow(`SELECT COUNT(*),SUM(quantity) FROM inbound_putaway_allocations WHERE shipment_id=$1`, inbound).Scan(&tasks, &total); err != nil || tasks != 2 || total != 8 {
		t.Fatalf("partial tasks=%d quantity=%d err=%v", tasks, total, err)
	}
	inv09PlacePending(t, db, f, inbound, warehouse)
	if err := wh.CompleteQC(ctx, f.product, inbound, f.user, 8, 2, 0, "remaining counted"); err != nil {
		t.Fatal(err)
	}
	var damaged, accepted, physical int
	if err := db.QueryRow(`SELECT SUM(quantity),COALESCE(SUM(quantity) FILTER(WHERE status='damaged'),0),COALESCE(SUM(quantity) FILTER(WHERE status='qc_accepted'),0) FROM inventory_balances WHERE product_id=$1`, f.product).Scan(&physical, &damaged, &accepted); err != nil || physical != 10 || damaged != 2 || accepted != 0 {
		t.Fatalf("physical=%d damaged=%d staging=%d err=%v", physical, damaged, accepted, err)
	}
	if err := db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock); err != nil || stock != 8 {
		t.Fatalf("partial final stock=%d err=%v", stock, err)
	}
	down, err := os.ReadFile("../../migrations/202611140009_exact_inbound_putaway.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(string(down)); err == nil {
		tx.Rollback()
		t.Fatal("rollback erased used physical provenance")
	}
	tx.Rollback()
}

func inv09ReceiveTracked(t *testing.T, quantity int) (*sql.DB, opsFixture, string, string, string, string) {
	t.Helper()
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	warehouse, source, red, blue := "w-"+uuid.NewString(), "sw-"+uuid.NewString(), uuid.NewString(), uuid.NewString()
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, warehouse)
	opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'p','c')`, source, f.supplier)
	opsExec(t, db, `UPDATE products SET schema_version=2,product_type='variable',attribute_schema='{}',origin_supplier_warehouse_id=$1 WHERE id=$2`, source, f.product)
	opsExec(t, db, `INSERT INTO product_variants(id,product_id,spnc,inventory_mode,stock) VALUES($1,$2,$3,'tracked',$6),($4,$2,$5,'tracked',3)`, red, f.product, "R"+red[:10], blue, "B"+blue[:10], quantity)
	for _, lot := range []struct {
		id       string
		quantity int
	}{{red, quantity}, {blue, 3}} {
		opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,$4,'supplier','supplier',$5,'sellable',$6,$7)`, uuid.NewString(), f.product, f.supplier, lot.id, "supplier-warehouse:"+source, lot.quantity, source)
	}
	sup := NewSupplierRepository(db)
	shipment, err := sup.CreateCartonShipment(ctx, f.supplier, []CartonShipmentItem{{VariantID: red, ExpectedQty: quantity}}, warehouse, "inv09-tracked")
	if err != nil {
		t.Fatal(err)
	}
	if err = sup.ChangeCartonDispatch(ctx, f.supplier, shipment.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = sup.ReceiveWarehouseShipment(ctx, shipment.ShipmentCode, []WarehouseReceiveItem{{VariantID: red, ReceivedQty: quantity, QCStatus: "pass"}}); err != nil {
		t.Fatal(err)
	}
	return db, f, warehouse, shipment.ID, red, blue
}

func TestINV09TrackedReceiptUsesSameExactPutaway(t *testing.T) {
	db, f, warehouse, shipment, red, blue := inv09ReceiveTracked(t, 10)
	ctx := context.Background()
	wh := NewWarehouseRepository(db)
	var err error
	if err = wh.RecordQCPass(ctx, f.product, shipment, f.user, 10); err != nil {
		t.Fatal(err)
	}
	order, token := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, red)
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	orders := NewOrderRepository(db)
	if _, err = orders.MarkPaymentPaid(ctx, token, "pre-tracked", "mask"); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("unplaced SKU captured: %v", err)
	}
	inv09PlacePending(t, db, f, shipment, warehouse)
	if _, err = orders.MarkPaymentPaid(ctx, token, "R"+red[:20], "mask"); err != nil {
		t.Fatal(err)
	}
	var r, b int
	if err = db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&r); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, blue).Scan(&b); err != nil || r != 9 || b != 3 {
		t.Fatalf("tracked=%d sibling=%d err=%v", r, b, err)
	}
}

func inv09PlacePending(t *testing.T, db *sql.DB, f opsFixture, inbound, warehouse string) {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM putaway_tasks WHERE inbound_id=$1 AND status='pending' ORDER BY id COLLATE "C"`, inbound)
	if err != nil {
		t.Fatal(err)
	}
	var tasks []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		tasks = append(tasks, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 {
		t.Fatal("no exact putaway tasks")
	}
	wh := NewWarehouseRepository(db)
	for _, task := range tasks {
		bin := "BP-" + uuid.NewString()[:12]
		opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,quantity,warehouse_id,warehouse_name,capacity_volume_cm3,max_weight_grams,capacity_status,position_x,position_y,position_z) VALUES($1,'test','1','1',0,$2,$3,1000000000,1000000000,'verified',0,0,0)`, bin, warehouse, warehouse)
		suggestions, err := wh.SuggestBins(context.Background(), task, warehouseopt.Point{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, candidate := range suggestions {
			if candidate.BinCode == bin {
				found = true
			}
		}
		if !found {
			t.Fatalf("exact destination bin missing from suggestions: %+v", suggestions)
		}
		if err = wh.AssignPutawayTask(context.Background(), task, bin, f.user); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err = wh.ConfirmPutaway(context.Background(), task, f.user); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Exercise the actual legacy ASN/QC/assignment/confirmation/payment endpoints
// against the migrated schema. QC cannot make staged goods purchasable.
func TestINV09LegacyQCAwaitsExactPutaway(t *testing.T) {
	for _, endpoint := range []string{"record", "complete"} {
		t.Run(endpoint, func(t *testing.T) {
			db := operationsDB(t)
			f := opsFixtureNew(t, db)
			ctx := context.Background()
			wh, source, bin := "w-"+uuid.NewString(), "sw-"+uuid.NewString(), "B-"+uuid.NewString()[:12]
			opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, wh)
			opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES($1,$2,$1,'p','c')`, source, f.supplier)
			opsExec(t, db, `UPDATE products SET origin_supplier_warehouse_id=$1 WHERE id=$2`, source, f.product)
			opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,$2,$3,'supplier','supplier',$4,'sellable',13,$5)`, uuid.NewString(), f.product, f.supplier, "supplier-warehouse:"+source, source)
			sup, warehouse := NewSupplierRepository(db), NewWarehouseRepository(db)
			inbound, err := sup.CreateInbound(ctx, f.supplier, map[string]interface{}{"warehouseId": wh, "items": []interface{}{map[string]interface{}{"product_id": f.product, "quantity": float64(10), "carton_count": float64(1)}}})
			if err != nil {
				t.Fatal(err)
			}
			if err = sup.DispatchInbound(ctx, f.supplier, inbound); err != nil {
				t.Fatal(err)
			}
			if err = warehouse.StartReceiving(ctx, inbound, f.user); err != nil {
				t.Fatal(err)
			}
			if _, err = warehouse.ScanItem(ctx, inbound, map[string]interface{}{"productId": f.product, "quantity": float64(10)}, f.user); err != nil {
				t.Fatal(err)
			}
			if err = warehouse.CompleteInbound(ctx, inbound, f.user); err != nil {
				t.Fatal(err)
			}
			qc := func() error {
				if endpoint == "record" {
					return warehouse.RecordQCPass(ctx, f.product, inbound, f.user, 10)
				}
				return warehouse.CompleteQC(ctx, f.product, inbound, f.user, 10, 0, 0, "counted accepted")
			}
			if err = qc(); err != nil {
				t.Fatal(err)
			}
			if err = qc(); err != nil {
				t.Fatal(err)
			}
			var task string
			var tasks, quantity int
			if err = db.QueryRow(`SELECT COUNT(*),COALESCE(MIN(id),''),COALESCE(SUM(quantity),0) FROM putaway_tasks WHERE inbound_id=$1`, inbound).Scan(&tasks, &task, &quantity); err != nil {
				t.Fatal(err)
			}
			if tasks != 1 || quantity != 10 {
				t.Fatalf("accepted receipt tasks=%d quantity=%d; want one exact task of 10", tasks, quantity)
			}
			if err = warehouse.ConfirmPutaway(ctx, task, f.user); !errors.Is(err, ErrInboundProvenance) {
				t.Fatalf("unassigned task completed: %v", err)
			}
			order, token := opsOrder(t, db, f, false)
			seedCanonicalWarehouseOrderOrigins(t, db, order, wh)
			orders := NewOrderRepository(db)
			if _, err = orders.MarkPaymentPaid(ctx, token, "before-putaway", "mask"); !errors.Is(err, ErrInsufficientInventory) {
				t.Fatalf("staged receipt purchased: %v", err)
			}
			var stock, local int
			if err = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock); err != nil || stock != 3 {
				t.Fatalf("unrelated supplier stock=%d err=%v; want 3", stock, err)
			}
			opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,quantity,warehouse_id,warehouse_name,capacity_volume_cm3,max_weight_grams,capacity_status) VALUES($1,'test','1','1',0,$2,$3,100000,100000,'verified')`, bin, wh, wh)
			if err = warehouse.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err = warehouse.ConfirmPutaway(ctx, task, f.user); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = orders.MarkPaymentPaid(ctx, token, "putaway-"+uuid.NewString()[:20], "mask"); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE product_id=$1 AND warehouse_id=$2 AND status='sellable'`, f.product, wh).Scan(&local); err != nil || local != 9 {
				t.Fatalf("placed and captured warehouse stock=%d err=%v; want 9", local, err)
			}
			if err = db.QueryRow(`SELECT stock FROM products WHERE id=$1`, f.product).Scan(&stock); err != nil || stock != 12 {
				t.Fatalf("aggregate stock=%d err=%v; want 12", stock, err)
			}
			var pending int
			if err = db.QueryRow(`SELECT COUNT(*) FROM putaway_tasks WHERE inbound_id=$1 AND status<>'completed'`, inbound).Scan(&pending); err != nil || pending != 0 {
				t.Fatalf("stranded tasks=%d err=%v", pending, err)
			}
		})
	}
}

func TestINV09MigrationPreservesHistoricalExceptions(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	warehouse := "w-" + uuid.NewString()
	down, err := os.ReadFile("../../migrations/202611140009_exact_inbound_putaway.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	inbound := inv09ReceiveLegacy(t, db, f, warehouse, 1)
	var line string
	if err = db.QueryRow(`SELECT id FROM inbound_inventory_lines WHERE shipment_id=$1`, inbound).Scan(&line); err != nil {
		t.Fatal(err)
	}
	oldTask, disposition := "OLD-"+uuid.NewString(), "QC-"+uuid.NewString()
	opsExec(t, db, `INSERT INTO putaway_tasks(id,product_id,inbound_id,warehouse_id,quantity,status) VALUES($1,$2,$3,$4,1,'pending')`, oldTask, f.product, inbound, warehouse)
	// Reproduce the previous version's accepted aggregate, without inventing an
	// allocation. Migration must preserve it for counted reconciliation.
	opsExec(t, db, `UPDATE inventory_balances SET status='qc_accepted' WHERE product_id=$1 AND location_key=$2 AND status='quarantine'`, f.product, "inbound-line:"+line)
	opsExec(t, db, `INSERT INTO inbound_qc_dispositions(id,receipt_line_id,cumulative_disposed,received_quantity,quantity_passed,quantity_failed,quantity_shortage,passed_delta,failed_delta) VALUES($1,$2,1,1,1,0,0,1,0)`, disposition, line)
	up, err := os.ReadFile("../../migrations/202611140009_exact_inbound_putaway.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	var allocations, total, accepted, sellable int
	if err = db.QueryRow(`SELECT COUNT(*) FROM inbound_putaway_allocations`).Scan(&allocations); err != nil || allocations != 0 {
		t.Fatalf("history was allocated: %d err=%v", allocations, err)
	}
	if err = db.QueryRow(`SELECT SUM(quantity),COALESCE(SUM(quantity) FILTER(WHERE status='qc_accepted'),0),COALESCE(SUM(quantity) FILTER(WHERE status='sellable'),0) FROM inventory_balances WHERE product_id=$1`, f.product).Scan(&total, &accepted, &sellable); err != nil || total != 1 || accepted != 1 || sellable != 0 {
		t.Fatalf("migration changed history total=%d accepted=%d sellable=%d err=%v", total, accepted, sellable, err)
	}
	wh := NewWarehouseRepository(db)
	queue, err := wh.GetPutawayQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	blocked := 0
	for _, item := range queue {
		if item.ProductID == f.product {
			if !item.ReconciliationRequired {
				t.Fatalf("historical task became actionable: %+v", item)
			}
			blocked++
		}
	}
	if blocked != 2 {
		t.Fatalf("historical task and accepted allocation exceptions=%d; want 2", blocked)
	}
	if err = wh.ConfirmPutaway(ctx, oldTask, f.user); !errors.Is(err, ErrInboundProvenance) {
		t.Fatalf("history placed without exact source: %v", err)
	}
}
