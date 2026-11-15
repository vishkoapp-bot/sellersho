package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/warehouseopt"
	"github.com/google/uuid"
)

func inv06Placed(t *testing.T, tracked bool, quantity int, physical ...int) (*sql.DB, opsFixture, string, string, string) {
	t.Helper()
	ctx := context.Background()
	var db *sql.DB
	var f opsFixture
	var warehouse, inbound, sku string
	if tracked {
		db, f, warehouse, inbound, sku, _ = inv09ReceiveTracked(t, quantity)
	} else {
		db = operationsDB(t)
		f = opsFixtureNew(t, db)
		warehouse = "w-" + uuid.NewString()
		inbound = inv09ReceiveLegacy(t, db, f, warehouse, quantity)
	}
	wh := NewWarehouseRepository(db)
	dimensions := []int{100, 100, 100, 100}
	if len(physical) == 4 {
		dimensions = physical
	}
	if err := NewSupplierRepository(db).RecordProductMeasurement(ctx, f.product, f.user, dimensions[0], dimensions[1], dimensions[2], dimensions[3], "measured departure lot"); err != nil {
		t.Fatal(err)
	}
	if err := wh.RecordQCPass(ctx, f.product, inbound, f.user, quantity); err != nil {
		t.Fatal(err)
	}
	var task string
	if err := db.QueryRow(`SELECT id FROM putaway_tasks WHERE inbound_id=$1`, inbound).Scan(&task); err != nil {
		t.Fatal(err)
	}
	volume, weight, err := warehouseopt.Capacity(int64(dimensions[0]), int64(dimensions[1]), int64(dimensions[2]), int64(dimensions[3]), int64(quantity))
	if err != nil {
		t.Fatal(err)
	}
	bin := inv08Bin(t, db, warehouse, int(volume), int(weight))
	if err := wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	if err := wh.ConfirmPutaway(ctx, task, f.user); err != nil {
		t.Fatal(err)
	}
	return db, f, warehouse, bin, sku
}

func inv06PaidOrder(t *testing.T, db *sql.DB, f opsFixture, warehouse, sku string, quantities ...int) string {
	t.Helper()
	order, token := opsOrder(t, db, f, false)
	if len(quantities) > 0 {
		q := quantities[0]
		opsExec(t, db, `UPDATE order_items SET quantity=$2 WHERE order_id=$1`, order, q)
		opsExec(t, db, `UPDATE orders SET total_amount=2000*$2,payable_amount=2000*$2 WHERE id=$1`, order, q)
		opsExec(t, db, `UPDATE payment_transactions SET amount=2000*$2 WHERE order_id=$1`, order, q)
	}
	if sku != "" {
		opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, sku)
	}
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	if _, err := NewOrderRepository(db).MarkPaymentPaid(context.Background(), token, "r"+uuid.NewString()[:20], "mask"); err != nil {
		t.Fatal(err)
	}
	return order
}

func inv06AssertOccupancy(t *testing.T, db *sql.DB, bin string, quantity, volume, weight int) {
	t.Helper()
	var q, v, w int
	if err := db.QueryRow(`SELECT quantity,occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code=$1`, bin).Scan(&q, &v, &w); err != nil || q != quantity || v != volume || w != weight {
		t.Fatalf("bin=%s physical quantity/volume/weight=%d/%d/%d want%d/%d/%d err=%v", bin, q, v, w, quantity, volume, weight, err)
	}
}

func TestAuditSupplyShippingReleasesActualPutawayBinCapacity(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "tracked"}[tracked], func(t *testing.T) {
			db, f, warehouse, bin, sku := inv06Placed(t, tracked, 1)
			ctx := context.Background()
			order := inv06PaidOrder(t, db, f, warehouse, sku)
			inv06AssertOccupancy(t, db, bin, 1, 1000, 100) // Paid units are still physically held.
			if err := NewSupplierRepository(db).RecordProductMeasurement(ctx, f.product, f.user, 200, 200, 200, 200, "remeasured after placement"); err != nil {
				t.Fatal(err)
			}
			opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
			wh := NewWarehouseRepository(db)
			if err := wh.ShipOrderWithActor(ctx, order, "post", "TRK-"+order, f.user); err != nil {
				t.Fatal(err)
			}
			var actor string
			if err := db.QueryRow(`SELECT actor_user_id FROM warehouse_departure_events WHERE event_id=$1`, order).Scan(&actor); err != nil || actor != f.user {
				t.Fatalf("departure actor=%q err=%v", actor, err)
			}
			inv06AssertOccupancy(t, db, bin, 0, 0, 0) // Release the archived physical tuple, not today's dimensions.
			_ = wh.ShipOrder(ctx, order, "post", "TRK-"+order)
			inv06AssertOccupancy(t, db, bin, 0, 0, 0)
			if err := NewOrderRepository(db).CancelOrder(ctx, order, f.user, true); !errors.Is(err, ErrOrderPartiallyDispatched) {
				t.Fatalf("post-departure cancellation=%v", err)
			}
		})
	}
}

func TestINV06PaymentCancellationKeepsPhysicalOccupancy(t *testing.T) {
	db, f, warehouse, bin, sku := inv06Placed(t, true, 1)
	order := inv06PaidOrder(t, db, f, warehouse, sku)
	if err := NewOrderRepository(db).CancelOrder(context.Background(), order, f.user, true); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 1, 1000, 100)
	var stock int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, sku).Scan(&stock); err != nil || stock != 1 {
		t.Fatalf("cancelled paid unit not restored=%d err=%v", stock, err)
	}
}

func TestINV06ForgedDepartureCannotOccupyBusinessIdentity(t *testing.T) {
	db, f, warehouse, bin, sku := inv06Placed(t, true, 1)
	order := inv06PaidOrder(t, db, f, warehouse, sku)
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := loadOrderDepartureAllocationsTx(context.Background(), tx, order)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	allocations, err = resolveDepartureAllocationsTx(context.Background(), tx, allocations)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Rollback()
	raw, err := json.Marshal(allocations)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	// A plausible immutable event must not block the real handover before the
	// order has physically departed, or refer to a nonexistent business event.
	for _, id := range []string{order, "unknown-order"} {
		if _, err := db.Exec(`INSERT INTO warehouse_departure_events(event_type,event_id,payload_hash,actor_user_id,payload_json) VALUES('warehouse_shipment',$1,$2,$3,$4)`, id, hex.EncodeToString(hash[:]), f.user, string(raw)); err == nil {
			t.Fatal("forged immutable departure occupied business identity")
		}
	}
	inv06AssertOccupancy(t, db, bin, 1, 1000, 100)
}

func inv06RefillSKU(t *testing.T, db *sql.DB, f opsFixture, warehouse, bin, sku string, quantity int) {
	t.Helper()
	ctx := context.Background()
	sup, wh := NewSupplierRepository(db), NewWarehouseRepository(db)
	var existing int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, sku).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if _, err := sup.AdjustProductStock(ctx, f.supplier, f.product, sku, existing+quantity, "additional measured receipt", f.user); err != nil {
		t.Fatal(err)
	}
	shipment, err := sup.CreateCartonShipment(ctx, f.supplier, []CartonShipmentItem{{VariantID: sku, ExpectedQty: quantity}}, warehouse, "refill-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err = sup.ChangeCartonDispatch(ctx, f.supplier, shipment.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = sup.ReceiveWarehouseShipment(ctx, shipment.ShipmentCode, []WarehouseReceiveItem{{VariantID: sku, ReceivedQty: quantity, QCStatus: "pass"}}); err != nil {
		t.Fatal(err)
	}
	if err = wh.RecordQCPass(ctx, f.product, shipment.ID, f.user, quantity); err != nil {
		t.Fatal(err)
	}
	var task string
	if err = db.QueryRow(`SELECT id FROM putaway_tasks WHERE inbound_id=$1`, shipment.ID).Scan(&task); err != nil {
		t.Fatal(err)
	}
	if err = wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	if err = wh.ConfirmPutaway(ctx, task, f.user); err != nil {
		t.Fatal(err)
	}
}

func TestINV06ConcurrentDeparturesPreserveRoundingAndPermitRefill(t *testing.T) {
	db, f, warehouse, bin, sku := inv06Placed(t, true, 2, 1, 1, 1, 1)
	ctx := context.Background()
	orders := []string{inv06PaidOrder(t, db, f, warehouse, sku), inv06PaidOrder(t, db, f, warehouse, sku)}
	inv06AssertOccupancy(t, db, bin, 2, 1, 2)
	for _, order := range orders {
		opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, order := range orders {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			results <- NewWarehouseRepository(db).ShipOrder(ctx, id, "post", "TRK-"+id)
		}(order)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	inv06AssertOccupancy(t, db, bin, 0, 0, 0)
	var quantity, volume, weight, count int
	if err := db.QueryRow(`SELECT COUNT(*),SUM(quantity),SUM(volume_cm3),SUM(weight_grams) FROM warehouse_occupancy_releases`).Scan(&count, &quantity, &volume, &weight); err != nil || count != 2 || quantity != 2 || volume != 1 || weight != 2 {
		t.Fatalf("rounded release count=%d qty/volume/weight=%d/%d/%d err=%v", count, quantity, volume, weight, err)
	}
	// A retry verifies the original payload; altered quantities cannot consume
	// another placement under the same immutable business event.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := loadOrderDepartureAllocationsTx(ctx, tx, orders[0])
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = releasePlacementOccupancyTx(ctx, tx, orders[0], "warehouse_shipment", f.user, allocations); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	allocations[0].Quantity++
	if err = releasePlacementOccupancyTx(ctx, tx, orders[0], "warehouse_shipment", f.user, allocations); !errors.Is(err, ErrOccupancyReconciliation) {
		tx.Rollback()
		t.Fatalf("changed replay=%v", err)
	}
	if err = releasePlacementOccupancyTx(ctx, tx, orders[0], "warehouse_shipment", f.user, nil); !errors.Is(err, ErrOccupancyReconciliation) {
		tx.Rollback()
		t.Fatalf("empty changed replay=%v", err)
	}
	tx.Rollback()
	inv06RefillSKU(t, db, f, warehouse, bin, sku, 1)
	inv06AssertOccupancy(t, db, bin, 1, 1, 1)
}

func TestINV06MultiBinShipmentReleasesOnlyCapturedUnits(t *testing.T) {
	db, f, warehouse, first, sku := inv06Placed(t, true, 2)
	second := inv08Bin(t, db, warehouse, 2000, 200)
	inv06RefillSKU(t, db, f, warehouse, second, sku, 2)
	order := inv06PaidOrder(t, db, f, warehouse, sku, 3)
	inv06AssertOccupancy(t, db, first, 2, 2000, 200)
	inv06AssertOccupancy(t, db, second, 2, 2000, 200)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err := NewWarehouseRepository(db).ShipOrder(context.Background(), order, "post", "TRK-"+order); err != nil {
		t.Fatal(err)
	}
	var qty, volume, weight int
	if err := db.QueryRow(`SELECT SUM(quantity),SUM(occupied_volume_cm3),SUM(occupied_weight_grams) FROM warehouse_bins WHERE bin_code IN($1,$2)`, first, second).Scan(&qty, &volume, &weight); err != nil || qty != 1 || volume != 1000 || weight != 100 {
		t.Fatalf("multi-bin remaining=%d/%d/%d err=%v", qty, volume, weight, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_occupancy_releases WHERE event_id=$1`, order).Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected exact departure in both bins count=%d err=%v", count, err)
	}
}

func TestINV06SupplierReturnHandoverReleasesOnlyDepartedUnits(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "tracked"}[tracked], func(t *testing.T) {
			db, f, warehouse, bin, sku := inv06Placed(t, tracked, 4)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			sup := NewSupplierRepository(db)
			request, err := sup.CreateReturnRequestForVariantWithIdempotency(ctx, f.supplier, f.product, sku, 2, "supplier_collect", "counted handover", "return-"+uuid.NewString(), f.user)
			if err != nil {
				t.Fatal(err)
			}
			if err = sup.ReviewReturn(ctx, request.ID, "approved", "counted", f.user); err != nil {
				t.Fatal(err)
			}
			opsExec(t, db, `INSERT INTO carrier_configs(code,display_name,enabled,supports_receiver_pays) VALUES('inv06','local handover fixture',TRUE,TRUE) ON CONFLICT(code) DO NOTHING`)
			parcel, err := sup.CreateReturnParcel(ctx, request.ID, f.user, ReturnParcelInput{Quantity: 2, CarrierCode: "inv06", PaymentMethod: "receiver_pays", Origin: warehouse, Destination: "supplier", LengthMM: 100, WidthMM: 100, HeightMM: 100, WeightGrams: 200}, "parcel-"+uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			if err = sup.ConfirmReturnParcelCharge(ctx, request.ID, parcel.ID, f.user, "charge-"+uuid.NewString()); err != nil {
				t.Fatal(err)
			}
			inv06AssertOccupancy(t, db, bin, 4, 4000, 400)
			for i := 0; i < 2; i++ {
				if err = sup.HandoverReturn(ctx, request.ID, f.user, "handover-"+request.ID); err != nil {
					t.Fatal(err)
				}
			}
			inv06AssertOccupancy(t, db, bin, 2, 2000, 200)
		})
	}
}

func TestINV06HistoricalUnknownBinShipsWithoutInventingCapacity(t *testing.T) {
	db, f, warehouse, _, sku := inv06Placed(t, true, 1)
	unknown := inv08Bin(t, db, warehouse, 10000, 10000)
	opsExec(t, db, `UPDATE warehouse_bins SET quantity=1,occupied_volume_cm3=123,occupied_weight_grams=456 WHERE bin_code=$1`, unknown)
	opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,$3,$4,'supplier','bin',$5,'sellable',1,$6)`, "000-legacy-"+uuid.NewString(), f.product, f.supplier, sku, "bin:"+unknown, warehouse)
	opsExec(t, db, `UPDATE warehouse_storage_positions SET qty=qty+1 WHERE product_id=$1`, f.product)
	order := inv06PaidOrder(t, db, f, warehouse, sku)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err := NewWarehouseRepository(db).ShipOrder(context.Background(), order, "post", "TRK-"+order); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, unknown, 1, 123, 456)
	var status string
	if err := db.QueryRow(`SELECT capacity_status FROM warehouse_bins WHERE bin_code=$1`, unknown).Scan(&status); err != nil || status != "disputed" {
		t.Fatalf("unknown history was not disputed=%s err=%v", status, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_occupancy_exceptions WHERE bin_code=$1`, unknown).Scan(&count); err != nil || count != 1 {
		t.Fatal("unknown history hidden")
	}
}

func TestINV06SharedBinRevisionFIFOAndDatabaseEventCaps(t *testing.T) {
	db, f, warehouse, bin, sku := inv06Placed(t, true, 1)
	ctx := context.Background()
	opsExec(t, db, `UPDATE warehouse_bins SET capacity_volume_cm3=10000,max_weight_grams=1000 WHERE bin_code=$1`, bin)
	if err := NewSupplierRepository(db).RecordProductMeasurement(ctx, f.product, f.user, 200, 200, 200, 200, "second measured lot"); err != nil {
		t.Fatal(err)
	}
	inv06RefillSKU(t, db, f, warehouse, bin, sku, 1)
	inv06AssertOccupancy(t, db, bin, 2, 9000, 300)
	first := inv06PaidOrder(t, db, f, warehouse, sku)
	second := inv06PaidOrder(t, db, f, warehouse, sku)
	for _, order := range []string{first, second} {
		opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	}
	wh := NewWarehouseRepository(db)
	if err := wh.ShipOrder(ctx, first, "post", "TRK-"+first); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 1, 8000, 200)
	var secondPlacement string
	if err := db.QueryRow(`SELECT id FROM warehouse_remaining_placements WHERE bin_code=$1 AND remaining_quantity=1`, bin).Scan(&secondPlacement); err != nil {
		t.Fatal(err)
	}
	// A second release under the first order cannot take a different placement
	// merely because that lot still has units available.
	if _, err := db.Exec(`INSERT INTO warehouse_occupancy_releases(id,event_type,event_id,placement_id,quantity,volume_cm3,weight_grams) VALUES($1,'warehouse_shipment',$2,$3,1,8000,200)`, uuid.NewString(), first, secondPlacement); err == nil {
		t.Fatal("SQL release exceeded the event's exact captured quantity")
	}
	inv06AssertOccupancy(t, db, bin, 1, 8000, 200)
	for _, mutation := range []string{`UPDATE warehouse_bins SET occupied_volume_cm3=8001 WHERE bin_code=$1`, `UPDATE warehouse_bins SET capacity_status='disputed' WHERE bin_code=$1`} {
		bad, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = bad.Exec(`UPDATE orders SET status='shipped',shipped_at=NOW() WHERE id=$1`, second); err != nil {
			bad.Rollback()
			t.Fatal(err)
		}
		allocations, e := loadOrderDepartureAllocationsTx(ctx, bad, second)
		if e != nil {
			bad.Rollback()
			t.Fatal(e)
		}
		allocations, e = resolveDepartureAllocationsTx(ctx, bad, allocations)
		if e != nil {
			bad.Rollback()
			t.Fatal(e)
		}
		raw, e := json.Marshal(allocations)
		if e != nil {
			bad.Rollback()
			t.Fatal(e)
		}
		hash := sha256.Sum256(raw)
		if _, err = bad.Exec(`INSERT INTO warehouse_departure_events(event_type,event_id,payload_hash,payload_json,actor_user_id) VALUES('warehouse_shipment',$1,$2,$3,$4)`, second, hex.EncodeToString(hash[:]), string(raw), f.user); err != nil {
			bad.Rollback()
			t.Fatal(err)
		}
		if _, err = bad.Exec(mutation, bin); err != nil {
			bad.Rollback()
			t.Fatal(err)
		}
		_, err = bad.Exec(`INSERT INTO warehouse_occupancy_releases(id,event_type,event_id,placement_id,quantity,volume_cm3,weight_grams) VALUES($1,'warehouse_shipment',$2,$3,1,8000,200)`, uuid.NewString(), second, secondPlacement)
		bad.Rollback()
		if err == nil {
			t.Errorf("direct SQL released disputed or inconsistent counters: %s", mutation)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE orders SET status='shipped',shipped_at=NOW() WHERE id=$1`, second); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	allocations, err := loadOrderDepartureAllocationsTx(ctx, tx, second)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	allocations, err = resolveDepartureAllocationsTx(ctx, tx, allocations)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	allocations[0].Quantity++
	raw, err := json.Marshal(allocations)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if _, err = tx.Exec(`INSERT INTO warehouse_departure_events(event_type,event_id,payload_hash,payload_json,actor_user_id) VALUES('warehouse_shipment',$1,$2,$3,$4)`, second, hex.EncodeToString(sum[:]), string(raw), f.user); err == nil {
		tx.Rollback()
		t.Fatal("SQL event forged its captured allocation payload")
	}
	tx.Rollback()
	if err = wh.ShipOrder(ctx, second, "post", "TRK-"+second); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 0, 0, 0)
	for _, q := range []string{`UPDATE warehouse_departure_events SET actor_user_id='other' WHERE event_id=$1`, `DELETE FROM warehouse_departure_events WHERE event_id=$1`, `UPDATE warehouse_occupancy_releases SET quantity=2 WHERE event_id=$1`, `DELETE FROM warehouse_occupancy_releases WHERE event_id=$1`} {
		if _, err = db.Exec(q, first); err == nil {
			t.Fatal("departure history mutated")
		}
	}
}

func TestINV06ReturnCreationWaitsForSupplierBeforeProduct(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "tracked"}[tracked], func(t *testing.T) {
			db, f, _, _, sku := inv06Placed(t, tracked, 4)
			creator := operationsDB(t)
			creator.SetMaxOpenConns(1)
			creator.SetMaxIdleConns(1)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var pid int
			if err := creator.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			blocker, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback()
			if _, err = blocker.ExecContext(ctx, `SELECT id FROM suppliers WHERE id=$1 FOR UPDATE`, f.supplier); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				_, err := NewSupplierRepository(creator).CreateReturnRequestForVariantWithIdempotency(ctx, f.supplier, f.product, sku, 1, "supplier_collect", "prefix contention", "prefix-"+uuid.NewString(), f.user)
				result <- err
			}()
			waiting := false
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				var event string
				if err = db.QueryRowContext(ctx, `SELECT COALESCE(wait_event_type,'') FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&event); err != nil {
					blocker.Rollback()
					t.Fatal(err)
				}
				if event == "Lock" {
					waiting = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				blocker.Rollback()
				t.Fatal("creation never reached the held supplier guard")
			}
			_, lockErr := blocker.ExecContext(ctx, `SELECT id FROM products WHERE id=$1 FOR UPDATE NOWAIT`, f.product)
			blocker.Rollback()
			if err = <-result; err != nil {
				t.Fatal(err)
			}
			if lockErr != nil {
				t.Fatalf("creation held product while waiting for supplier, inverting transition prefix: %v", lockErr)
			}
		})
	}
}

func TestINV06LegacyReturnWithoutOriginMetadataKeepsCustodyTransfer(t *testing.T) {
	db := team04TestDB(t)
	storagePositionSeed(t, db)
	bin := "old-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,quantity,occupied_volume_cm3,occupied_weight_grams,capacity_status) VALUES($1,'old','1','1',5,5000,500,'verified')`, bin)
	opsExec(t, db, `INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,qty,stocked_at,original_stocked_at) VALUES('old-position','prod-measured','sup-1','wh-1',5,NOW(),NOW())`)
	opsExec(t, db, `INSERT INTO supplier_return_requests(id,supplier_id,product_id,requested_quantity,status) VALUES('old-return','sup-1','prod-measured',2,'packed')`)
	opsExec(t, db, `INSERT INTO supplier_return_parcels(id,return_request_id,status) VALUES('old-parcel','old-return','confirmed')`)
	opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES('old-source','prod-measured','sup-1','supplier','bin',$1,'return_requested',2,TRUE)`, bin)
	opsExec(t, db, `INSERT INTO inventory_reservations(id,product_id,supplier_id,location_key,quantity,reference_type,reference_id,status,idempotency_key) VALUES('old-reservation','prod-measured','sup-1',$1,2,'supplier_return','old-return','active','old-return-reservation')`, bin)
	if err := NewSupplierRepository(db).HandoverReturn(context.Background(), "old-return", "warehouse-1", "old-handover"); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 5, 5000, 500)
	var status string
	if err := db.QueryRow(`SELECT status FROM supplier_return_requests WHERE id='old-return'`).Scan(&status); err != nil || status != "in_transit" {
		t.Fatalf("legacy custody=%s err=%v", status, err)
	}
	if err := db.QueryRow(`SELECT capacity_status FROM warehouse_bins WHERE bin_code=$1`, bin).Scan(&status); err != nil || status != "disputed" {
		t.Fatalf("unknown origin occupancy=%s err=%v", status, err)
	}
}

func TestINV06SharedBinUnknownProductKeepsWholeBinConservative(t *testing.T) {
	db, f, warehouse, bin, _ := inv06Placed(t, true, 1)
	ctx := context.Background()
	var sku string
	if err := db.QueryRow(`SELECT variant_id FROM warehouse_placements WHERE bin_code=$1`, bin).Scan(&sku); err != nil {
		t.Fatal(err)
	}
	other := "extra-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,stock,original_price,supplier_base_cost,platform_margin) VALUES($1,'historical extra product',$1,$2,0,2000,1000,0)`, other, f.supplier)
	opsExec(t, db, `INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id) VALUES($1,$2,$3,'supplier','bin',$4,'sellable',1,$5)`, "000-unknown-"+uuid.NewString(), other, f.supplier, "bin:"+bin, warehouse)
	opsExec(t, db, `INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,qty,stocked_at,original_stocked_at) VALUES($1,$2,$3,$4,1,NOW(),NOW())`, uuid.NewString(), other, f.supplier, warehouse)
	order, token := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE order_items SET variant_id=$2 WHERE order_id=$1`, order, sku)
	opsExec(t, db, `INSERT INTO order_items(id,order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price) VALUES($1,$2,$3,1,1000,0,1000,2000)`, uuid.NewString(), order, other)
	opsExec(t, db, `UPDATE orders SET total_amount=4000,payable_amount=4000 WHERE id=$1`, order)
	opsExec(t, db, `UPDATE payment_transactions SET amount=4000 WHERE order_id=$1`, order)
	seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
	if _, err := NewOrderRepository(db).MarkPaymentPaid(ctx, token, "r"+uuid.NewString()[:20], "mask"); err != nil {
		t.Fatal(err)
	}
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err := NewWarehouseRepository(db).ShipOrder(ctx, order, "post", "TRK-"+order); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 1, 1000, 100)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM warehouse_occupancy_releases WHERE event_id=$1`, order).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partly freed a bin with unknown mixed-product history=%d err=%v", count, err)
	}
}

func TestINV06SelectedLegacySKUUsesPhysicalLegacyPlacement(t *testing.T) {
	db, f, warehouse, bin, _ := inv06Placed(t, false, 1)
	sku := uuid.NewString()
	opsExec(t, db, `INSERT INTO product_variants(id,product_id,spnc,inventory_mode,stock) VALUES($1,$2,'LEG-SELECT','legacy',1)`, sku, f.product)
	order := inv06PaidOrder(t, db, f, warehouse, sku)
	opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
	if err := NewWarehouseRepository(db).ShipOrder(context.Background(), order, "post", "TRK-"+order); err != nil {
		t.Fatal(err)
	}
	inv06AssertOccupancy(t, db, bin, 0, 0, 0)
}
