package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func codedTestOrderAt(t *testing.T, tx *sql.Tx, order, sku, warehouse string) {
	t.Helper()
	if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, order); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,$2,'p',$3,1)`, uuid.NewString(), order, sku); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE order_item_origins SET supplier_warehouse_id=$2 WHERE order_id=$1`, order, warehouse); err != nil {
		t.Fatal(err)
	}
}

func TestSpncxStockReductionPreservesHeldOrigin(t *testing.T) {
	db, schema, sku, _ := skuInventoryFixture(t)
	ctx := context.Background()
	cache := testRedisClient(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	if err := creditSKUInventoryTx(ctx, tx, "p", "s", sku, 1, "sellable", "supplier", "supplier-warehouse:sw2", "sku_stock_adjustment", sku, "test-stock-other:"+sku, ""); err != nil {
		t.Fatal(err)
	}
	codedTestOrderAt(t, tx, "held-local", sku, "sw1")
	codedTestOrderAt(t, tx, "held-other", sku, "sw2")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	repo := NewOrderRepository(scoped)
	hold := func(order string) []InventoryHoldRef {
		t.Helper()
		refs, err := repo.ReserveCodedOrderInventory(ctx, cache, order, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cache.ReleaseOrderInventory(ctx, order, refs...) })
		return refs
	}
	refs := hold("held-local")
	// Supplier stock sums to 2 across sw1 and sw2. Reducing to 1
	// removes the sole sw1 unit; the sw2 code cannot back sw1's hold.
	tx = skuTestTx(t, db, schema)
	err := setTrackedSKUStockTx(ctx, tx, "p", "s", sku, 1, "reduce while sw1 has a hold", "", cache, nil)
	tx.Rollback()
	if !errors.Is(err, ErrStockBelowHeld) {
		t.Errorf("stock reduction retired sw1's held unit: %v", err)
	}
	if err := cache.ReleaseOrderInventory(ctx, "held-local", refs...); err != nil {
		t.Fatal(err)
	}
	hold("held-other")
	tx = skuTestTx(t, db, schema)
	defer tx.Rollback()
	if err := setTrackedSKUStockTx(ctx, tx, "p", "s", sku, 1, "reduce unrelated sw1 stock", "", cache, nil); err != nil {
		t.Fatalf("stock reduction at sw1 blocked by unrelated sw2 hold: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	origin := InventoryHoldRef{ProductID: "p", VariantID: sku, SupplierWarehouseID: "sw2"}
	if count, err := cache.liveInventoryHeld(ctx, origin); err != nil || count != 1 {
		t.Fatalf("unrelated exact hold was lost: count=%d err=%v", count, err)
	}
}

func TestSpncxReservationCannotUseStaleCapacityDuringRetirement(t *testing.T) {
	db, schema, sku, _ := skuInventoryFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cache := testRedisClient(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	codedTestOrderAt(t, tx, "retirement-race", sku, "sw1")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	tx = skuTestTx(t, db, schema)
	defer tx.Rollback()
	if err := setTrackedSKUStockTx(ctx, tx, "p", "s", sku, 0, "retire before reserve", "", cache, nil); err != nil {
		t.Fatal(err)
	}
	var blocker int
	if err := tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	repo := NewOrderRepository(scoped)
	result := make(chan error, 1)
	go func() {
		refs, err := repo.ReserveCodedOrderInventory(ctx, cache, "retirement-race", time.Now().Add(time.Minute))
		if len(refs) > 0 {
			_ = cache.ReleaseOrderInventory(ctx, "retirement-race", refs...)
		}
		result <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			t.Fatalf("reservation bypassed an in-flight stock reduction: %v", err)
		case <-ctx.Done():
			t.Fatalf("reservation did not reach the stock mutex: %v", ctx.Err())
		case <-ticker.C:
			var waiting bool
			if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, blocker).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if !waiting {
				continue
			}
		}
		break
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("retired stock was reserved: %v", err)
	}
}

func TestSpncxLegacyPhysicalDecreaseCannotInvalidateCodedHold(t *testing.T) {
	db, schema, _, _ := skuInventoryFixture(t)
	ctx := context.Background()
	cache := testRedisClient(t)
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	sku := uuid.NewString()
	for _, step := range []struct {
		query string
		args  []any
	}{
		{`CREATE TABLE audit_logs (id text, timestamp timestamptz, operator text, action text, category text, details jsonb)`, nil},
		{`INSERT INTO products(id,supplier_id) VALUES('legacy-held','s')`, nil},
		{`INSERT INTO product_variants(id,product_id,spnc,is_default,inventory_mode) VALUES($1,'legacy-held','LEGACY-HELD',TRUE,'legacy')`, []any{sku}},
		{`INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity)
			VALUES('legacy-held-lot','legacy-held','s','supplier','supplier','supplier-warehouse:sw1','sellable',1)`, nil},
		{`INSERT INTO product_units(code,variant_id,product_id,sequence,status,balance_id)
			VALUES('LEGACY-HELD-X0001',$1,'legacy-held',1,'in_stock','legacy-held-lot')`, []any{sku}},
		{`INSERT INTO orders(id) VALUES('legacy-held-order')`, nil},
		{`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,'legacy-held-order','legacy-held',$2,1)`, []any{uuid.NewString(), sku}},
	} {
		if _, err := tx.Exec(step.query, step.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	scoped := skuScopedDB(t, schema)
	refs, err := NewOrderRepository(scoped).ReserveCodedOrderInventory(ctx, cache, "legacy-held-order", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.ReleaseOrderInventory(ctx, "legacy-held-order", refs...) })
	if _, err := NewSupplierRepository(scoped).WithInventoryHolds(cache).AdjustProductStock(ctx, "s", "legacy-held", sku, 0, "reduce held legacy stock", ""); !errors.Is(err, ErrStockBelowHeld) {
		t.Fatalf("physical decrease invalidated a held legacy code: %v", err)
	}
}
