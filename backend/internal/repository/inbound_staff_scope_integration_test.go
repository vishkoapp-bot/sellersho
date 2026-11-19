package repository

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestQUAL01InboundCityScope(t *testing.T) {
	_, schema, _, _ := skuInventoryFixture(t)
	db := skuScopedDB(t, schema)
	const staff = "00000000-0000-4000-8000-000000000901"
	const unconfirmed = "00000000-0000-4000-8000-000000000903"
	const inactive = "00000000-0000-4000-8000-000000000904"
	ctx := context.Background()
	scoped := WithWarehouseStaffID(ctx, staff)
	for _, q := range []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS warehouse_city_confirmed BOOLEAN NOT NULL DEFAULT FALSE`,
		`INSERT INTO users(id,mobile,role,warehouse_city,is_active,warehouse_city_confirmed) VALUES
		 ('00000000-0000-4000-8000-000000000901','09129999991','warehouse_staff','Tehran',TRUE,TRUE),
		 ('00000000-0000-4000-8000-000000000902','09129999992','warehouse_staff',' ',TRUE,TRUE),
		 ('00000000-0000-4000-8000-000000000903','09129999993','warehouse_staff','Tehran',TRUE,FALSE),
		 ('00000000-0000-4000-8000-000000000904','09129999994','warehouse_staff','Tehran',FALSE,TRUE)`,
		`UPDATE warehouses SET city_name='Tehran' WHERE id IN ('w1','w2')`,
		`INSERT INTO warehouses(id,city_name) VALUES('w3','Qom')`,
		`CREATE TABLE inbound_receiving_logs(id TEXT PRIMARY KEY,inbound_id TEXT,staff_user_id TEXT,action TEXT,scanned_item JSONB,created_at TIMESTAMPTZ)`,
		`INSERT INTO products(id,supplier_id,stock,schema_version) VALUES('legacy','s',0,0)`,
		`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled,supplier_warehouse_id) VALUES('asn-source','legacy','s','supplier','supplier','supplier-warehouse:sw1','sellable',3,TRUE,'sw1')`,
		`INSERT INTO inbound_shipments(id,status,warehouse_id,supplier_id,supplier_name,warehouse_name,items_count,cartons_count,carrier,truck_plate,items,created_at) VALUES
		 ('INB-a','in_transit','w1','s','Supplier','Qom',1,1,'carrier','plate','[{"productId":"legacy","quantity":1}]',NOW()),
		 ('INB-b','in_transit','w2','s','Supplier','Qom',1,1,'carrier','plate','[{"productId":"legacy","quantity":1}]',NOW()-INTERVAL '1 second'),
		 ('INB-c','in_transit','w3','s','Supplier','Tehran',1,1,'carrier','plate','[{"productId":"legacy","quantity":1}]',NOW()-INTERVAL '2 seconds'),
		 ('INB-unresolved','in_transit',NULL,'s','Supplier','Tehran',1,1,'carrier','plate','[]',NOW()-INTERVAL '3 seconds')`,
	} {
		opsExec(t, db, q)
	}
	for i, id := range []string{"INB-a", "INB-b", "INB-c"} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := lockInventoryParentsTx(ctx, tx, []inventoryCaptureItem{{ProductID: "legacy"}}); err != nil {
			t.Fatal(err)
		}
		if err := reserveInboundTransferTx(ctx, tx, id, "s", []string{"w1", "w2", "w3"}[i], []InboundTransferLine{{ProductID: "legacy", Quantity: 1, SourceKind: "transfer", Source: InventoryOrigin{Type: "supplier", SupplierID: "s", SupplierWarehouseID: "sw1"}}}); err != nil {
			t.Fatal(err)
		}
		if err := dispatchInboundTransferTx(ctx, tx, id); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	warehouse, wms := NewWarehouseRepository(db), NewWMSRepository(db)
	for _, tc := range []struct {
		ctx context.Context
		ids []string
	}{
		{scoped, []string{"INB-a", "INB-b"}},
		{WithWarehouseStaffID(ctx, "00000000-0000-4000-8000-000000000902"), nil},
		{WithWarehouseStaffID(ctx, unconfirmed), nil},
		{WithWarehouseStaffID(ctx, inactive), nil},
		{ctx, []string{"INB-a", "INB-b", "INB-c", "INB-unresolved"}},
	} {
		pending, err := warehouse.GetPendingInbounds(tc.ctx)
		if err != nil {
			t.Fatal(err)
		}
		all, err := wms.GetInboundShipments(tc.ctx)
		if err != nil {
			t.Fatal(err)
		}
		var pendingIDs, allIDs []string
		for _, row := range pending {
			pendingIDs = append(pendingIDs, row.ID)
		}
		for _, row := range all {
			allIDs = append(allIDs, row.ID)
		}
		if !reflect.DeepEqual(pendingIDs, tc.ids) || !reflect.DeepEqual(allIDs, tc.ids) {
			t.Fatalf("pending=%v, WMS=%v, want %v", pendingIDs, allIDs, tc.ids)
		}
	}
	scan := map[string]interface{}{"productId": "legacy", "quantity": float64(1), "warehouseId": "w1"}
	for _, denied := range []struct{ actor, id string }{
		{staff, "INB-c"}, {staff, "INB-unresolved"}, {staff, "INB-missing"},
		{unconfirmed, "INB-a"}, {inactive, "INB-a"},
	} {
		actor, id := denied.actor, denied.id
		deniedCtx := WithWarehouseStaffID(ctx, actor)
		if err := warehouse.StartReceiving(deniedCtx, id, actor); !errors.Is(err, ErrInboundOutsideStaffScope) {
			t.Fatalf("start %s: %v", id, err)
		}
		if _, err := warehouse.ScanItem(deniedCtx, id, scan, actor); !errors.Is(err, ErrInboundOutsideStaffScope) {
			t.Fatalf("scan %s: %v", id, err)
		}
		if err := warehouse.CompleteInbound(deniedCtx, id, actor); !errors.Is(err, ErrInboundOutsideStaffScope) {
			t.Fatalf("complete %s: %v", id, err)
		}
	}
	var untouched bool
	if err := db.QueryRow(`SELECT status='in_transit' AND started_receiving_at IS NULL AND completed_at IS NULL AND COALESCE(scanned_items,'[]')='[]' AND NOT EXISTS(SELECT 1 FROM inbound_receiving_logs) AND NOT EXISTS(SELECT 1 FROM inbound_inventory_lines WHERE received_quantity>0) FROM inbound_shipments WHERE id='INB-c'`).Scan(&untouched); err != nil || !untouched {
		t.Fatalf("denied workflow changed state: untouched=%v err=%v", untouched, err)
	}
	for _, id := range []string{"INB-a", "INB-b"} {
		if err := warehouse.StartReceiving(scoped, id, staff); err != nil {
			t.Fatal(err)
		}
		if n, err := warehouse.ScanItem(scoped, id, scan, staff); err != nil || n != 1 {
			t.Fatalf("scan %s: %d, %v", id, n, err)
		}
		if err := warehouse.CompleteInbound(scoped, id, staff); err != nil {
			t.Fatal(err)
		}
		var received bool
		if err := db.QueryRow(`SELECT status='received' AND completed_at IS NOT NULL AND (SELECT SUM(received_quantity) FROM inbound_inventory_lines WHERE shipment_id=$1)=1 FROM inbound_shipments WHERE id=$1`, id).Scan(&received); err != nil || !received {
			t.Fatalf("complete %s: received=%v, err=%v", id, received, err)
		}
	}
}
