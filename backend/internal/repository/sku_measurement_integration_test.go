package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/drup/platform/internal/warehouseopt"
	"github.com/google/uuid"
)

func inv08Task(t *testing.T, quantity int) (*sql.DB, opsFixture, string, string, string) {
	t.Helper()
	db, f, warehouse, shipment, red, _ := inv09ReceiveTracked(t, quantity)
	repo := NewSupplierRepository(db)
	ctx := context.Background()
	if err := repo.RecordProductMeasurement(ctx, f.product, f.user, 10, 10, 10, 10, "measured parent"); err != nil {
		t.Fatal(err)
	}
	l, g := 100, 1000
	if _, err := repo.UpdateVariantPhysical(ctx, f.supplier, f.product, red, &g, &l, &l, &l); err != nil {
		t.Fatal(err)
	}
	if err := NewWarehouseRepository(db).RecordQCPass(ctx, f.product, shipment, f.user, quantity); err != nil {
		t.Fatal(err)
	}
	var task string
	if err := db.QueryRow(`SELECT id FROM putaway_tasks WHERE inbound_id=$1`, shipment).Scan(&task); err != nil {
		t.Fatal(err)
	}
	return db, f, warehouse, task, red
}

func inv08Bin(t *testing.T, db *sql.DB, warehouse string, volume, weight int) string {
	t.Helper()
	bin := "BM-" + uuid.NewString()[:12]
	opsExec(t, db, `INSERT INTO warehouse_bins(bin_code,hall_name,aisle,shelf,quantity,warehouse_id,warehouse_name,capacity_volume_cm3,max_weight_grams,capacity_status,position_x,position_y,position_z) VALUES($1,'test','1','1',0,$2,$3,$4,$5,'verified',0,0,0)`, bin, warehouse, warehouse, volume, weight)
	return bin
}

func TestINV08DeclaredSKUOverrideCannotUseParentVerification(t *testing.T) {
	db, f, warehouse, task, _ := inv08Task(t, 1)
	wh := NewWarehouseRepository(db)
	ctx := context.Background()
	small, large := inv08Bin(t, db, warehouse, 10, 50), inv08Bin(t, db, warehouse, 1000, 1000)
	t.Run("suggestions", func(t *testing.T) {
		suggestions, err := wh.SuggestBins(ctx, task, warehouseopt.Point{})
		if err == nil && len(suggestions) > 0 {
			t.Fatalf("unverified SKU override inherited parent's verification: %d suggestions", len(suggestions))
		}
	})
	for _, bin := range []string{small, large} {
		t.Run(bin, func(t *testing.T) {
			if err := wh.AssignPutawayTask(ctx, task, bin, f.user); err == nil {
				t.Fatal("unverified SKU override reserved capacity using parent measurements")
			}
		})
	}
}

func TestINV08VerifiedSKUCapacityAndImmutableCompletedPlacement(t *testing.T) {
	db, f, warehouse, task, sku := inv08Task(t, 1)
	ctx := context.Background()
	sup, wh := NewSupplierRepository(db), NewWarehouseRepository(db)
	physical, err := sup.RecordSKUPhysicalMeasurement(ctx, f.product, sku, f.user, 100, 100, 100, 1000, "measured this SKU")
	if err != nil {
		t.Fatal(err)
	}
	small, large := inv08Bin(t, db, warehouse, 10, 50), inv08Bin(t, db, warehouse, 1000, 1000)
	suggestions, err := wh.SuggestBins(ctx, task, warehouseopt.Point{})
	if err != nil || len(suggestions) != 1 || suggestions[0].BinCode != large {
		t.Fatalf("SKU suggestions=%+v err=%v", suggestions, err)
	}
	if err = wh.AssignPutawayTask(ctx, task, small, f.user); err == nil {
		t.Fatal("small bin accepted verified large SKU")
	}
	if err = wh.AssignPutawayTask(ctx, task, large, f.user); err != nil {
		t.Fatal(err)
	}
	var volume, weight int64
	var revision string
	if err = db.QueryRow(`SELECT volume_cm3,weight_grams,measurement_revision_id FROM putaway_capacity_reservations WHERE task_id=$1`, task).Scan(&volume, &weight, &revision); err != nil || volume != 1000 || weight != 1000 || revision != physical.Revision {
		t.Fatalf("reservation=%d/%d %s err=%v", volume, weight, revision, err)
	}
	// Confirmation must recheck the physical capacity, including reductions made after assignment.
	opsExec(t, db, `UPDATE warehouse_bins SET capacity_volume_cm3=10 WHERE bin_code=$1`, large)
	if err = wh.ConfirmPutaway(ctx, task, f.user); err == nil {
		t.Fatal("confirmation ignored reduced capacity")
	}
	opsExec(t, db, `UPDATE warehouse_bins SET capacity_volume_cm3=1000 WHERE bin_code=$1`, large)
	for i := 0; i < 2; i++ {
		if err = wh.ConfirmPutaway(ctx, task, f.user); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = sup.RecordSKUPhysicalMeasurement(ctx, f.product, sku, f.user, 200, 200, 200, 2000, "remeasured after placement"); err != nil {
		t.Fatal(err)
	}
	if err = wh.ConfirmPutaway(ctx, task, f.user); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*),MAX(volume_cm3),MAX(weight_grams),MAX(measurement_revision_id) FROM warehouse_placements WHERE task_id=$1`, task).Scan(&count, &volume, &weight, &revision); err != nil || count != 1 || volume != 1000 || weight != 1000 || revision != physical.Revision {
		t.Fatalf("historical placement changed count=%d volume=%d weight=%d revision=%s err=%v", count, volume, weight, revision, err)
	}
	if err = db.QueryRow(`SELECT occupied_volume_cm3,occupied_weight_grams FROM warehouse_bins WHERE bin_code=$1`, large).Scan(&volume, &weight); err != nil || volume != 1000 || weight != 1000 {
		t.Fatalf("historical occupancy=%d/%d err=%v", volume, weight, err)
	}
	for _, q := range []string{`UPDATE inventory_measurement_revisions SET weight_grams=1 WHERE id=$1`, `DELETE FROM inventory_measurement_revisions WHERE id=$1`} {
		if _, err = db.Exec(q, physical.Revision); err == nil {
			t.Fatal("measurement history mutated")
		}
	}
	if _, err = db.Exec(`UPDATE warehouse_placements SET volume_cm3=1 WHERE task_id=$1`, task); err == nil {
		t.Fatal("placement history mutated")
	}
}

func TestINV08MeasurementChangeInvalidatesOnlyPendingScope(t *testing.T) {
	db, f, warehouse, task, sku := inv08Task(t, 1)
	ctx := context.Background()
	sup, wh := NewSupplierRepository(db), NewWarehouseRepository(db)
	verify := func(l int) SKUPhysical {
		t.Helper()
		p, e := sup.RecordSKUPhysicalMeasurement(ctx, f.product, sku, f.user, l, l, l, 1000, "counted SKU")
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	first := verify(100)
	bin := inv08Bin(t, db, warehouse, 100000, 100000)
	if err := wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	// A parent change cannot invalidate an independently verified override.
	if err := sup.RecordProductMeasurement(ctx, f.product, f.user, 20, 20, 20, 20, "changed parent"); err != nil {
		t.Fatal(err)
	}
	var status, ref string
	if err := db.QueryRow(`SELECT status,measurement_revision_id FROM putaway_capacity_reservations WHERE task_id=$1`, task).Scan(&status, &ref); err != nil || status != "active" || ref != first.Revision {
		t.Fatalf("independent SKU invalidated status=%s revision=%s err=%v", status, ref, err)
	}
	second := verify(100) // Same dimensions still create a new auditable verification.
	if first.Revision == second.Revision {
		t.Fatal("new measurement reused immutable identity")
	}
	if err := db.QueryRow(`SELECT status FROM putaway_tasks WHERE id=$1`, task).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("task not reset: %s %v", status, err)
	}
	if err := wh.ConfirmPutaway(ctx, task, f.user); err == nil {
		t.Fatal("stale measurement confirmed")
	}
	if err := wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	l, g := 101, 1001
	if _, err := sup.UpdateVariantPhysical(ctx, f.supplier, f.product, sku, &g, &l, &l, &l); err != nil {
		t.Fatal(err)
	}
	if err := wh.AssignPutawayTask(ctx, task, bin, f.user); err == nil {
		t.Fatal("supplier edit retained warehouse verification")
	}
	if err := wh.ConfirmPutaway(ctx, task, f.user); err == nil {
		t.Fatal("supplier edit retained active reservation")
	}
	var verified bool
	if err := db.QueryRow(`SELECT verified FROM inventory_effective_measurements WHERE product_id=$1 AND variant_id=$2`, f.product, sku).Scan(&verified); err != nil || verified {
		t.Fatalf("declaration verified=%t err=%v", verified, err)
	}
}

func TestINV08ParentFallbackAndAggregateRounding(t *testing.T) {
	db, f, warehouse, task, sku := inv08Task(t, 1001)
	ctx := context.Background()
	sup, wh := NewSupplierRepository(db), NewWarehouseRepository(db)
	if _, err := sup.UpdateVariantPhysical(ctx, f.supplier, f.product, sku, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	parent, err := sup.RecordSKUPhysicalMeasurement(ctx, f.product, "", f.user, 1, 1, 1, 1, "tiny units measured")
	if err != nil {
		t.Fatal(err)
	}
	bin := inv08Bin(t, db, warehouse, 2, 1001)
	if err = wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	var volume, weight int64
	var ref string
	if err = db.QueryRow(`SELECT volume_cm3,weight_grams,measurement_revision_id FROM putaway_capacity_reservations WHERE task_id=$1`, task).Scan(&volume, &weight, &ref); err != nil || volume != 2 || weight != 1001 || ref != parent.Revision {
		t.Fatalf("aggregate rounding volume=%d weight=%d ref=%s err=%v", volume, weight, ref, err)
	}
	if err = sup.RecordProductMeasurement(ctx, f.product, f.user, 2, 2, 2, 2, "fallback revision changed"); err != nil {
		t.Fatal(err)
	}
	if err = wh.ConfirmPutaway(ctx, task, f.user); err == nil {
		t.Fatal("changed parent fallback kept stale reservation")
	}
	if err = wh.AssignPutawayTask(ctx, task, bin, f.user); err == nil {
		t.Fatal("changed parent dimensions ignored")
	}
	if _, err = sup.RecordSKUPhysicalMeasurement(ctx, f.product, uuid.NewString(), f.user, 1, 1, 1, 1, "foreign SKU"); err == nil {
		t.Fatal("unknown SKU inherited product")
	}
	for _, dims := range [][4]int{{1000001, 1, 1, 1}, {1, 1, 1, 1000000001}, {0, 1, 1, 1}, {-1, 1, 1, 1}} {
		if _, err = sup.RecordSKUPhysicalMeasurement(ctx, f.product, sku, f.user, dims[0], dims[1], dims[2], dims[3], "invalid"); err == nil {
			t.Fatalf("out of bounds dimensions accepted: %v", dims)
		}
	}
}

func TestINV08PlacementInsertRejectsForgedOrStaleEvidence(t *testing.T) {
	db, f, warehouse, task, sku := inv08Task(t, 1)
	ctx := context.Background()
	sup, wh := NewSupplierRepository(db), NewWarehouseRepository(db)
	physical, err := sup.RecordSKUPhysicalMeasurement(ctx, f.product, sku, f.user, 100, 100, 100, 1000, "measured")
	if err != nil {
		t.Fatal(err)
	}
	bin := inv08Bin(t, db, warehouse, 1000, 1000)
	if err = wh.AssignPutawayTask(ctx, task, bin, f.user); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO warehouse_placements(id,task_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,quantity,volume_cm3,weight_grams,measurement_revision_id) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,1000,$9)`
	for _, v := range []struct {
		supplier, revision string
		volume             int
	}{{"other-owner", physical.Revision, 1000}, {f.supplier, physical.Revision, 999}, {f.supplier, "missing-revision", 1000}} {
		if _, err = db.Exec(insert, uuid.NewString(), task, f.product, sku, v.supplier, warehouse, bin, v.volume, v.revision); err == nil {
			t.Fatal("forged placement permanently occupied task identity")
		}
	}
	// Simulates a historical/raw physical edit outside the repository. Current
	// measurement guard clears verification; old reservation is never sufficient.
	opsExec(t, db, `UPDATE product_variants SET length_mm=101 WHERE id=$1`, sku)
	if _, err = db.Exec(insert, uuid.NewString(), task, f.product, sku, f.supplier, warehouse, bin, 1000, physical.Revision); err == nil {
		t.Fatal("stale placement evidence accepted")
	}
	if err = wh.ConfirmPutaway(ctx, task, f.user); err == nil {
		t.Fatal("stale raw edit confirmed")
	}
}
