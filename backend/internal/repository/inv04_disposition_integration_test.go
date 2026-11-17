package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/migrations"
	"github.com/google/uuid"
)

// Removing the exact quarantine debit, or restoring the old supplier debit,
// must break these conserved-quantity assertions against PostgreSQL.
func inv04Receipt(t *testing.T, quantity, received int, both ...bool) (*sql.DB, *WarehouseRepository, string, string) {
	t.Helper()
	db, red, blue := inv04CanonicalFixture(t)
	if _, err := db.Exec(`UPDATE inventory_balances SET quantity=$2 WHERE variant_id=$1`, red, quantity); err != nil {
		t.Fatal(err)
	}
	sup := NewSupplierRepository(db)
	items := []CartonShipmentItem{{red, quantity}}
	if len(both) > 0 && both[0] {
		items = append(items, CartonShipmentItem{blue, 2})
	}
	sh, err := sup.CreateCartonShipment(context.Background(), "s", items, "w1", "inv04")
	if err != nil {
		t.Fatal(err)
	}
	if err := sup.ChangeCartonDispatch(context.Background(), "s", sh.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := sup.ReceiveWarehouseShipment(context.Background(), sh.ShipmentCode, []WarehouseReceiveItem{{red, received, "pass"}}); err != nil {
		t.Fatal(err)
	}
	if len(both) > 0 && both[0] {
		if _, err := sup.ReceiveWarehouseShipment(context.Background(), sh.ShipmentCode, []WarehouseReceiveItem{{blue, 2, "fail"}}); err != nil {
			t.Fatal(err)
		}
	}
	return db, NewWarehouseRepository(db), sh.ID, red
}

func TestINV04ExactSKUSelectorAndConcurrentEndpoints(t *testing.T) {
	db, wh, shipment, red := inv04Receipt(t, 5, 5, true)
	ctx := context.Background()
	if err := wh.RecordQCPass(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 5); err != ErrInboundProvenance {
		t.Fatalf("ambiguous SKU selection: %v", err)
	}
	var redLine, blueLine, blue string
	if err := db.QueryRow(`SELECT id FROM inbound_inventory_lines WHERE shipment_id=$1 AND variant_id=$2`, shipment, red).Scan(&redLine); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT id,variant_id FROM inbound_inventory_lines WHERE shipment_id=$1 AND variant_id<>$2`, shipment, red).Scan(&blueLine, &blue); err != nil {
		t.Fatal(err)
	}
	for _, selector := range [][]string{{""}, {"missing"}, {redLine, blueLine}} {
		if err := wh.RecordQCPass(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 5, selector...); err != ErrInboundProvenance {
			t.Fatalf("invalid selector accepted: %v", err)
		}
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 3, 2, 0, "red inspection", redLine); err != nil {
		t.Fatal(err)
	}
	var blueQuarantine, blueDamaged, blueSellable int
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity) FILTER(WHERE status='quarantine'),0),COALESCE(SUM(quantity) FILTER(WHERE status='damaged'),0),COALESCE(SUM(quantity) FILTER(WHERE status='sellable'),0) FROM inventory_balances WHERE variant_id=$1`, blue).Scan(&blueQuarantine, &blueDamaged, &blueSellable); err != nil {
		t.Fatal(err)
	}
	if blueQuarantine != 2 || blueDamaged != 0 || blueSellable != 1 {
		t.Fatalf("red QC touched blue: quarantine=%d damaged=%d supplier=%d", blueQuarantine, blueDamaged, blueSellable)
	}
	inv04Quantities(t, db, red, 5, 3, 2, 0)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 0, 2, 0, "blue failed", blueLine)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inbound_qc_dispositions WHERE receipt_line_id=$1`, blueLine).Scan(&events); err != nil || events != 1 {
		t.Fatalf("concurrent QC events=%d err=%v", events, err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity) FILTER(WHERE status='quarantine'),0),COALESCE(SUM(quantity) FILTER(WHERE status='damaged'),0),COALESCE(SUM(quantity) FILTER(WHERE status='sellable'),0) FROM inventory_balances WHERE variant_id=$1`, blue).Scan(&blueQuarantine, &blueDamaged, &blueSellable); err != nil {
		t.Fatal(err)
	}
	if blueQuarantine != 0 || blueDamaged != 2 || blueSellable != 1 {
		t.Fatalf("failed QC touched remaining supplier stock: quarantine=%d damaged=%d supplier=%d", blueQuarantine, blueDamaged, blueSellable)
	}
}

func TestINV04DispositionEvidenceAndRollbackGuard(t *testing.T) {
	db, wh, shipment, _ := inv04Receipt(t, 1, 1)
	ctx := context.Background()
	down, err := os.ReadFile("../../migrations/202611140008_inbound_qc_disposition.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	putawayDown, err := os.ReadFile("../../migrations/202611140009_exact_inbound_putaway.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(putawayDown)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	inv04Migrate(t, db)
	if err := wh.RecordQCPass(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 1); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE inbound_qc_dispositions SET quantity_passed=0`, `DELETE FROM inbound_qc_dispositions`, `INSERT INTO inbound_qc_dispositions SELECT id||'-duplicate',receipt_line_id,cumulative_disposed,received_quantity,quantity_passed,quantity_failed,quantity_shortage,passed_delta,failed_delta,reason,actor_user_id,created_at FROM inbound_qc_dispositions`} {
		if _, err := db.Exec(q); err == nil {
			t.Fatalf("QC identity evidence changed: %s", q)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(down)); err == nil {
		tx.Rollback()
		t.Fatal("migration erased recorded disposition")
	}
	tx.Rollback()
	if err := wh.RecordQCPass(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 1); err != nil {
		t.Fatalf("refused rollback damaged event: %v", err)
	}
}

func TestINV04AmbiguousPhysicalEvidenceRequiresReconciliation(t *testing.T) {
	for _, mode := range []string{"extra-quarantine", "historic-qc"} {
		t.Run(mode, func(t *testing.T) {
			db, wh, shipment, variant := inv04Receipt(t, 1, 1)
			if mode == "extra-quarantine" {
				if _, err := db.Exec(`UPDATE inventory_balances SET quantity=quantity+1 WHERE variant_id=$1 AND status='quarantine'`, variant); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := db.Exec(`INSERT INTO qc_results(id,product_id,inbound_id,status,quantity_failed) VALUES('historical','p',$1,'completed',1)`, shipment); err != nil {
					t.Fatal(err)
				}
			}
			if err := wh.CompleteQC(context.Background(), "p", shipment, "00000000-0000-4000-8000-000000000999", 0, 1, 0, "counted fail"); err != ErrInboundProvenance {
				t.Fatalf("ambiguous history accepted: %v", err)
			}
			var events, moves int
			if err := db.QueryRow(`SELECT COUNT(*) FROM inbound_qc_dispositions`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='inbound_qc_disposition'`).Scan(&moves); err != nil {
				t.Fatal(err)
			}
			if events != 0 || moves != 0 {
				t.Fatalf("unresolved evidence repaired: events=%d moves=%d", events, moves)
			}
		})
	}
}

func inv04Migrate(t *testing.T, db *sql.DB) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`SELECT to_regclass('inbound_qc_dispositions') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		return
	}
	ddl, err := os.ReadFile("../../migrations/202611140008_inbound_qc_disposition.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	putaway, err := os.ReadFile("../../migrations/202611140009_exact_inbound_putaway.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(putaway)); err != nil {
		t.Fatal(err)
	}
}

func inv04LegacyReceipt(t *testing.T, quantity int) (*sql.DB, *WarehouseRepository) {
	t.Helper()
	db, _, _ := inv04CanonicalFixture(t)
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,schema_version) VALUES('legacy','legacy','inv04-legacy','s',2000,1000,0,0,0)`)
	if _, err := db.Exec(`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled,supplier_warehouse_id) VALUES('legacy-source','legacy','s','supplier','supplier','supplier-warehouse:sw1','sellable',$1,TRUE,'sw1')`, quantity); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO inbound_shipments(id,status,warehouse_id,supplier_id,supplier_name,items_count,cartons_count,items) VALUES('INB-inv04','in_transit','w1','s','INV04 supplier',$1,1,jsonb_build_array(jsonb_build_object('product_id','legacy','quantity',$1::int)))`, quantity); err != nil {
		t.Fatal(err)
	}
	ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := LockTransactionRowsTx(ctx, tx, &TransactionLockSet{}, TransactionRowLock{Table: "suppliers", ID: "s"}, TransactionRowLock{Table: "products", ID: "legacy"}, TransactionRowLock{Table: "inbound_shipments", ID: "INB-inv04"}); err != nil {
		t.Fatal(err)
	}
	if err := reserveInboundTransferTx(ctx, tx, "INB-inv04", "s", "w1", []InboundTransferLine{{ProductID: "legacy", Quantity: quantity, SourceKind: "transfer", Source: InventoryOrigin{Type: "supplier", SupplierID: "s", SupplierWarehouseID: "sw1"}}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatchInboundTransferTx(ctx, tx, "INB-inv04"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	wh := NewWarehouseRepository(db)
	if err := wh.StartReceiving(ctx, "INB-inv04", "00000000-0000-4000-8000-000000000999"); err != nil {
		t.Fatal(err)
	}
	if _, err := wh.ScanItem(ctx, "INB-inv04", map[string]interface{}{"productId": "legacy", "quantity": float64(quantity)}, "00000000-0000-4000-8000-000000000999"); err != nil {
		t.Fatal(err)
	}
	if err := wh.CompleteInbound(ctx, "INB-inv04", "00000000-0000-4000-8000-000000000999"); err != nil {
		t.Fatal(err)
	}
	return db, wh
}

func TestAuditSupplyFailedLegacyQCRemovesDeclaredSellableStock(t *testing.T) {
	db, wh := inv04LegacyReceipt(t, 1)
	if err := wh.CompleteQC(context.Background(), "legacy", "INB-inv04", "00000000-0000-4000-8000-000000000999", 0, 1, 0, "failed"); err != nil {
		t.Fatal(err)
	}
	var total, sellable, damaged int
	if err := db.QueryRow(`SELECT SUM(quantity),COALESCE(SUM(quantity) FILTER(WHERE status='sellable'),0),COALESCE(SUM(quantity) FILTER(WHERE status='damaged'),0) FROM inventory_balances WHERE product_id='legacy'`).Scan(&total, &sellable, &damaged); err != nil {
		t.Fatal(err)
	}
	if total != 1 || sellable != 0 || damaged != 1 {
		t.Fatalf("total=%d sellable=%d damaged=%d", total, sellable, damaged)
	}
}

func TestINV04PartialDispositionStaysInQueue(t *testing.T) {
	_, wh := inv04LegacyReceipt(t, 5)
	ctx := context.Background()
	if err := wh.RecordQCPass(ctx, "legacy", "INB-inv04", "00000000-0000-4000-8000-000000000999", 3); err != nil {
		t.Fatal(err)
	}
	queue, err := wh.GetQCQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("undisposed units hidden after partial QC: queue=%v", queue)
	}
	raw, _ := json.Marshal(queue)
	var wire []map[string]interface{}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire[0]["passedQuantity"] != float64(3) || wire[0]["failedQuantity"] != float64(0) || wire[0]["remainingQuantity"] != float64(2) {
		t.Fatalf("queue lacks cumulative progress: %s", raw)
	}
	if err := wh.CompleteQC(ctx, "legacy", "INB-inv04", "00000000-0000-4000-8000-000000000999", 3, 2, 0, "remaining failed"); err != nil {
		t.Fatal(err)
	}
	if err := wh.RecordQCPass(ctx, "legacy", "INB-inv04", "00000000-0000-4000-8000-000000000999", 3); err != nil {
		t.Fatalf("delayed partial replay: %v", err)
	}
	queue, err = wh.GetQCQueue(ctx)
	if err != nil || len(queue) != 0 {
		t.Fatalf("fully disposed queue=%v error=%v", queue, err)
	}
}

func TestINV04HistoricalQueueRemainsVisibleAsException(t *testing.T) {
	db, wh := inv04LegacyReceipt(t, 5)
	if _, err := db.Exec(`INSERT INTO inbound_shipments(id,status,warehouse_id,supplier_id,supplier_name,items_count,cartons_count,items,scanned_items) VALUES('INB-unallocated','received','w1','s','INV04 supplier',1,1,'[{"product_id":"legacy","quantity":1}]','[{"productId":"legacy","quantity":1}]')`); err != nil {
		t.Fatal(err)
	}
	queue, err := wh.GetQCQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 {
		t.Fatalf("unallocated historical receipt disappeared: %+v", queue)
	}
	raw, _ := json.Marshal(queue)
	var wire []map[string]interface{}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	for _, item := range wire {
		if item["inboundId"] == "INB-unallocated" {
			if item["reconciliationRequired"] != true || item["receiptLineId"] != "" {
				t.Fatalf("history not surfaced as explicit exception: %s", raw)
			}
			return
		}
	}
	t.Fatalf("history absent: %s", raw)
}

func inv04Quantities(t *testing.T, db *sql.DB, variant string, wantTotal, wantAccepted, wantFailed, wantQuarantine int) {
	t.Helper()
	var total, accepted, failed, quarantine, sellable, tasks int
	if err := db.QueryRow(`SELECT SUM(quantity),COALESCE(SUM(quantity) FILTER(WHERE status='qc_accepted'),0),COALESCE(SUM(quantity) FILTER(WHERE status='damaged'),0),COALESCE(SUM(quantity) FILTER(WHERE status='quarantine'),0),COALESCE(SUM(quantity) FILTER(WHERE status='sellable'),0) FROM inventory_balances WHERE variant_id=$1`, variant).Scan(&total, &accepted, &failed, &quarantine, &sellable); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0) FROM putaway_tasks WHERE variant_id=$1`, variant).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if total != wantTotal || accepted != wantAccepted || failed != wantFailed || quarantine != wantQuarantine || sellable != 0 || tasks != wantAccepted {
		t.Fatalf("physical=%d accepted=%d failed=%d quarantine=%d sellable=%d tasks=%d", total, accepted, failed, quarantine, sellable, tasks)
	}
	var unmatched int
	if err := db.QueryRow(`SELECT COUNT(*) FROM putaway_tasks pt LEFT JOIN inbound_putaway_allocations a ON a.task_id=pt.id LEFT JOIN inventory_balances b ON b.location_key=a.source_location_key AND b.status='qc_accepted' AND b.variant_id=pt.variant_id WHERE pt.variant_id=$1 AND (a.task_id IS NULL OR pt.quantity IS DISTINCT FROM a.quantity OR b.quantity IS DISTINCT FROM a.quantity)`, variant).Scan(&unmatched); err != nil || unmatched != 0 {
		t.Fatalf("putaway lacks exact accepted units: %d err=%v", unmatched, err)
	}
	// The supplier origin persisted on the order cannot claim failed,
	// accepted-but-unplaced, or undisposed receipt stock.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	opsExec(t, db, `INSERT INTO stores(id,name,slug,owner_name,owner_user_id,active_plan_id,status) VALUES('inv04-store','INV04','inv04-store','INV04','00000000-0000-4000-8000-000000000999','standard','active') ON CONFLICT DO NOTHING`)
	opsExec(t, db, `INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,tracking_code,customer_user_id,payment_status) VALUES('inv04-capture','inv04-capture','inv04-store','INV04',2000,2000,'INV04','09999999999','p','c','a','1234512345','inv04-capture','00000000-0000-4000-8000-000000000999','pending') ON CONFLICT DO NOTHING`)
	if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,'inv04-capture','p',$2,1,1000,0,1000,2000)`, uuid.NewString(), variant); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,supplier_warehouse_id,origin_label,quantity) SELECT id::text,'inv04-capture',id,0,'supplier','s','sw1','source',1 FROM order_items WHERE order_id='inv04-capture'`); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(context.Background(), tx, "inv04-capture"); err != ErrInsufficientInventory {
		t.Fatalf("non-sellable receipt captured: %v", err)
	}
	if _, err := tx.Exec(`UPDATE order_item_origins SET origin_type='warehouse',warehouse_id='w1',supplier_warehouse_id=NULL WHERE order_id='inv04-capture'`); err != nil {
		t.Fatal(err)
	}
	if _, err := captureOrderInventoryTx(context.Background(), tx, "inv04-capture"); err != ErrInsufficientInventory {
		t.Fatalf("warehouse captured non-sellable receipt: %v", err)
	}
	var projected int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, variant).Scan(&projected); err != nil || projected != 0 {
		t.Fatalf("non-sellable QC projected as stock=%d err=%v", projected, err)
	}
}

func TestINV04FailedReceiptConservesTransferredUnit(t *testing.T) {
	db, wh, shipment, variant := inv04Receipt(t, 1, 1)
	if err := wh.CompleteQC(context.Background(), "p", shipment, "00000000-0000-4000-8000-000000000999", 0, 1, 0, "damaged"); err != nil {
		t.Fatalf("safe failed disposition rejected: %v", err)
	}
	inv04Quantities(t, db, variant, 1, 0, 1, 0)
}

func TestINV04PartialPassFailAndImmutableReplay(t *testing.T) {
	db, wh, shipment, variant := inv04Receipt(t, 5, 5)
	ctx := context.Background()
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 3, 2, 0, "inspection"); err != nil {
		t.Fatal(err)
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "another-inspector", 3, 2, 0, "inspection"); err != nil {
		t.Fatal(err)
	}
	for _, q := range [][3]int{{4, 1, 0}, {3, 3, 0}, {3, 2, 1}} {
		if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", q[0], q[1], q[2], "inspection"); err == nil {
			t.Fatalf("changed disposition accepted: %v", q)
		}
	}
	if err := wh.RecordQCPass(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 5); err == nil {
		t.Fatal("opposite endpoint replaced failed units")
	}
	inv04Quantities(t, db, variant, 5, 3, 2, 0)
}

func TestINV04OppositeEndpointReplay(t *testing.T) {
	for _, first := range []string{"record", "complete"} {
		t.Run(first, func(t *testing.T) {
			db, wh, shipment, variant := inv04Receipt(t, 2, 2)
			ctx := context.Background()
			record := func() error { return wh.RecordQCPass(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 2) }
			complete := func() error {
				return wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 2, 0, 0, "all passed")
			}
			calls := []func() error{record, complete, record, complete}
			if first == "complete" {
				calls = []func() error{complete, record, complete, record}
			}
			for _, call := range calls {
				if err := call(); err != nil {
					t.Fatal(err)
				}
			}
			if err := wh.RecordQCPass(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 1); err == nil {
				t.Fatal("changed pass acknowledged")
			}
			inv04Quantities(t, db, variant, 2, 2, 0, 0)
			var results, movements int
			if err := db.QueryRow(`SELECT COUNT(*) FROM qc_results`).Scan(&results); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM inventory_movements WHERE reference_type='inbound_qc_disposition'`).Scan(&movements); err != nil {
				t.Fatal(err)
			}
			if results != 1 || movements != 1 {
				t.Fatalf("duplicate receipt result=%d movements=%d", results, movements)
			}
		})
	}
}

func TestINV04CumulativeReceiptAndDelayedReplay(t *testing.T) {
	db, wh, shipment, variant := inv04Receipt(t, 5, 2)
	ctx := context.Background()
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 1, 1, 0, "batch one"); err != nil {
		t.Fatal(err)
	}
	inv04Quantities(t, db, variant, 5, 1, 1, 0)
	var code string
	if err := db.QueryRow(`SELECT shipment_code FROM shipments WHERE id=$1`, shipment).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSupplierRepository(db).ReceiveWarehouseShipment(ctx, code, []WarehouseReceiveItem{{variant, 5, "pass"}}); err != nil {
		t.Fatal(err)
	}
	// This is the old completed batch, never a new delta against receipt 5.
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 1, 1, 0, "batch one"); err != nil {
		t.Fatal(err)
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 4, 1, 0, "batch two"); err != nil {
		t.Fatal(err)
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 1, 1, 0, "batch one"); err != nil {
		t.Fatal(err)
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 3, 2, 0, "rewrite failed disposition"); err == nil {
		t.Fatal("prior passed quantity was rewritten")
	}
	inv04Quantities(t, db, variant, 5, 4, 1, 0)
}

func TestINV04ShortageHasNoPhysicalCredit(t *testing.T) {
	db, wh, shipment, variant := inv04Receipt(t, 5, 3)
	ctx := context.Background()
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 2, 1, 2, "two never arrived"); err != nil {
		t.Fatal(err)
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 2, 1, 2, "two never arrived"); err != nil {
		t.Fatal(err)
	}
	inv04Quantities(t, db, variant, 5, 2, 1, 0)
	var discrepancies, expected, received int
	if err := db.QueryRow(`SELECT COUNT(*),MAX(expected_quantity),MAX(received_quantity) FROM qc_discrepancies WHERE inbound_id=$1`, shipment).Scan(&discrepancies, &expected, &received); err != nil {
		t.Fatal(err)
	}
	if discrepancies != 1 || expected != 5 || received != 3 {
		t.Fatalf("shortage evidence count=%d expected=%d received=%d", discrepancies, expected, received)
	}
	var code string
	if err := db.QueryRow(`SELECT shipment_code FROM shipments WHERE id=$1`, shipment).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSupplierRepository(db).ReceiveWarehouseShipment(ctx, code, []WarehouseReceiveItem{{variant, 5, "pass"}}); err != nil {
		t.Fatal(err)
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 4, 1, 0, "late arrival counted"); err != nil {
		t.Fatalf("resolved shortage prevented later counted receipt disposition: %v", err)
	}
	if err := wh.CompleteQC(ctx, "p", shipment, "00000000-0000-4000-8000-000000000999", 2, 1, 2, "two never arrived"); err != nil {
		t.Fatalf("old shortage replay after late receipt: %v", err)
	}
	inv04Quantities(t, db, variant, 5, 4, 1, 0)
}

// Positive QC/storage integration uses the real migrated schema and guards.
// The fixed domain IDs retain these disposition tests' exact conservation probes.
func inv04CanonicalFixture(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	root := operationsDB(t)
	schema := "inv04_current_" + uuid.NewString()[:8]
	opsExec(t, root, `CREATE SCHEMA `+schema)
	t.Cleanup(func() { root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`) })
	db := skuScopedDB(t, schema)
	opsExec(t, db, `CREATE FUNCTION uuid_generate_v4() RETURNS UUID LANGUAGE SQL AS 'SELECT public.uuid_generate_v4()'`)
	if _, err := migrations.Run(context.Background(), db, migrations.Options{Dir: "../../migrations", MigrationTimeout: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	actor := "00000000-0000-4000-8000-000000000999"
	opsExec(t, db, `INSERT INTO users(id,mobile,full_name,role) VALUES($1,'09999999999','INV04 staff','supplier')`, actor)
	opsExec(t, db, `INSERT INTO suppliers(id,name,owner_user_id,approval_status) VALUES('s','INV04 supplier',$1,'approved')`, actor)
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES('w1','INV04 central',1,1,'p','c')`)
	opsExec(t, db, `INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name) VALUES('sw1','s','INV04 source','p','c')`)
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,schema_version,product_type,attribute_schema,origin_supplier_warehouse_id) VALUES('p','INV04 product','inv04-product','s',2000,1000,0,0,2,'variable','{}','sw1')`)
	red, blue := uuid.NewString(), uuid.NewString()
	opsExec(t, db, `INSERT INTO product_variants(id,product_id,spnc,inventory_mode,stock) VALUES($1,'p','INV04-RED','tracked',5),($2,'p','INV04-BLUE','tracked',3)`, red, blue)
	for _, lot := range []struct {
		id string
		q  int
	}{{red, 5}, {blue, 3}} {
		opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,supplier_warehouse_id) VALUES($1,'p','s',$2,'supplier','supplier','supplier-warehouse:sw1','sellable',$3,'sw1')`, uuid.NewString(), lot.id, lot.q)
	}
	return db, red, blue
}
