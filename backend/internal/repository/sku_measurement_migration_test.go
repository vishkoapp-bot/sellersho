package repository

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestINV08MigrationPreservesHistoricalCapacity(t *testing.T) {
	db := operationsDB(t)
	ctx := context.Background()
	// Later migrations depend on placements and preserve legacy exceptions. An
	// earlier upgrade rehearsal must not drop that live fixture evidence.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer conn.ExecContext(ctx, `RESET search_path`)
	schema := "inv08_upgrade_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = conn.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SET search_path TO `+schema+`,pg_catalog`); err != nil {
		t.Fatal(err)
	}
	ddl := `
 CREATE TABLE products(id TEXT PRIMARY KEY,length_mm INT,width_mm INT,height_mm INT,weight_grams INT,measurement_status TEXT,measurement_source TEXT,measurement_version INT DEFAULT 0);
 CREATE TABLE product_variants(id UUID PRIMARY KEY,product_id TEXT,length_mm INT,width_mm INT,height_mm INT,weight_grams INT);
 CREATE TABLE putaway_tasks(id TEXT PRIMARY KEY,product_id TEXT,variant_id UUID,warehouse_id TEXT,bin_location TEXT,quantity INT,status TEXT);
 CREATE TABLE inbound_putaway_allocations(task_id TEXT,product_id TEXT,variant_id TEXT,supplier_id TEXT,warehouse_id TEXT,quantity INT);
 CREATE TABLE putaway_capacity_reservations(id TEXT PRIMARY KEY,task_id TEXT,bin_code TEXT,volume_cm3 BIGINT,weight_grams BIGINT,status TEXT);
 CREATE TABLE warehouse_bins(bin_code TEXT PRIMARY KEY,quantity INT,occupied_volume_cm3 BIGINT,occupied_weight_grams BIGINT);
 `
	if _, err = conn.ExecContext(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/202611140010_sku_measurement_revision.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/202611140010_sku_measurement_revision.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	sku := uuid.NewString()
	if _, err = conn.ExecContext(ctx, `INSERT INTO products(id,length_mm,width_mm,height_mm,weight_grams,measurement_status) VALUES('old',10,10,10,10,'verified'); INSERT INTO warehouse_bins VALUES('old-bin',5,123,456); INSERT INTO putaway_tasks(id,product_id,quantity,status,bin_location) VALUES('old-task','old',5,'completed','old-bin')`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO product_variants(id,product_id,length_mm,width_mm,height_mm,weight_grams) VALUES($1,'old',100,100,100,1000)`, sku); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var volume, weight, count int
	var parentVerified, skuVerified bool
	if err = conn.QueryRowContext(ctx, `SELECT occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code='old-bin'`).Scan(&volume, &weight); err != nil || volume != 123 || weight != 456 {
		t.Fatalf("migration rewrote occupancy=%d/%d err=%v", volume, weight, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT verified FROM inventory_effective_measurements WHERE product_id='old' AND variant_id=''`).Scan(&parentVerified); err != nil || !parentVerified {
		t.Fatalf("verified parent lost=%t err=%v", parentVerified, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT verified FROM inventory_effective_measurements WHERE product_id='old' AND variant_id=$1`, sku).Scan(&skuVerified); err != nil || skuVerified {
		t.Fatalf("historical declaration promoted=%t err=%v", skuVerified, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM warehouse_placements`).Scan(&count); err != nil || count != 0 {
		t.Fatal("historical placement invented")
	}
	if err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM warehouse_measurement_reconciliation WHERE task_id='old-task'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("historical unresolved placement hidden")
	}
	if _, err = conn.ExecContext(ctx, string(down)); err == nil {
		t.Fatal("destructive downgrade discarded persisted revision")
	}
}
