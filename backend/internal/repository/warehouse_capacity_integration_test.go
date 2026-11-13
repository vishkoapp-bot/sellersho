package repository

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/testdb"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func openWarehouseCapacityIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Skipf("Postgres integration unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Skipf("Postgres integration unavailable: %v", err)
	}
	return db
}

func TestPutawayCapacityConcurrencyAllowsOnlyOneReservation(t *testing.T) {
	db := openWarehouseCapacityIntegrationDB(t)
	defer db.Close()
	ctx := context.Background()
	suffix := uuid.NewString()[:10]
	productID := "cap-product-" + suffix
	binCode := "CAP-" + suffix
	taskA := "CAP-TASK-A-" + suffix
	taskB := "CAP-TASK-B-" + suffix
	if _, err := db.ExecContext(ctx, `INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method,length_mm,width_mm,height_mm,weight_grams,measurement_status) VALUES ($1,$2,$3,'digital','sup-technolife',100000,50000,1000,TRUE,2,'warehouse',100,100,100,500,'verified')`, productID, "Capacity concurrency product", productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO warehouse_bins (bin_code,hall_name,aisle,shelf,capacity_percent,quantity,warehouse_name,capacity_volume_cm3,occupied_volume_cm3,occupied_weight_grams,capacity_status) VALUES ($1,'Capacity Test','1','1',0,0,'Test',1000000,0,0,'verified')`, binCode); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO putaway_tasks (id,product_id,bin_location,quantity,status,created_at) VALUES ($1,$2,NULL,1,'pending',NOW()),($3,$2,NULL,1,'pending',NOW())`, taskA, productID, taskB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM putaway_capacity_reservations WHERE task_id IN ($1,$2)`, taskA, taskB)
		_, _ = db.ExecContext(ctx, `DELETE FROM putaway_tasks WHERE id IN ($1,$2)`, taskA, taskB)
		_, _ = db.ExecContext(ctx, `DELETE FROM warehouse_bins WHERE bin_code=$1`, binCode)
		_, _ = db.ExecContext(ctx, `DELETE FROM products WHERE id=$1`, productID)
	})
	repo := NewWarehouseRepository(db)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, taskID := range []string{taskA, taskB} {
		wg.Add(1)
		go func(id string) { defer wg.Done(); results <- repo.AssignPutawayTask(ctx, id, binCode, "capacity-test") }(taskID)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("capacity contention successes=%d, want exactly 1", successes)
	}
	var activeReservations int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM putaway_capacity_reservations WHERE bin_code=$1 AND status='active'`, binCode).Scan(&activeReservations); err != nil {
		t.Fatal(err)
	}
	if activeReservations != 1 {
		t.Fatalf("active reservations=%d, want 1", activeReservations)
	}
}
