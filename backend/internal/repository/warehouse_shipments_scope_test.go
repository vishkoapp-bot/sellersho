package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

func TestQUAL01WarehouseShipmentSiteScope(t *testing.T) {
	db := openWarehouseCapacityIntegrationDB(t)
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx := context.Background()
	for _, stmt := range []string{
		`CREATE TEMP TABLE warehouses(id text PRIMARY KEY, city_name text)`,
		`CREATE TEMP TABLE users(id text PRIMARY KEY, warehouse_city text, warehouse_city_confirmed boolean, is_active boolean)`,
		`CREATE TEMP TABLE order_shipping_legs(id text, warehouse_id text, supplier_id text, supplier_warehouse_id text, origin_label text)`,
		`CREATE TEMP TABLE shipments(id text PRIMARY KEY, shipment_code text, warehouse_id text, supplier_id text, status text)`,
		`CREATE TEMP TABLE suppliers(id text PRIMARY KEY, name text)`,
		`CREATE TEMP TABLE products(id text PRIMARY KEY, title text)`,
		`CREATE TEMP TABLE product_variants(id text PRIMARY KEY, product_id text, variant_name text, spnc text)`,
		`CREATE TEMP TABLE shipment_items(shipment_id text, variant_id text, expected_qty int, received_qty int, inventory_received_qty int, qc_status text)`,
		`CREATE TEMP TABLE inbound_inventory_lines(
			id text PRIMARY KEY DEFAULT gen_random_uuid()::text,
			shipment_id text, warehouse_id text, supplier_id text, product_id text, variant_id text,
			quantity int, dispatched_quantity int, received_quantity int, cancelled_quantity int)`,
		`INSERT INTO users VALUES ('staff','City A',TRUE,TRUE), ('blank',' ',TRUE,TRUE), ('unassigned',NULL,TRUE,TRUE), ('unconfirmed','City A',FALSE,TRUE), ('inactive','City A',TRUE,FALSE)`,
		`INSERT INTO warehouses VALUES ('a1','City A'), ('a2','City A'), ('b1','City B')`,
		`INSERT INTO order_shipping_legs VALUES ('0-outside','b1',NULL,NULL,'City A'), ('1-a','a1',NULL,NULL,'Wrong label'), ('2-a','a2',NULL,NULL,'City A'), ('3-legacy',NULL,NULL,NULL,'City A'), ('4-supplier','a1','supplier',NULL,'City A'), ('5-supplier-site','a1',NULL,'supplier-site','City A'), ('6-orphan','missing',NULL,NULL,'City A')`,
		`INSERT INTO shipments VALUES ('a','A','a1','supplier','received'), ('a2','A2','a2','supplier','received'), ('b','B','b1','supplier','received'), ('legacy','LEGACY',NULL,'supplier','received'), ('orphan','ORPHAN','missing','supplier','received'), ('conflict','CONFLICT','a1','supplier','received')`,
		`INSERT INTO inbound_inventory_lines (shipment_id,warehouse_id) VALUES ('conflict','b1')`,
		`INSERT INTO suppliers VALUES ('supplier','Supplier')`,
		`INSERT INTO products VALUES ('p','Product')`,
		`INSERT INTO product_variants VALUES ('v','p','Variant','SPNC')`,
		`INSERT INTO shipment_items SELECT id,'v',1,1,1,'passed' FROM shipments`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	staff := WithWarehouseStaffID(ctx, "staff")
	legs, err := ListScopedWarehouseShippingLegs(staff, db, 2)
	if err != nil || len(legs) != 2 {
		t.Fatalf("scoped list = %s, %v", legs, err)
	}
	for i, want := range []string{"1-a", "2-a"} {
		var row struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(legs[i], &row); err != nil || row.ID != want {
			t.Fatalf("leg %d: %s, %v; want %s", i, legs[i], err, want)
		}
	}
	if legs, err := ListScopedWarehouseShippingLegs(staff, db, 200); err != nil || len(legs) != 2 {
		t.Fatalf("unbounded scoped list = %s, %v", legs, err)
	}
	if legs, err := ListScopedWarehouseShippingLegs(WithWarehouseStaffID(ctx, "inactive"), db, 200); err != nil || len(legs) != 0 {
		t.Fatalf("inactive staff list = %s, %v", legs, err)
	}

	repo := NewSupplierRepository(db)
	items := []WarehouseReceiveItem{{VariantID: "v", ReceivedQty: 1, QCStatus: "pass"}}
	for _, tc := range []struct {
		actor, code string
		allowed     bool
	}{
		{"staff", "a", true}, {"staff", "a2", true}, {"staff", "b", false},
		{"staff", "legacy", false}, {"staff", "orphan", false}, {"staff", "conflict", false},
		{"missing", "a", false}, {"blank", "a", false}, {"unassigned", "a", false}, {"unconfirmed", "a", false},
		{"inactive", "a", false},
		{"", "b", true}, {"", "legacy", true},
	} {
		t.Run(tc.actor+"/"+tc.code, func(t *testing.T) {
			actor := WithWarehouseStaffID(ctx, tc.actor)
			detail, readErr := repo.GetWarehouseShipmentByCode(actor, tc.code)
			status, receiveErr := repo.ReceiveWarehouseShipment(actor, tc.code, items)
			if tc.allowed {
				if readErr != nil || len(detail.Items) != 1 || receiveErr != nil || status != "received" {
					t.Fatalf("allowed detail=%+v (%v), receipt=%q (%v)", detail, readErr, status, receiveErr)
				}
			} else if !errors.Is(readErr, sql.ErrNoRows) || !errors.Is(receiveErr, sql.ErrNoRows) {
				t.Fatalf("denied detail error=%v, receipt error=%v", readErr, receiveErr)
			}
		})
	}
	// The final guard must not trust either a previously resolved city or a
	// carton whose immutable receipt provenance points at a different site.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var status string
	if err := lockScopedCartonReceipt(staff, tx, "a", "staff", "a", &status); err != nil {
		t.Fatalf("final guard rejects authorized site: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO inbound_inventory_lines (shipment_id,warehouse_id) VALUES ('a','b1')`); err != nil {
		t.Fatal(err)
	}
	if err := lockScopedCartonReceipt(staff, tx, "a", "staff", "a", &status); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("final guard accepts mismatched destination: %v", err)
	}
	if _, err := tx.Exec(`UPDATE users SET warehouse_city='City B' WHERE id='staff'`); err != nil {
		t.Fatal(err)
	}
	if err := lockScopedCartonReceipt(staff, tx, "a2", "staff", "a2", &status); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("final guard accepts former city: %v", err)
	}
}
