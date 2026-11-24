package repository

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestSPNCXReceivePreprintedCode(t *testing.T) {
	for _, tc := range []struct {
		code string
		seq  int
	}{
		{"SKU-X0001", 1},
		{"SKU-X9999", 9999},
		{"SKU-X0000", 0},
		{"SKU-X10000", 0},
		{"OTHER-X0001", 0},
		{"SKU-X+001", 0},
	} {
		seq, ok := parsePreprintedCode(tc.code, "SKU")
		if seq != tc.seq || ok != (tc.seq > 0) {
			t.Errorf("%s: got (%d, %t), want sequence %d", tc.code, seq, ok, tc.seq)
		}
	}
}

func receiveTestCarton(t *testing.T, db *sql.DB, external bool, items ...CartonShipmentItem) (shipmentID, code string, lines map[string]string) {
	t.Helper()
	ctx := context.Background()
	repo := NewSupplierRepository(db)
	key := uuid.NewString()
	source := CartonInboundSource{Kind: "transfer"}
	if external {
		source = CartonInboundSource{Kind: "external_replenishment", ExternalReference: key, Reason: "test delivery"}
	}
	shipment, err := repo.CreateCartonShipmentFromSource(ctx, "s", items, source, "w1", key)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.ChangeCartonDispatch(ctx, "s", shipment.ID, false); err != nil {
		t.Fatal(err)
	}
	lines = map[string]string{}
	for _, item := range items {
		var line string
		if err := db.QueryRow(`SELECT id FROM inbound_inventory_lines WHERE shipment_id=$1 AND variant_id=$2`, shipment.ID, item.VariantID).Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines[item.VariantID] = line
	}
	return shipment.ID, shipment.ShipmentCode, lines
}

func receiveTestCodes(t *testing.T, db *sql.DB, variant, line, lotStatus string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT u.code FROM product_units u JOIN inventory_balances b ON b.id=u.balance_id
		WHERE u.variant_id=$1 AND ($2='' OR b.location_key='inbound-line:'||$2) AND ($3='' OR b.status=$3) ORDER BY u.sequence`, variant, line, lotStatus)
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

func assertReceiveResult(t *testing.T, got UnitReceiptResult, err error, line string, qty int, codes []string) {
	t.Helper()
	if err != nil || got.LineID != line || got.Quantity != qty || !slices.Equal(got.UnitCodes, codes) || got.UnitCodes == nil || got.Mismatched == nil || len(got.Mismatched) != 0 {
		t.Fatalf("receipt=%+v err=%v, want line %s quantity %d codes %v", got, err, line, qty, codes)
	}
}

func assertReceiveMismatch(t *testing.T, err error, line, code, actual string) {
	t.Helper()
	var mismatch *UnitMismatchError
	if !errors.Is(err, ErrUnitMismatch) || !errors.As(err, &mismatch) || mismatch.LineID != line {
		t.Fatalf("got %v (%+v), want unit mismatch for %s", err, mismatch, line)
	}
	for _, item := range mismatch.Errors {
		if item.Code == code && (actual == "" || item.Actual == actual) && item.Expected != "" && item.Error == "unit_mismatch" {
			return
		}
	}
	t.Fatalf("missing entry for %q (%q) in %+v", code, actual, mismatch.Errors)
}

func assertReceiveLot(t *testing.T, db *sql.DB, line string, receivedQty, transitQty, quarantineQty int) {
	t.Helper()
	var received, transit, quarantine int
	if err := db.QueryRow(`SELECT l.received_quantity,
		COALESCE(SUM(b.quantity) FILTER(WHERE b.status='in_transit'),0),
		COALESCE(SUM(b.quantity) FILTER(WHERE b.status='quarantine'),0)
		FROM inbound_inventory_lines l LEFT JOIN inventory_balances b ON b.location_key='inbound-line:'||l.id WHERE l.id=$1 GROUP BY l.id`, line).Scan(&received, &transit, &quarantine); err != nil {
		t.Fatal(err)
	}
	if received != receivedQty || transit != transitQty || quarantine != quarantineQty {
		t.Fatalf("received/transit/quarantine: %d/%d/%d, want %d/%d/%d", received, transit, quarantine, receivedQty, transitQty, quarantineQty)
	}
}

func TestSPNCXReceiveExternalMint(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	_, _, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 3})
	line := lines[red]
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	want := []string{"A0C00001-X0002", "A0C00001-X0003", "A0C00001-X0004"}
	for i := 0; i < 2; i++ {
		got, err := repo.ReceiveInboundLine(ctx, line, 3, nil, "")
		assertReceiveResult(t, got, err, line, 3, want)
	}
	assertReceiveLot(t, db, line, 3, 0, 3)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_units u JOIN product_unit_events e ON e.unit_id=u.id
		JOIN inventory_balances b ON b.id=u.balance_id
		WHERE u.variant_id=$1 AND u.status='quarantined' AND b.status='quarantine' AND b.location_key='inbound-line:'||$2
		AND e.event_type='minted' AND e.reference_type='inbound_receive' AND e.reference_id=$2`, red, line).Scan(&count); err != nil || count != 3 {
		t.Fatalf("quarantined receive mints=%d err=%v", count, err)
	}
}

func TestSPNCXReceiveExternalPreprintedAndRepeat(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	_, _, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 4})
	line := lines[red]
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	a, b, c := "A0C00001-X0002", "A0C00001-X0004", "A0C00001-X0003"
	_, err := repo.ReceiveInboundLine(ctx, line, 1, []string{"OTHER-X0002"}, "")
	assertReceiveMismatch(t, err, line, "OTHER-X0002", "OTHER")
	_, err = repo.ReceiveInboundLine(ctx, line, 1, []string{"A0C00001-X0001"}, "")
	assertReceiveMismatch(t, err, line, "A0C00001-X0001", "in_stock")
	for i := 0; i < 2; i++ {
		got, err := repo.ReceiveInboundLine(ctx, line, 2, []string{b, a}, "")
		assertReceiveResult(t, got, err, line, 2, []string{a, b})
	}
	_, err = repo.ReceiveInboundLine(ctx, line, 2, []string{a, a}, "")
	assertReceiveMismatch(t, err, line, a, "duplicate")
	got, err := repo.ReceiveInboundLine(ctx, line, 3, []string{b, c, a}, "")
	assertReceiveResult(t, got, err, line, 3, []string{a, c, b})
	got, err = repo.ReceiveInboundLine(ctx, line, 4, nil, "")
	assertReceiveResult(t, got, err, line, 4, []string{a, c, b, "A0C00001-X0005"})
	assertReceiveLot(t, db, line, 4, 0, 4)
}

func TestSPNCXReceivePreprintedCap(t *testing.T) {
	_, schema, red, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	_, _, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 2})
	line := lines[red]
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	tooHigh := "A0C00001-X10000"
	_, err := repo.ReceiveInboundLine(ctx, line, 1, []string{tooHigh}, "")
	assertReceiveMismatch(t, err, line, tooHigh, tooHigh)
	last := "A0C00001-X9999"
	result, err := repo.ReceiveInboundLine(ctx, line, 1, []string{last}, "")
	assertReceiveResult(t, result, err, line, 1, []string{last})
	_, err = repo.ReceiveInboundLine(ctx, line, 2, nil, "")
	var capError *UnitCapError
	if !errors.As(err, &capError) || capError.Available != 0 {
		t.Fatalf("expected exhausted counter, got %v (%+v)", err, capError)
	}
	assertReceiveLot(t, db, line, 1, 0, 1)
}

func TestSPNCXReceiveTransferPreservesUnitIdentity(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "fifo"
		if explicit {
			name = "scanned"
		}
		t.Run(name, func(t *testing.T) {
			_, schema, red, blue := skuInventoryFixture(t)
			db := skuScopedDB(t, schema)
			before := receiveTestCodes(t, db, blue, "", "")
			_, _, lines := receiveTestCarton(t, db, false, CartonShipmentItem{blue, 3})
			line := lines[blue]
			repo := NewSupplierRepository(db)
			ctx := context.Background()
			if explicit {
				wrong := receiveTestCodes(t, db, red, "", "")[0]
				_, err := repo.ReceiveInboundLine(ctx, line, 1, []string{wrong}, "")
				assertReceiveMismatch(t, err, line, wrong, "in_stock")
				_, err = repo.ReceiveInboundLine(ctx, line, 1, []string{"UNKNOWN-X0001"}, "")
				assertReceiveMismatch(t, err, line, "UNKNOWN-X0001", "not_found")
				assertReceiveLot(t, db, line, 0, 3, 0)
				got, err := repo.ReceiveInboundLine(ctx, line, 2, []string{before[2], before[1]}, "")
				assertReceiveResult(t, got, err, line, 2, before[1:])
				assertReceiveLot(t, db, line, 2, 1, 2)
				got, err = repo.ReceiveInboundLine(ctx, line, 3, before, "")
				assertReceiveResult(t, got, err, line, 3, before)
			} else {
				got, err := repo.ReceiveInboundLine(ctx, line, 3, nil, "")
				assertReceiveResult(t, got, err, line, 3, before)
			}
			assertReceiveLot(t, db, line, 3, 0, 3)
			var total, quarantined int
			if err := db.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER(WHERE status='quarantined') FROM product_units WHERE variant_id=$1`, blue).Scan(&total, &quarantined); err != nil || total != 3 || quarantined != 3 {
				t.Fatalf("total=%d quarantined=%d err=%v", total, quarantined, err)
			}
		})
	}
}

func TestSPNCXReceiveQCLineDelta(t *testing.T) {
	db, red, blue := inv04CanonicalFixture(t)
	_, _, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 3})
	line := lines[red]
	ctx := context.Background()
	received, err := NewSupplierRepository(db).ReceiveInboundLine(ctx, line, 3, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := received.UnitCodes[0], received.UnitCodes[1], received.UnitCodes[2]
	repo := NewWarehouseRepository(db)
	const wrong = "INV04-BLUE-X0001"
	opsExec(t, db, `INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id)
		SELECT $1,variant_id,product_id,1,'in_stock',id FROM inventory_balances WHERE variant_id=$2 AND status='sellable'`, wrong, blue)
	_, err = repo.RecordQCLinePassDelta(ctx, line, 1, []string{wrong}, "")
	assertReceiveMismatch(t, err, line, wrong, "in_stock")
	_, err = repo.RecordQCLinePassDelta(ctx, line, 2, []string{a}, "")
	assertReceiveMismatch(t, err, line, "", "1 unit codes")
	assertReceiveLot(t, db, line, 3, 0, 3)
	got, err := repo.RecordQCLinePassDelta(ctx, line, 2, []string{b, c}, "")
	assertReceiveResult(t, got, err, line, 2, []string{b, c})
	assertReceiveLot(t, db, line, 3, 0, 1)
	if remaining := receiveTestCodes(t, db, red, line, "quarantine"); !slices.Equal(remaining, []string{a}) {
		t.Fatalf("left in quarantine: %v", remaining)
	}
	got, err = repo.RecordQCLinePassDelta(ctx, line, 1, []string{a}, "")
	assertReceiveResult(t, got, err, line, 1, []string{a})
	var accepted int
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_units u JOIN inventory_balances b ON b.id=u.balance_id
		JOIN inbound_qc_dispositions d ON b.location_key='qc-accepted:'||d.id
		WHERE d.receipt_line_id=$1 AND u.status='defined' AND b.status='qc_accepted'`, line).Scan(&accepted); err != nil || accepted != 3 {
		t.Fatalf("accepted=%d err=%v", accepted, err)
	}
}

func TestSPNCXReceiveQCMixed(t *testing.T) {
	db, red, _ := inv04CanonicalFixture(t)
	shipment, _, lines := receiveTestCarton(t, db, true, CartonShipmentItem{red, 5})
	line := lines[red]
	ctx := context.Background()
	received, err := NewSupplierRepository(db).ReceiveInboundLine(ctx, line, 4, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	codes := received.UnitCodes
	repo := NewWarehouseRepository(db)
	err = repo.CompleteQCWithUnits(ctx, "p", shipment, "", 2, 1, 0, "mixed", codes[:3], line)
	assertReceiveMismatch(t, err, line, "", "3 unit codes")
	assertReceiveLot(t, db, line, 4, 0, 4)
	// Scan the first FIFO code as failed. The accepted set must skip it.
	if err := repo.CompleteQCWithUnits(ctx, "p", shipment, "", 2, 1, 0, "mixed", codes[:1], line); err != nil {
		t.Fatal(err)
	}
	err = repo.CompleteQCWithUnits(ctx, "p", shipment, "", 2, 1, 1, "shortage", codes[3:], line)
	assertReceiveMismatch(t, err, line, codes[3], "unit_codes present")
	for i, want := range []string{"damaged", "defined", "defined", "quarantined"} {
		var status string
		if err := db.QueryRow(`SELECT status FROM product_units WHERE code=$1`, codes[i]).Scan(&status); err != nil || status != want {
			t.Fatalf("%s status=%s want %s, err=%v", codes[i], status, want, err)
		}
	}
	got, err := repo.RecordQCLinePassDelta(ctx, line, 1, codes[3:], "")
	assertReceiveResult(t, got, err, line, 1, codes[3:])
	var reason string
	if err := db.QueryRow(`SELECT reason FROM inbound_qc_dispositions WHERE receipt_line_id=$1 AND cumulative_disposed=4`, line).Scan(&reason); err != nil || reason != "mixed" {
		t.Fatalf("prior reason=%q err=%v", reason, err)
	}
}

func TestSPNCXReceiveShipmentAtomic(t *testing.T) {
	_, schema, red, blue := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	_, _, other := receiveTestCarton(t, db, false, CartonShipmentItem{blue, 1})
	_, shipment, lines := receiveTestCarton(t, db, false, CartonShipmentItem{red, 1}, CartonShipmentItem{blue, 2})
	redCodes := receiveTestCodes(t, db, red, lines[red], "in_transit")
	blueCodes := receiveTestCodes(t, db, blue, lines[blue], "in_transit")
	wrong := receiveTestCodes(t, db, blue, other[blue], "in_transit")[0]
	items := []WarehouseReceiveItem{
		{VariantID: red, ReceivedQty: 1, QCStatus: "pass", UnitCodes: redCodes},
		{VariantID: blue, ReceivedQty: 2, QCStatus: "fail", UnitCodes: []string{wrong, redCodes[0]}},
	}
	repo := NewSupplierRepository(db)
	_, _, err := repo.ReceiveWarehouseShipmentWithUnits(context.Background(), shipment, items)
	assertReceiveMismatch(t, err, lines[blue], wrong, "wrong_lot")
	assertReceiveMismatch(t, err, lines[blue], redCodes[0], "duplicate")
	assertReceiveLot(t, db, lines[red], 0, 1, 0)
	assertReceiveLot(t, db, lines[blue], 0, 2, 0)
	assertReceiveLot(t, db, other[blue], 0, 1, 0)
	items[1].UnitCodes = blueCodes
	status, got, err := repo.ReceiveWarehouseShipmentWithUnits(context.Background(), shipment, items)
	if err != nil || status != "received" || len(got) != 2 {
		t.Fatalf("status=%q receipts=%+v err=%v", status, got, err)
	}
	assertReceiveResult(t, got[0], nil, lines[red], 1, redCodes)
	assertReceiveResult(t, got[1], nil, lines[blue], 2, blueCodes)
}

func TestSPNCXReceiveVariantlessMintsOnlyAtQC(t *testing.T) {
	db, _, _ := inv04CanonicalFixture(t)
	ctx := context.Background()
	const product, shipment = "spncx-legacy", "INB-spncx-legacy"
	variant := uuid.NewString()
	opsExec(t, db, `INSERT INTO products(id,title,slug,supplier_id,original_price,supplier_base_cost,platform_margin)
		VALUES($1,'test product',$1,'s',2000,1000,0)`, product)
	opsExec(t, db, `INSERT INTO product_variants(id,product_id,spnc,is_default,inventory_mode) VALUES($1,$2,'B3LEGACY',TRUE,'legacy')`, variant, product)
	opsExec(t, db, `INSERT INTO inbound_shipments(id,status,warehouse_id,supplier_id,supplier_name,items_count,cartons_count,items)
		VALUES($1,'in_transit','w1','s','Fixture',2,1,'[]')`, shipment)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := reserveInboundTransferTx(ctx, tx, shipment, "s", "w1", []InboundTransferLine{{ProductID: product, SourceKind: "external_replenishment", ExternalReference: shipment, Reason: "test replenishment", Quantity: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatchInboundTransferTx(ctx, tx, shipment); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var line string
	if err := db.QueryRow(`SELECT id FROM inbound_inventory_lines WHERE shipment_id=$1`, shipment).Scan(&line); err != nil {
		t.Fatal(err)
	}
	repo := NewSupplierRepository(db)
	_, err = repo.ReceiveInboundLine(ctx, line, 1, []string{"B3LEGACY-X0001"}, "")
	assertReceiveMismatch(t, err, line, "B3LEGACY-X0001", "line has no variant")
	result, err := repo.ReceiveInboundLine(ctx, line, 2, nil, "")
	assertReceiveResult(t, result, err, line, 2, nil)
	assertReceiveLot(t, db, line, 2, 0, 2)
	if codes := receiveTestCodes(t, db, variant, "", ""); len(codes) != 0 {
		t.Fatalf("variant-less receipt minted %v", codes)
	}
	qc := NewWarehouseRepository(db)
	_, err = qc.RecordQCLinePassDelta(ctx, line, 1, []string{"B3LEGACY-X0001"}, "")
	assertReceiveMismatch(t, err, line, "B3LEGACY-X0001", "line has no variant")
	if err := qc.RecordQCPass(ctx, product, shipment, "", 1); err != nil {
		t.Fatal(err)
	}
	if err := qc.CompleteQC(ctx, product, shipment, "", 1, 1, 0, "failed"); err != nil {
		t.Fatal(err)
	}
	if codes := receiveTestCodes(t, db, variant, "", "qc_accepted"); !slices.Equal(codes, []string{"B3LEGACY-X0001"}) {
		t.Fatalf("legacy accepted codes=%v", codes)
	}
	if codes := receiveTestCodes(t, db, variant, line, "damaged"); !slices.Equal(codes, []string{"B3LEGACY-X0002"}) {
		t.Fatalf("legacy damaged codes=%v", codes)
	}
	var proven bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots l
		JOIN inventory_movements m ON m.id=l.receipt_movement_id
		WHERE l.receipt_line_id=$1 AND l.variant_id=$2 AND l.initial_quantity=1 AND m.variant_id IS NULL)`, line, variant).Scan(&proven); err != nil || !proven {
		t.Fatalf("legacy QC storage provenance=%v err=%v", proven, err)
	}
}

func TestSPNCXReceiveCustomerReturn(t *testing.T) {
	db, f, warehouse, _, sku := inv06Placed(t, true, 2)
	ctx := context.Background()
	repo := NewOrderRepository(db)
	type returnFixture struct {
		id, line, orderItem, code string
	}
	makeReturn := func() returnFixture {
		order := inv06PaidOrder(t, db, f, warehouse, sku, 1)
		seedVerifiedRefundFixturePayment(t, db, order)
		opsExec(t, db, `UPDATE orders SET status='packed',fulfillment_method='warehouse' WHERE id=$1`, order)
		if err := NewWarehouseRepository(db).ShipOrderWithActor(ctx, order, "post", "original-"+order, f.user); err != nil {
			t.Fatal(err)
		}
		opsExec(t, db, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW(),return_window_ends_at=NOW()+INTERVAL '7 days' WHERE id=$1`, order)
		var item, code string
		if err := db.QueryRow(`SELECT order_item_id::text,code FROM order_item_units WHERE order_id=$1`, order).Scan(&item, &code); err != nil {
			t.Fatal(err)
		}
		ret, err := repo.CreateCustomerReturn(ctx, f.user, order, "test return", []CustomerReturnItemRequest{{OrderItemID: item, Qty: 1}}, CustomerReturnOptions{Prepaid: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.ApproveCustomerReturn(ctx, ret.ID, "test receipt"); err != nil {
			t.Fatal(err)
		}
		return returnFixture{ret.ID, ret.Items[0].ID, item, code}
	}
	first, second := makeReturn(), makeReturn()
	submit := func(ret returnFixture, qty int, codes []string) ([]UnitReceiptResult, error) {
		return repo.ReceiveCustomerReturnUnits(ctx, ret.id, []CustomerReturnUnitItem{{OrderItemID: ret.orderItem, Qty: qty, UnitCodes: codes}}, f.user)
	}
	_, err := submit(first, 2, []string{first.code})
	if !errors.Is(err, ErrIncompleteReturnReceipt) {
		t.Fatalf("whole quantity validation: %v", err)
	}
	_, err = submit(first, 1, []string{second.code})
	assertReceiveMismatch(t, err, first.line, second.code, "not_captured_for_item")
	_, err = submit(first, 1, []string{"MISSING-X0001"})
	assertReceiveMismatch(t, err, first.line, "MISSING-X0001", "not_found")

	checkState := func(ret returnFixture, wantReturn, wantUnit string, received bool) {
		t.Helper()
		var retState, unitState string
		var ok bool
		if err := db.QueryRow(`SELECT cr.status, u.status, (ri.received_at IS NOT NULL AND ri.qc_status='ok')
			FROM customer_returns cr JOIN return_items ri ON ri.return_id=cr.id
			JOIN product_units u ON u.code=$3 WHERE cr.id=$1 AND ri.id=$2`, ret.id, ret.line, ret.code).Scan(&retState, &unitState, &ok); err != nil {
			t.Fatal(err)
		}
		if retState != wantReturn || unitState != wantUnit || ok != received {
			t.Fatalf("return=%s unit=%s received/ok=%v, want %s/%s/%v", retState, unitState, ok, wantReturn, wantUnit, received)
		}
	}
	checkState(first, "approved", "sold", false)
	checkState(second, "approved", "sold", false)
	for i, ret := range []returnFixture{first, second} {
		var codes []string
		if i == 0 {
			codes = []string{ret.code}
		}
		got, err := submit(ret, 1, codes)
		if err != nil || len(got) != 1 {
			t.Fatalf("return receipt=%+v err=%v", got, err)
		}
		assertReceiveResult(t, got[0], nil, ret.line, 1, []string{ret.code})
		checkState(ret, "received", "in_stock", true)
		var holding, restored, guarded, proven bool
		if err := db.QueryRow(`SELECT
			EXISTS(SELECT 1 FROM product_unit_events WHERE code=$1 AND reference_id=$2 AND from_status='sold' AND to_status='returned'),
			EXISTS(SELECT 1 FROM product_unit_events WHERE code=$1 AND reference_id=$3 AND from_status='returned' AND to_status='in_stock'),
			EXISTS(SELECT 1 FROM order_inventory_returns WHERE return_item_id=$3),
			EXISTS(SELECT 1 FROM warehouse_customer_return_bin_sources s JOIN product_units u ON u.balance_id=s.source_balance_id
				WHERE s.return_item_id::text=$3 AND s.source_proven AND u.code=$1)`, ret.code, ret.id, ret.line).Scan(&holding, &restored, &guarded, &proven); err != nil || !holding || !restored || !guarded || !proven {
			t.Fatalf("holding event=%v restored event=%v guarded=%v source proven=%v err=%v", holding, restored, guarded, proven, err)
		}
	}
}
