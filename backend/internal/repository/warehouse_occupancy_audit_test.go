package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestINV06ReadOnlyAuditPagesWholeWarehouse(t *testing.T) {
	db := operationsDB(t)
	// TEST-02 round 5: the warehouse_occupancy_audit view is
	// CASCADE-dropped by any earlier fixture that DROP TABLEs one of
	// its base tables (order_inventory_captures, inventory_balances,
	// warehouse_placements, ...). Re-create it (and its dependencies)
	// before running the audit scripts, or this test fails with 42P01
	// in the full run.
	createWarehouseOccupancyAuditViews(t, db)
	warehouse := "audit-" + uuid.NewString()
	prefix := "A6-" + uuid.NewString()[:8] + "-"
	opsExec(t, db, `INSERT INTO warehouses(id,name,tapin_province_id,tapin_city_id,province_name,city_name) VALUES($1,$1,1,1,'p','c')`, warehouse)
	opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,warehouse_id,quantity,occupied_volume_cm3,occupied_weight_grams,capacity_status) SELECT $1||lpad(g::text,4,'0'),'audit','1','1',$2,0,0,0,'unverified' FROM generate_series(1,501) g`, prefix, warehouse)
	report, err := os.ReadFile("../../../scripts/audit-warehouse-occupancy.sql")
	if err != nil {
		t.Fatal(err)
	}
	orphans, err := os.ReadFile("../../../scripts/audit-warehouse-occupancy-orphans.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT set_config('sellersho.audit_warehouse_id',$1,TRUE)`, warehouse); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 2; page++ {
		if _, err = tx.Exec(`SELECT set_config('sellersho.audit_after_bin_code',$1,TRUE)`, cursor); err != nil {
			t.Fatal(err)
		}
		rows, err := tx.Query(string(report))
		if err != nil {
			t.Fatal(err)
		}
		size := 0
		more := false
		for rows.Next() {
			var bin, wh, status, next string
			var evidence []byte
			var requires bool
			if err = rows.Scan(&bin, &wh, &status, &evidence, &requires, &next, &more); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if seen[bin] || bin <= cursor || wh != warehouse {
				rows.Close()
				t.Fatal("audit repeated/regressed/mixed warehouse identities")
			}
			if requires || next != "capacity_verification_pending" {
				rows.Close()
				t.Fatal("empty unverified bin presented as a verified physical count")
			}
			seen[bin] = true
			cursor = bin
			size++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if page == 0 && (size != 500 || !more) {
			t.Fatalf("page1 size=%d more=%t", size, more)
		}
		if page == 1 && (size != 1 || more) {
			t.Fatalf("page2 size=%d more=%t", size, more)
		}
	}
	if len(seen) != 501 {
		t.Fatalf("audit covered %d/501 bins", len(seen))
	}
	rows, err := tx.Query(string(orphans))
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key, reason string
		var raw []byte
		var more bool
		if err = rows.Scan(&key, &reason, &raw, &more); err != nil {
			rows.Close()
			t.Fatal(err)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	var quantity, volume, weight int
	if err = tx.QueryRow(`SELECT SUM(quantity),SUM(occupied_volume_cm3),SUM(occupied_weight_grams) FROM warehouse_bins WHERE warehouse_id=$1`, warehouse).Scan(&quantity, &volume, &weight); err != nil || quantity != 0 || volume != 0 || weight != 0 {
		t.Fatal("read-only reconciliation changed historical counters")
	}
}
