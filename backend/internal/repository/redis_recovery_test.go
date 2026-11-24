package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestRebuildInventoryHolds (G-09): the manual PG-authoritative rebuild
// re-creates holds only for orders that are still payable — pending,
// not captured, deadline in the future — and never resurrects holds for
// expired or cancelled orders. Re-running it is idempotent.
func TestRebuildInventoryHolds(t *testing.T) {
	db, schema, sku, _ := skuInventoryFixture(t)
	ctx := context.Background()
	redis := testRedisClient(t)
	live, expired, cancelled := "rebuild-live", "rebuild-expired", "rebuild-cancelled"
	tx := skuTestTx(t, db, schema)
	defer tx.Rollback()
	for _, id := range []string{live, expired, cancelled} {
		if _, err := tx.Exec(`INSERT INTO orders(id) VALUES($1)`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO order_items(id,order_id,product_id,variant_id,quantity) VALUES($1,$2,'p',$3,1)`, uuid.NewString(), id, sku); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE orders SET status='cancelled' WHERE id=$1`, cancelled); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	r := NewOrderRepository(skuScopedDB(t, schema))
	aggregate := InventoryHoldRef{ProductID: "p", VariantID: sku}
	origin := InventoryHoldRef{ProductID: "p", VariantID: sku, SupplierWarehouseID: "sw1"}
	t.Cleanup(func() { _ = redis.ReleaseOrderInventory(ctx, live, aggregate, origin) })

	rebuilt, err := r.RebuildInventoryHolds(ctx, redis)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt != 2 {
		t.Fatalf("rebuilt = %d, want 2 (SKU and exact origin of the live pending order)", rebuilt)
	}
	held, err := redis.HasInventoryHold(ctx, live, "p", sku, 1)
	if err != nil || !held {
		t.Fatalf("live order hold should exist after rebuild, held=%v err=%v", held, err)
	}
	for _, id := range []string{expired, cancelled} {
		held, err := redis.HasInventoryHold(ctx, id, "p", sku, 1)
		if err != nil {
			t.Fatalf("HasInventoryHold %s: %v", id, err)
		}
		if held {
			t.Fatalf("order %s must not get a hold back from rebuild", id)
		}
	}
	for _, ref := range []InventoryHoldRef{aggregate, origin} {
		if got := heldAggregate(t, redis, ref); got != 1 {
			t.Fatalf("pool %+v after rebuild = %d, want 1", ref, got)
		}
	}

	// Second run: idempotent — same pool rebuilt, aggregate unchanged.
	rebuilt, err = r.RebuildInventoryHolds(ctx, redis)
	if err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	if rebuilt != 2 {
		t.Fatalf("second rebuild = %d, want 2", rebuilt)
	}
	if got := heldAggregate(t, redis, aggregate); got != 1 {
		t.Fatalf("aggregate after second rebuild = %d, want 1", got)
	}

	// The rebuilt hold behaves like a normal hold: it ends at the deadline.
	if err := redis.ReleaseOrderInventory(ctx, live, aggregate, origin); err != nil {
		t.Fatalf("release rebuilt hold: %v", err)
	}
}
