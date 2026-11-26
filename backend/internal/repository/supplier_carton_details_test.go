package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSupplierCartonDetailsOwnReceiptAndPrintScope(t *testing.T) {
	_, schema, red, blue := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	inv04Migrate(t, db)
	migration, err := os.ReadFile("../../migrations/202611250001_inbound_document_per_shipment.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	carton, _, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 3}, CartonShipmentItem{blue, 2})
	other, _, otherLines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 2})
	first, err := repo.ReceiveInboundLine(ctx, lines[red], 2, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.ReceiveInboundLine(ctx, otherLines[red], 2, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.GetSupplierCartonDetails(ctx, "s", carton)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != carton || len(detail.Lines) != 2 {
		t.Fatalf("detail = %+v", detail)
	}
	lineByVariant := map[string]SupplierCartonLine{}
	for _, line := range detail.Lines {
		lineByVariant[line.VariantID] = line
	}
	redLine, blueLine := lineByVariant[red], lineByVariant[blue]
	if redLine.ExpectedQty != 3 || redLine.ReceivedQty != 2 || redLine.PutawayQty == nil || *redLine.PutawayQty != 0 || len(redLine.Units) != 2 || redLine.Units[0].Code != first.UnitCodes[0] {
		t.Fatalf("red line = %+v", redLine)
	}
	if blueLine.ExpectedQty != 2 || blueLine.ReceivedQty != 0 || len(blueLine.Units) != 0 {
		t.Fatalf("blue line = %+v", blueLine)
	}
	if _, err := repo.GetSupplierCartonDetails(ctx, "different", carton); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another supplier's detail: %v", err)
	}
	req := MarkUnitsPrintedRequest{SupplierID: "s", CartonShipmentID: carton, ReceiptLineID: &redLine.ID, IdempotencyKey: uuid.NewString(), UnitCodes: []string{first.UnitCodes[0], second.UnitCodes[0]}}
	if _, err := repo.MarkUnitsPrinted(ctx, req); !errors.Is(err, ErrPrintNotPrintable) {
		t.Fatalf("mixed carton print: %v", err)
	}
	req.CartonShipmentID = other
	if _, err := repo.MarkUnitsPrinted(ctx, req); !errors.Is(err, ErrPrintReceiptLineNotFound) {
		t.Fatalf("mismatched carton and line: %v", err)
	}
	req.CartonShipmentID = carton
	req.UnitCodes = first.UnitCodes
	printed, err := repo.MarkUnitsPrinted(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	var job printedUnitLabels
	if err := json.Unmarshal(printed, &job); err != nil || len(job.Units) != 2 || job.Units[0].Code != first.UnitCodes[0] || job.Units[0].PrintCount != 1 {
		t.Fatalf("print = %s, err = %v", printed, err)
	}
	replay, err := repo.MarkUnitsPrinted(ctx, req)
	if err != nil || string(replay) != string(printed) {
		t.Fatalf("retry = %s, err = %v", replay, err)
	}
	req.IdempotencyKey = uuid.NewString()
	req.Reprint = true
	reprint, err := repo.MarkUnitsPrinted(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reprint, &job); err != nil || len(job.Units) != 2 || job.Units[0].PrintCount != 2 {
		t.Fatalf("reprint = %s, err = %v", reprint, err)
	}

	const task, stage = "PUT-carton-detail", "qc-accepted:carton-detail"
	opsExec(t, db, `INSERT INTO putaway_tasks(id,product_id,variant_id,inbound_id,warehouse_id,inventory_source_location_key,quantity,status)
		VALUES($1,'p',$2,$3,'w1',$4,1,'pending')`, task, red, carton, stage)
	opsExec(t, db, `INSERT INTO inbound_putaway_allocations(task_id,disposition_id,receipt_line_id,shipment_id,supplier_id,warehouse_id,product_id,variant_id,source_location_key,quantity)
		VALUES($1,'QC-carton-detail',$2,$3,'s','w1','p',$4,$5,1)`, task, redLine.ID, carton, red, stage)
	opsExec(t, db, `INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key)
		VALUES('MV-carton-detail','p','s',$1,1,'qc_accepted','sellable',$2,'bin:A-1','putaway_confirmed',$3,$4)`, red, stage, task, "putaway:"+task)
	opsExec(t, db, `UPDATE putaway_tasks SET status='completed' WHERE id=$1`, task)
	after, err := repo.GetSupplierCartonDetails(ctx, "s", carton)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range after.Lines {
		want := 0
		if line.VariantID == red {
			want = 1
		}
		if line.PutawayQty == nil || *line.PutawayQty != want {
			t.Fatalf("carton putaway line: %+v, want %d", line, want)
		}
	}
	opsExec(t, db, `INSERT INTO putaway_tasks(id,product_id,variant_id,inbound_id,warehouse_id,quantity,status)
		VALUES('PUT-no-exact-allocation','p',$1,$2,'w1',1,'completed')`, red, carton)
	after, err = repo.GetSupplierCartonDetails(ctx, "s", carton)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range after.Lines {
		if line.VariantID == red && line.PutawayQty != nil {
			t.Fatalf("historical putaway without exact allocation reported as confirmed: %+v", line)
		}
	}
}

func TestSupplierCartonDetailsLegacyReceiptHasUnknownPutaway(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	inv04Migrate(t, db)
	carton := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO shipments(id,shipment_code,supplier_id,status) VALUES($1,'LEGACY42','s','partially_received')`, carton); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO shipment_items(shipment_id,variant_id,expected_qty,received_qty,qc_status) VALUES($1,$2,3,2,'failed')`, carton, red); err != nil {
		t.Fatal(err)
	}
	got, err := NewSupplierRepository(db).GetSupplierCartonDetails(context.Background(), "s", carton)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 1 || got.Lines[0].ExpectedQty != 3 || got.Lines[0].ReceivedQty != 2 || got.Lines[0].PutawayQty != nil || len(got.Lines[0].Units) != 0 {
		t.Fatalf("legacy detail = %+v", got.Lines)
	}
	opsExec(t, db, `UPDATE product_variants SET spnc=NULL WHERE id=$1`, red)
	legacy, err := NewSupplierRepository(db).GetWarehouseShipmentByCode(context.Background(), "LEGACY42")
	if err != nil || len(legacy.Items) != 1 || legacy.Items[0].ReceivedQty != 2 || legacy.Items[0].SPNC != "" {
		t.Fatalf("legacy code lookup without a SKU code: %+v (%v)", legacy, err)
	}
}

func TestCartonRejectsDuplicateUnscannedReceiptLines(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	_, code, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 2})
	_, err := repo.ReceiveWarehouseShipment(ctx, code, []WarehouseReceiveItem{
		{VariantID: red, ReceivedQty: 1, QCStatus: "pass"},
		{VariantID: red, ReceivedQty: 2, QCStatus: "pass"},
	})
	if err == nil {
		t.Error("duplicate input recorded two receipt deltas for one SKU")
	}
	assertReceiveLot(t, db, lines[red], 0, 0, 0)
}

func TestSupplierCartonDetailsMovesTransferUnitBetweenCartons(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	inv04Migrate(t, db)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	items := []CartonShipmentItem{{red, 1}}
	first, err := repo.CreateCartonShipment(ctx, "s", items, "w1", "prep-first")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.GetSupplierCartonDetails(ctx, "s", first.ID)
	if err != nil || len(detail.Lines) != 1 || len(detail.Lines[0].Units) != 1 {
		t.Fatalf("reserved unit belongs to first carton: %+v (%v)", detail, err)
	}
	unit := detail.Lines[0].Units[0].Code
	oldLine := detail.Lines[0].ID
	if err := repo.ChangeCartonDispatch(ctx, "s", first.ID, true); err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateCartonShipment(ctx, "s", items, "w1", "prep-second")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = repo.GetSupplierCartonDetails(ctx, "s", first.ID)
	if err != nil || len(detail.Lines) != 1 || len(detail.Lines[0].Units) != 0 {
		t.Fatalf("cancelled carton retained label: %+v (%v)", detail, err)
	}
	detail, err = repo.GetSupplierCartonDetails(ctx, "s", second.ID)
	if err != nil || len(detail.Lines) != 1 || len(detail.Lines[0].Units) != 1 || detail.Lines[0].Units[0].Code != unit {
		t.Fatalf("reserved unit belongs to second carton: %+v (%v)", detail, err)
	}
	_, err = repo.MarkUnitsPrinted(ctx, MarkUnitsPrintedRequest{
		SupplierID: "s", CartonShipmentID: first.ID, ReceiptLineID: &oldLine,
		IdempotencyKey: uuid.NewString(), UnitCodes: []string{unit},
	})
	if !errors.Is(err, ErrPrintReceiptLineNotFound) {
		t.Fatalf("cancelled carton print: %v", err)
	}
}

func TestCartonExactLineReceiptUpdatesListAndCodeLookup(t *testing.T) {
	db, red, blue := inv04CanonicalFixture(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	carton, code, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 1}, CartonShipmentItem{blue, 1})
	if _, err := repo.ReceiveInboundLine(ctx, lines[red], 1, nil, ""); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListCartonShipments(ctx, "s")
	if err != nil || len(list) != 1 || list[0].Status != "partially_received" {
		t.Fatalf("partially received carton list: %+v (%v)", list, err)
	}
	lookup, err := repo.GetWarehouseShipmentByCode(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range lookup.Items {
		if item.VariantID == red && (item.ReceivedQty != 1 || item.QCStatus != "pending") {
			t.Fatalf("receipt counter or pending QC lost on code lookup: %+v", item)
		}
	}
	status, err := repo.ReceiveWarehouseShipment(ctx, code, []WarehouseReceiveItem{{VariantID: blue, ReceivedQty: 1, QCStatus: "pass"}})
	if status != "received" || err != nil {
		t.Fatalf("receipt by code after exact-line receipt: %q (%v)", status, err)
	}
	detail, err := repo.GetSupplierCartonDetails(ctx, "s", carton)
	if err != nil || detail.Status != "received" || detail.ReceivedAt == nil {
		t.Fatalf("supplier receipt header: %+v (%v)", detail, err)
	}
}

func TestCartonCannotReceiveZeroBeforeDispatchOrAfterCancellation(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	carton, err := repo.CreateCartonShipment(ctx, "s", []CartonShipmentItem{{red, 1}}, "w1", "closed-receipt")
	if err != nil {
		t.Fatal(err)
	}
	for _, wantStatus := range []string{"draft", "cancelled"} {
		if wantStatus == "cancelled" {
			if err := repo.ChangeCartonDispatch(ctx, "s", carton.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		_, err := repo.ReceiveWarehouseShipment(ctx, carton.ShipmentCode, []WarehouseReceiveItem{{VariantID: red, ReceivedQty: 0, QCStatus: "pass"}})
		var status string
		var receivedAt *time.Time
		if readErr := db.QueryRow(`SELECT status,received_at FROM shipments WHERE id=$1`, carton.ID).Scan(&status, &receivedAt); readErr != nil {
			t.Fatal(readErr)
		}
		if err == nil || status != wantStatus || receivedAt != nil {
			t.Errorf("zero receipt for %s carton: status=%s received_at=%v err=%v", wantStatus, status, receivedAt, err)
		}
	}
}

func TestCartonCannotCancelDispatchedInventory(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	id, _, _ := receiveTestCarton(t, db, false, CartonShipmentItem{red, 1})
	if err := repo.ChangeCartonDispatch(ctx, "s", id, true); err == nil {
		t.Fatal("cancel acknowledged for a fully dispatched carton")
	}
}

func TestCartonCumulativeScanAfterPriorUnitsPassQC(t *testing.T) {
	db, red, _ := inv04CanonicalFixture(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	_, _, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 2})
	line := lines[red]
	first := "INV04-RED-X0001"
	second := "INV04-RED-X0002"
	if _, err := repo.ReceiveInboundLine(ctx, line, 1, []string{first}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWarehouseRepository(db).RecordQCLinePassDelta(ctx, line, 1, []string{first}, ""); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ReceiveInboundLine(ctx, line, 2, []string{first, second}, "")
	assertReceiveResult(t, got, err, line, 2, []string{first, second})
	assertReceiveLot(t, db, line, 2, 0, 1)
}

func TestCartonReadsReconcileHistoricalReceiptProjections(t *testing.T) {
	db, red, _ := inv04CanonicalFixture(t)
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	carton, code, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 1})
	if _, err := repo.ReceiveInboundLine(ctx, lines[red], 1, nil, ""); err != nil {
		t.Fatal(err)
	}
	// Earlier exact line receipts updated only the canonical inventory line.
	opsExec(t, db, `UPDATE shipments SET status='in_transit',received_at=NULL WHERE id=$1`, carton)
	opsExec(t, db, `UPDATE shipment_items SET received_qty=0,inventory_received_qty=0 WHERE shipment_id=$1`, carton)
	list, err := repo.ListCartonShipments(ctx, "s")
	if err != nil || len(list) != 1 || list[0].Status != "received" {
		t.Errorf("historic carton list: %+v (%v)", list, err)
	}
	counts, err := repo.GetSupplierWorkCounts(ctx, "s")
	if err != nil || counts.OpenCartons != 0 {
		t.Errorf("historic receipt left in open carton count: %+v (%v)", counts, err)
	}
	lookup, err := repo.GetWarehouseShipmentByCode(ctx, code)
	if err != nil || len(lookup.Items) != 1 || lookup.Status != "received" || lookup.Items[0].ReceivedQty != 1 {
		t.Errorf("historic receipt code lookup: %+v (%v)", lookup, err)
	}
	detail, err := repo.GetSupplierCartonDetails(ctx, "s", carton)
	if err != nil || detail.Status != "received" || detail.ReceivedAt == nil {
		t.Errorf("historic receipt detail: %+v (%v)", detail, err)
	}
}
