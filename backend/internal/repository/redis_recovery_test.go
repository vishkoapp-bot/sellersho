package repository

import (
	"context"
	"os"
	"testing"

	"github.com/drup/platform/internal/testredis"
)

// TestRebuildInventoryHolds (G-09): the manual PG-authoritative rebuild
// re-creates holds only for orders that are still payable — pending,
// not captured, deadline in the future — and never resurrects holds for
// expired or cancelled orders. Re-running it is idempotent.
func TestRebuildInventoryHolds(t *testing.T) {
	db := operationsDB(t)
	f := opsFixtureNew(t, db)
	ctx := context.Background()
	redis := &RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
	r := NewOrderRepository(db)

	live, _ := opsOrder(t, db, f, false)
	expired, _ := opsOrder(t, db, f, false)
	cancelled, _ := opsOrder(t, db, f, false)
	opsExec(t, db, `UPDATE orders SET payment_expires_at=NOW()+INTERVAL '10 minutes' WHERE id=$1`, live)
	opsExec(t, db, `UPDATE orders SET payment_expires_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, expired)
	opsExec(t, db, `UPDATE orders SET status='cancelled', payment_expires_at=NOW()+INTERVAL '10 minutes' WHERE id=$1`, cancelled)

	rebuilt, err := r.RebuildInventoryHolds(ctx, redis)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if rebuilt != 1 {
		t.Fatalf("rebuilt = %d, want 1 (only the live pending order)", rebuilt)
	}
	held, err := redis.HasInventoryHold(ctx, live, f.product, "", 1)
	if err != nil || !held {
		t.Fatalf("live order hold should exist after rebuild, held=%v err=%v", held, err)
	}
	for _, id := range []string{expired, cancelled} {
		held, err := redis.HasInventoryHold(ctx, id, f.product, "", 1)
		if err != nil {
			t.Fatalf("HasInventoryHold %s: %v", id, err)
		}
		if held {
			t.Fatalf("order %s must not get a hold back from rebuild", id)
		}
	}
	if got := heldAggregate(t, redis, InventoryHoldRef{ProductID: f.product}); got != 1 {
		t.Fatalf("aggregate after rebuild = %d, want 1", got)
	}

	// Second run: idempotent — same pool rebuilt, aggregate unchanged.
	rebuilt, err = r.RebuildInventoryHolds(ctx, redis)
	if err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	if rebuilt != 1 {
		t.Fatalf("second rebuild = %d, want 1", rebuilt)
	}
	if got := heldAggregate(t, redis, InventoryHoldRef{ProductID: f.product}); got != 1 {
		t.Fatalf("aggregate after second rebuild = %d, want 1", got)
	}

	// The rebuilt hold behaves like a normal hold: it ends at the deadline.
	if err := redis.ReleaseOrderInventory(ctx, live, InventoryHoldRef{ProductID: f.product}); err != nil {
		t.Fatalf("release rebuilt hold: %v", err)
	}
}
