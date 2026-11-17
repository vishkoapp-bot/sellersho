package repository

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"os"
	"sync"
	"testing"
)

func TestAuditSupplyReceiptDoesNotRecreateSoldTransferredUnits(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := inboundFixtureDB(t, schema)
	repo := NewSupplierRepository(db)
	if _, err := repo.CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 1}}, "w1", "booking"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("booked unit remains sellable: stock=%d; payment can claim the same transferred unit", n)
	}
	var sh, code string
	if err := db.QueryRow(`SELECT id,shipment_code FROM shipments WHERE creation_key='booking'`).Scan(&sh, &code); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"booked", "dispatched", "received"} {
		if stage == "dispatched" {
			if err := repo.ChangeCartonDispatch(context.Background(), "s", sh, false); err != nil {
				t.Fatal(err)
			}
		}
		if stage == "received" {
			if _, err := repo.ReceiveWarehouseShipment(context.Background(), code, []WarehouseReceiveItem{{red, 1, "pass"}}); err != nil {
				t.Fatal(err)
			}
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items VALUES($1,$2,'p',$3,1)`, uuid.NewString(), stage, red); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if _, err := captureOrderInventoryTx(context.Background(), tx, stage); err != ErrInsufficientInventory {
			tx.Rollback()
			t.Fatalf("%s unit captured twice: %v", stage, err)
		}
		tx.Rollback()
	}

}

func inboundFixtureDB(t *testing.T, schema string) *sql.DB { return skuScopedDB(t, schema) }
func TestInboundTransferCumulativeReceiptAndCancel(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := inboundFixtureDB(t, schema)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE inventory_balances SET quantity=5 WHERE variant_id=$1`, red); err != nil {
		t.Fatal(err)
	}
	shipment, err := repo.CreateCartonShipment(ctx, "s", []CartonShipmentItem{{red, 5}}, "w1", "five")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReceiveWarehouseShipment(ctx, shipment.ShipmentCode, []WarehouseReceiveItem{{red, 1, "pass"}}); err == nil {
		t.Fatal("undispatched receipt accepted")
	}
	if err := repo.ChangeCartonDispatch(ctx, "s", shipment.ID, false); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{2, 5, 5} {
		if _, err := repo.ReceiveWarehouseShipment(ctx, shipment.ShipmentCode, []WarehouseReceiveItem{{red, n, "pass"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.ReceiveWarehouseShipment(ctx, shipment.ShipmentCode, []WarehouseReceiveItem{{red, 6, "pass"}}); err == nil {
		t.Fatal("over-receipt accepted")
	}
	if err := repo.ChangeCartonDispatch(ctx, "s", shipment.ID, true); err != nil {
		t.Fatal(err)
	}
	var total, sellable int
	if err := db.QueryRow(`SELECT SUM(quantity),COALESCE(SUM(quantity) FILTER(WHERE status='sellable'),0) FROM inventory_balances WHERE variant_id=$1`, red).Scan(&total, &sellable); err != nil {
		t.Fatal(err)
	}
	if total != 5 || sellable != 0 {
		t.Fatalf("total=%d sellable=%d", total, sellable)
	}
}
func TestInboundTransferCancellationReleasesOnce(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := inboundFixtureDB(t, schema)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	sh, err := repo.CreateCartonShipment(ctx, "s", []CartonShipmentItem{{red, 1}}, "w1", "cancel")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := repo.ChangeCartonDispatch(ctx, "s", sh.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ChangeCartonDispatch(ctx, "s", sh.ID, false); err == nil {
		t.Fatal("cancelled transfer dispatched")
	}
	var n int
	if err := db.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cancel restored %d", n)
	}
}

func TestInboundTransferBookingPaymentRace(t *testing.T) {
	for iteration := 0; iteration < 4; iteration++ {
		t.Run(fmtInt(iteration), func(t *testing.T) {
			_, schema, red, _ := skuInventoryFixture(t)
			db := inboundFixtureDB(t, schema)
			for _, q := range []string{`INSERT INTO orders(id,status,payment_status) VALUES('race','processing','pending')`, `INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,amount,status) VALUES('race','race','race','race',1000,'pending')`} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO order_items VALUES($1,'race','p',$2,1)`, uuid.NewString(), red); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, err := NewOrderRepository(db).MarkPaymentPaid(context.Background(), "race", "rrn", "masked")
				results <- err
			}()
			go func() {
				defer wg.Done()
				<-start
				_, err := NewSupplierRepository(db).CreateCartonShipment(context.Background(), "s", []CartonShipmentItem{{red, 1}}, "w1", "race")
				results <- err
			}()
			close(start)
			wg.Wait()
			close(results)
			wins := 0
			for err := range results {
				if err == nil {
					wins++
				}
			}
			if wins != 1 {
				t.Fatalf("one unit allowed %d successful competing claims", wins)
			}
			var quantity int
			if err := db.QueryRow(`SELECT COALESCE(SUM(quantity),0)+(SELECT COALESCE(SUM(quantity),0) FROM order_inventory_captures) FROM inventory_balances WHERE variant_id=$1`, red).Scan(&quantity); err != nil {
				t.Fatal(err)
			}
			if quantity != 1 {
				t.Fatalf("conserved quantity=%d", quantity)
			}
		})
	}
}
func fmtInt(n int) string { return string(rune('0' + n)) }
func TestInboundTransferExternalReferenceAndUnknownSource(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := inboundFixtureDB(t, schema)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	source := CartonInboundSource{Kind: "external_replenishment", ExternalReference: "supplier-delivery-note-1", Reason: "independent counted purchase"}
	sh, err := repo.CreateCartonShipmentFromSource(ctx, "s", []CartonShipmentItem{{red, 1}}, source, "w1", "external")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ChangeCartonDispatch(ctx, "s", sh.ID, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := repo.ReceiveWarehouseShipment(ctx, sh.ShipmentCode, []WarehouseReceiveItem{{red, 1, "pass"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.CreateCartonShipmentFromSource(ctx, "s", []CartonShipmentItem{{red, 1}}, source, "w1", "cross-key"); err == nil {
		t.Fatal("cross-key external reference reused")
	}
	if _, err := repo.CreateCartonShipmentFromSource(ctx, "s", []CartonShipmentItem{{red, 2}}, source, "w1", "changed"); err == nil {
		t.Fatal("external reference changed quantity")
	}
	if _, err := repo.CreateCartonShipmentFromSource(ctx, "s", []CartonShipmentItem{{red, 1}}, CartonInboundSource{Kind: "transfer", ExternalReference: source.ExternalReference}, "w1", "cross-mode"); err == nil {
		t.Fatal("cross-mode evidence reused")
	}
	if _, err := repo.CreateCartonShipmentFromSource(ctx, "s", []CartonShipmentItem{{red, 1}}, CartonInboundSource{Kind: "transfer", SupplierWarehouseID: "unknown"}, "w1", "unknown"); err == nil {
		t.Fatal("unknown source accepted")
	}
	var quantity int
	if err := db.QueryRow(`SELECT SUM(quantity) FROM inventory_balances WHERE variant_id=$1`, red).Scan(&quantity); err != nil {
		t.Fatal(err)
	}
	if quantity != 2 {
		t.Fatalf("initial 1 plus new 1 yielded %d", quantity)
	}
	if _, err := db.Exec(`UPDATE inbound_inventory_lines SET quantity=quantity+1 WHERE shipment_id=$1`, sh.ID); err == nil {
		t.Fatal("immutable provenance changed")
	}
}

func TestInboundTransferQCRequiresExactReceiptProvenance(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := inboundFixtureDB(t, schema)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := NewWarehouseRepository(db).RecordQCPass(context.Background(), "p", "unintegrated", "", 1); err != ErrInboundProvenance {
		t.Fatalf("QC without exact receipt provenance enabled: %v", err)
	}
	var n int
	if err := tx.QueryRow(`SELECT stock FROM product_variants WHERE id=$1`, red).Scan(&n); err != nil || n != 1 {
		t.Fatalf("gate changed stock %d %v", n, err)
	}
}

func TestInboundTransferHistoricalReceiptReplayDoesNotBackfill(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := inboundFixtureDB(t, schema)
	id := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO shipments(id,shipment_code,status) VALUES($1,'HISTORY','received');`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty,received_qty,inventory_received_qty,qc_status) VALUES($1,$2,1,1,1,'passed')`, id, red); err != nil {
		t.Fatal(err)
	}
	repo := NewSupplierRepository(db)
	if _, err := repo.ReceiveWarehouseShipment(context.Background(), "HISTORY", []WarehouseReceiveItem{{red, 1, "pass"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReceiveWarehouseShipment(context.Background(), "HISTORY", []WarehouseReceiveItem{{red, 2, "pass"}}); err == nil {
		t.Fatal("historical receipt expanded")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inbound_inventory_lines WHERE shipment_id=$1`, id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("history backfilled %d %v", n, err)
	}
}

func TestInboundTransferMigrationClassificationAndRollbackGuard(t *testing.T) {
	_, schema, _, _ := skuInventoryFixture(t)
	db := inboundFixtureDB(t, schema)
	for _, status := range []string{"draft", "in_transit", "received"} {
		if _, err := db.Exec(`INSERT INTO shipments(id,status,shipment_code) VALUES($1,$2,$2)`, uuid.NewString(), status); err != nil {
			t.Fatal(err)
		}
	}
	var exceptions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM inbound_transfer_reconciliation`).Scan(&exceptions); err != nil || exceptions != 3 {
		t.Fatalf("classification=%d err=%v", exceptions, err)
	}
	down, err := os.ReadFile("../../migrations/202611140006_inventory_transfer_provenance.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	// in_transit is a live additive state and prevents reverting the old constraint.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(string(down)); err == nil {
		tx.Rollback()
		t.Fatal("rollback discarded live transfer state")
	}
	tx.Rollback()
	var total int
	if err := db.QueryRow(`SELECT SUM(quantity) FROM inventory_balances`).Scan(&total); err != nil || total != 4 {
		t.Fatalf("classification changed units %d %v", total, err)
	}
}
