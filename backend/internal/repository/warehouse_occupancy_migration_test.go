package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The canonical migrated fixture already contains retained legacy exceptions;
// never delete their evidence merely to downgrade a test. Rehearse the upgrade
// in an owned schema with the exact pre-upgrade columns used by this migration.
func TestINV06MigrationPreservesAndFlagsHistoricalCounters(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer conn.ExecContext(ctx, `RESET search_path`)
	var originalExceptions int
	if err = db.QueryRow(`SELECT COUNT(*) FROM warehouse_occupancy_exceptions`).Scan(&originalExceptions); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/202611150001_warehouse_occupancy.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611150001_warehouse_occupancy.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := `
 CREATE TABLE warehouse_bins(bin_code TEXT PRIMARY KEY,warehouse_id TEXT,capacity_status TEXT,quantity INT,occupied_volume_cm3 BIGINT,occupied_weight_grams BIGINT,updated_at TIMESTAMPTZ DEFAULT NOW());
 CREATE TABLE inventory_measurement_revisions(id TEXT PRIMARY KEY,length_mm INT,width_mm INT,height_mm INT,weight_grams INT,verified BOOLEAN);
 CREATE TABLE warehouse_placements(id TEXT PRIMARY KEY,task_id TEXT,product_id TEXT,variant_id TEXT,supplier_id TEXT,warehouse_id TEXT,bin_code TEXT,quantity INT,volume_cm3 BIGINT,weight_grams BIGINT,measurement_revision_id TEXT,created_at TIMESTAMPTZ DEFAULT NOW());
 CREATE TABLE inventory_balances(id TEXT PRIMARY KEY,product_id TEXT,variant_id UUID,supplier_id TEXT,owner_type TEXT,warehouse_id TEXT,location_type TEXT,location_key TEXT,status TEXT,quantity INT);
 CREATE TABLE orders(id TEXT PRIMARY KEY,status TEXT,payment_status TEXT,shipped_at TIMESTAMPTZ);
 CREATE TABLE order_inventory_captures(order_id TEXT,order_item_id TEXT,product_id TEXT,variant_id UUID,quantity INT,restored_quantity INT DEFAULT 0,inventory_mode TEXT DEFAULT 'tracked',allocations JSONB);
 CREATE TABLE inventory_reservations(id TEXT,product_id TEXT,variant_id UUID,supplier_id TEXT,location_key TEXT,quantity INT,reference_type TEXT,reference_id TEXT,status TEXT);
 CREATE TABLE supplier_return_requests(id TEXT,status TEXT);
 `
	for _, seed := range []bool{false, true} {
		schema := "inv06_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err = conn.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, `SET search_path TO `+schema+`,pg_catalog`); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
		if seed {
			fixture := `
   INSERT INTO inventory_measurement_revisions VALUES('m1',100,100,100,100,TRUE);
   INSERT INTO warehouse_bins(bin_code,warehouse_id,capacity_status,quantity,occupied_volume_cm3,occupied_weight_grams) VALUES('departed','wh','verified',1,1000,100),('unknown','wh','verified',4,123,456),('paid-held','wh','verified',2,2000,200);
   INSERT INTO warehouse_placements(id,task_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,quantity,volume_cm3,weight_grams,measurement_revision_id) VALUES('p1','t1','prod1','10000000-0000-0000-0000-000000000001','sup','wh','departed',1,1000,100,'m1'),('p2','t2','prod2','10000000-0000-0000-0000-000000000002','sup','wh','paid-held',2,2000,200,'m1');
   INSERT INTO inventory_balances VALUES('b1','prod1','10000000-0000-0000-0000-000000000001','sup','supplier','wh','bin','bin:departed','sellable',0),('b2','prod2','10000000-0000-0000-0000-000000000002','sup','supplier','wh','bin','bin:paid-held','sellable',1);
   INSERT INTO orders VALUES('o1','shipped','paid',NOW()),('o2','pending','paid',NULL);
   INSERT INTO order_inventory_captures(order_id,order_item_id,product_id,variant_id,quantity,allocations) VALUES('o1','oi1','prod1','10000000-0000-0000-0000-000000000001',1,'[{"balanceId":"b1","quantity":1}]'),('o2','oi2','prod2','10000000-0000-0000-0000-000000000002',1,'[{"balanceId":"b2","quantity":1}]');
   `
			if _, err = conn.ExecContext(ctx, fixture); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = conn.ExecContext(ctx, string(up)); err != nil {
			t.Fatal(err)
		}
		if !seed {
			if _, err = conn.ExecContext(ctx, string(down)); err != nil {
				t.Fatal(err)
			}
			if _, err = conn.ExecContext(ctx, string(up)); err != nil {
				t.Fatal(err)
			}
			continue
		}
		for _, row := range []struct {
			bin, status string
			q, v, w     int
		}{{"departed", "disputed", 1, 1000, 100}, {"unknown", "disputed", 4, 123, 456}, {"paid-held", "verified", 2, 2000, 200}} {
			var status string
			var q, v, w int
			if err = conn.QueryRowContext(ctx, `SELECT capacity_status,quantity,occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code=$1`, row.bin).Scan(&status, &q, &v, &w); err != nil || status != row.status || q != row.q || v != row.v || w != row.w {
				t.Errorf("history bin=%s status=%s qty/volume/weight=%d/%d/%d err=%v", row.bin, status, q, v, w, err)
			}
		}
		var events int
		if err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM warehouse_departure_events`).Scan(&events); err != nil || events != 0 {
			t.Fatal("migration invented historical departures")
		}
		if _, err = conn.ExecContext(ctx, `UPDATE warehouse_bins SET capacity_status='verified' WHERE bin_code='unknown'`); err == nil {
			t.Fatal("capacity verification silently cleared unknown occupancy")
		}
		if _, err = conn.ExecContext(ctx, string(down)); err == nil {
			t.Fatal("downgrade discarded retained exceptions")
		}
	}
	var after int
	if err = db.QueryRow(`SELECT COUNT(*) FROM public.warehouse_occupancy_exceptions`).Scan(&after); err != nil || after != originalExceptions {
		t.Fatalf("upgrade rehearsal changed original fixture evidence=%d/%d err=%v", originalExceptions, after, err)
	}
}
