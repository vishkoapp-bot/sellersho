package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/drup/platform/internal/testredis"
	"github.com/redis/go-redis/v9"
)

func testRedisClient(t *testing.T) *RedisClient {
	t.Helper()
	return &RedisClient{Client: testredis.Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN"))}
}

func TestInventoryHoldWindow(t *testing.T) {
	client := testRedisClient(t)

	ctx := context.Background()
	productID := fmt.Sprintf("test-product-%d", time.Now().UnixNano())
	orderA := fmt.Sprintf("test-order-a-%d", time.Now().UnixNano())
	orderB := fmt.Sprintf("test-order-b-%d", time.Now().UnixNano())
	defer client.ReleaseOrderInventory(ctx, orderA, InventoryHoldRef{ProductID: productID})
	defer client.ReleaseOrderInventory(ctx, orderB, InventoryHoldRef{ProductID: productID})

	if err := client.HoldInventory(ctx, orderA, productID, "", 1, 1, 10*time.Minute); err != nil {
		t.Fatalf("first payment-window hold should succeed: %v", err)
	}

	if err := client.HoldInventory(ctx, orderB, productID, "", 1, 1, 10*time.Minute); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("second hold should fail with ErrInsufficientInventory, got %v", err)
	}

	held, err := client.HasInventoryHold(ctx, orderA, productID, "", 1)
	if err != nil || !held {
		t.Fatalf("first hold should remain active, held=%v err=%v", held, err)
	}

	if err := client.ReleaseOrderInventory(ctx, orderA, InventoryHoldRef{ProductID: productID}); err != nil {
		t.Fatalf("releasing the first hold should succeed: %v", err)
	}
	if err := client.HoldInventory(ctx, orderB, productID, "", 1, 1, 10*time.Minute); err != nil {
		t.Fatalf("second hold should succeed after release: %v", err)
	}
}

func TestConcurrentInventoryHoldsCannotOversell(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	productID := fmt.Sprintf("last-unit-%d", time.Now().UnixNano())
	orderIDs := []string{fmt.Sprintf("last-unit-a-%d", time.Now().UnixNano()), fmt.Sprintf("last-unit-b-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		for _, id := range orderIDs {
			_ = client.ReleaseOrderInventory(ctx, id, InventoryHoldRef{ProductID: productID})
		}
	})

	start := make(chan struct{})
	results := make(chan error, len(orderIDs))
	var wg sync.WaitGroup
	for _, orderID := range orderIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			results <- client.HoldInventory(ctx, id, productID, "", 1, 1, time.Minute)
		}(orderID)
	}
	close(start)
	wg.Wait()
	close(results)

	accepted, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrInsufficientInventory):
			rejected++
		default:
			t.Fatalf("unexpected inventory hold error: %v", err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent last-unit holds accepted=%d rejected=%d; want 1 each", accepted, rejected)
	}
}

// heldAggregate reads the maintained aggregate for one pool (test oracle).
func heldAggregate(t *testing.T, client *RedisClient, ref InventoryHoldRef) int {
	t.Helper()
	_, agg, _ := holdKeys(ref)
	v, err := client.Client.Get(context.Background(), agg).Result()
	if err != nil {
		// An absent aggregate means zero: the release script deletes it
		// together with the ZSET when the last member leaves, and the
		// next hold rebuilds it from a scan.
		if errors.Is(err, redis.Nil) {
			return 0
		}
		t.Fatalf("read aggregate: %v", err)
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("aggregate %q is not an integer: %v", v, err)
	}
	return n
}

// TestHoldDuplicateAndDoubleRelease (G-07): a re-hold replaces the quantity
// exactly once in the aggregate, and a double release is a no-op.
func TestHoldDuplicateAndDoubleRelease(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	ref := InventoryHoldRef{ProductID: fmt.Sprintf("dup-%d", time.Now().UnixNano())}
	order := fmt.Sprintf("dup-order-%d", time.Now().UnixNano())

	if err := client.HoldInventory(ctx, order, ref.ProductID, "", 2, 10, 10*time.Minute); err != nil {
		t.Fatalf("first hold: %v", err)
	}
	if got := heldAggregate(t, client, ref); got != 2 {
		t.Fatalf("aggregate after first hold = %d, want 2", got)
	}
	// Re-hold the same order with a new quantity: replaces, does not add.
	if err := client.HoldInventory(ctx, order, ref.ProductID, "", 3, 10, 10*time.Minute); err != nil {
		t.Fatalf("re-hold: %v", err)
	}
	if got := heldAggregate(t, client, ref); got != 3 {
		t.Fatalf("aggregate after re-hold = %d, want 3", got)
	}
	held, err := client.HasInventoryHold(ctx, order, ref.ProductID, "", 3)
	if err != nil || !held {
		t.Fatalf("re-held quantity should verify, held=%v err=%v", held, err)
	}
	if err := client.ReleaseInventory(ctx, order, ref.ProductID, ""); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if got := heldAggregate(t, client, ref); got != 0 {
		t.Fatalf("aggregate after release = %d, want 0", got)
	}
	// Double release: no error, aggregate stays 0.
	if err := client.ReleaseInventory(ctx, order, ref.ProductID, ""); err != nil {
		t.Fatalf("double release should be a no-op, got %v", err)
	}
	if got := heldAggregate(t, client, ref); got != 0 {
		t.Fatalf("aggregate after double release = %d, want 0", got)
	}
}

// TestHoldVariantAdmission (G-06): holds are per-SKU. Two orders for the
// last unit of ONE variant admit exactly one; the product-level pool is
// separate and does not admit against variant stock.
func TestHoldVariantAdmission(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	product := fmt.Sprintf("sku-product-%d", time.Now().UnixNano())
	variant := fmt.Sprintf("sku-variant-%d", time.Now().UnixNano())
	orderA := fmt.Sprintf("sku-a-%d", time.Now().UnixNano())
	orderB := fmt.Sprintf("sku-b-%d", time.Now().UnixNano())
	orderC := fmt.Sprintf("sku-c-%d", time.Now().UnixNano())
	defer client.ReleaseOrderInventory(ctx, orderA, InventoryHoldRef{ProductID: product, VariantID: variant})
	defer client.ReleaseOrderInventory(ctx, orderB, InventoryHoldRef{ProductID: product, VariantID: variant})
	defer client.ReleaseOrderInventory(ctx, orderC, InventoryHoldRef{ProductID: product})

	// Variant pool: stock 1, two contenders, exactly one admits.
	if err := client.HoldInventory(ctx, orderA, product, variant, 1, 1, 10*time.Minute); err != nil {
		t.Fatalf("first variant hold should succeed: %v", err)
	}
	if err := client.HoldInventory(ctx, orderB, product, variant, 1, 1, 10*time.Minute); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("second variant hold should fail with ErrInsufficientInventory, got %v", err)
	}
	// Product pool is a different pool: its own stock bound applies.
	if err := client.HoldInventory(ctx, orderC, product, "", 1, 100, 10*time.Minute); err != nil {
		t.Fatalf("product-level hold should succeed independently: %v", err)
	}
	if got := heldAggregate(t, client, InventoryHoldRef{ProductID: product, VariantID: variant}); got != 1 {
		t.Fatalf("variant aggregate = %d, want 1", got)
	}
	if got := heldAggregate(t, client, InventoryHoldRef{ProductID: product}); got != 1 {
		t.Fatalf("product aggregate = %d, want 1", got)
	}
}

// TestReleaseOrderContinuesAfterError (G-05): a failure releasing one
// product's hold must not leak the remaining products' holds, and the
// error must be returned (joined) instead of swallowed.
func TestReleaseOrderContinuesAfterError(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	order := fmt.Sprintf("partial-%d", time.Now().UnixNano())
	products := []string{
		fmt.Sprintf("partial-p1-%d", time.Now().UnixNano()),
		fmt.Sprintf("partial-p2-%d", time.Now().UnixNano()),
		fmt.Sprintf("partial-p3-%d", time.Now().UnixNano()),
	}
	refs := []InventoryHoldRef{
		{ProductID: products[0]}, {ProductID: products[1]}, {ProductID: products[2]},
	}
	for _, ref := range refs {
		if err := client.HoldInventory(ctx, order, ref.ProductID, "", 1, 100, 10*time.Minute); err != nil {
			t.Fatalf("hold %s: %v", ref.ProductID, err)
		}
	}
	// Corrupt p2's quantity key type so its release raises WRONGTYPE.
	_, _, prefix := holdKeys(refs[1])
	if err := client.Client.Del(ctx, prefix+order).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Client.LPush(ctx, prefix+order, "x").Err(); err != nil {
		t.Fatal(err)
	}

	err := client.ReleaseOrderInventory(ctx, order, refs...)
	if err == nil {
		t.Fatalf("release should return the p2 failure, got nil")
	}
	// p1 and p3 must be released despite the p2 failure. p2's own hold
	// stays (its release genuinely failed) and is bounded by its TTL;
	// check it via ZSCORE, since HasInventoryHold would trip on the
	// corrupted quantity key by construction.
	for i, ref := range refs {
		if i == 1 {
			holdsKey, _, _ := holdKeys(ref)
			if _, err := client.Client.ZScore(ctx, holdsKey, order).Result(); err != nil {
				t.Fatalf("failed pool's ZSET member should remain: %v", err)
			}
			continue
		}
		held, herr := client.HasInventoryHold(ctx, order, ref.ProductID, "", 1)
		if herr != nil {
			t.Fatalf("HasInventoryHold %s: %v", ref.ProductID, herr)
		}
		if held {
			t.Fatalf("hold %s leaked after partial release failure", ref.ProductID)
		}
	}
}

// TestAggregateMatchesScan (G-08): after a mixed sequence of holds,
// re-holds, releases and expirations, the maintained aggregate equals the
// exact full-scan sum.
func TestAggregateMatchesScan(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	ref := InventoryHoldRef{ProductID: fmt.Sprintf("agg-%d", time.Now().UnixNano())}
	orders := []string{
		fmt.Sprintf("agg-a-%d", time.Now().UnixNano()),
		fmt.Sprintf("agg-b-%d", time.Now().UnixNano()),
		fmt.Sprintf("agg-c-%d", time.Now().UnixNano()),
	}
	// a=2, b=3, c re-held 1->4, then b released: live sum = 2+4 = 6.
	if err := client.HoldInventory(ctx, orders[0], ref.ProductID, "", 2, 100, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := client.HoldInventory(ctx, orders[1], ref.ProductID, "", 3, 100, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := client.HoldInventory(ctx, orders[2], ref.ProductID, "", 1, 100, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := client.HoldInventory(ctx, orders[2], ref.ProductID, "", 4, 100, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := client.ReleaseInventory(ctx, orders[1], ref.ProductID, ""); err != nil {
		t.Fatal(err)
	}
	if got := heldAggregate(t, client, ref); got != 6 {
		t.Fatalf("maintained aggregate = %d, want 6", got)
	}
	scanned, err := client.ReconcileHeldAggregate(ctx, ref)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if scanned != 6 {
		t.Fatalf("reconciled sum = %d, want 6", scanned)
	}
	if got := heldAggregate(t, client, ref); got != 6 {
		t.Fatalf("aggregate after reconcile = %d, want 6", got)
	}
}

// TestExpiredHolderBurstRepairs (G-04 semantics + G-08): 1,000 expired
// members whose quantity keys were already reaped leave a phantom
// over-count in the aggregate (safe direction: the next hold still
// succeeds); the reconciler repairs it to the exact live sum.
func TestExpiredHolderBurstRepairs(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	ref := InventoryHoldRef{ProductID: fmt.Sprintf("burst-%d", time.Now().UnixNano())}
	holdsKey, _, prefix := holdKeys(ref)

	// 1,000 live holds: aggregate = 1,000.
	for i := 0; i < 1000; i++ {
		member := fmt.Sprintf("burst-member-%d", i)
		if err := client.HoldInventory(ctx, member, ref.ProductID, "", 1, 100000, 10*time.Minute); err != nil {
			t.Fatalf("seed hold %d: %v", i, err)
		}
	}
	if got := heldAggregate(t, client, ref); got != 1000 {
		t.Fatalf("seed aggregate = %d, want 1000", got)
	}
	// Simulate TTL reap of every quantity key, then expire the members.
	pipe := client.Client.Pipeline()
	for i := 0; i < 1000; i++ {
		pipe.Del(ctx, prefix+fmt.Sprintf("burst-member-%d", i))
		pipe.ZAdd(ctx, holdsKey, redis.Z{Score: float64(time.Now().Add(-time.Minute).UnixMilli()) / 1000, Member: fmt.Sprintf("burst-member-%d", i)})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("expire seed: %v", err)
	}
	// One new hold: bounded cleanup reaps 128 expired members (whose keys
	// are gone, so the aggregate keeps their phantom quantity — safe
	// over-count) and still admits.
	probe := fmt.Sprintf("burst-probe-%d", time.Now().UnixNano())
	if err := client.HoldInventory(ctx, probe, ref.ProductID, "", 1, 100000, 10*time.Minute); err != nil {
		t.Fatalf("hold after expired burst should succeed: %v", err)
	}
	// 1,000 phantom (expired members whose keys are gone) + the live
	// probe's own 1: the phantom over-count is the safe direction.
	if got := heldAggregate(t, client, ref); got != 1001 {
		t.Fatalf("aggregate after burst hold = %d, want phantom 1000 + live 1", got)
	}
	// The reconciler repairs the phantom to the exact live sum (1 probe).
	scanned, err := client.ReconcileHeldAggregate(ctx, ref)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if scanned != 1 {
		t.Fatalf("reconciled sum = %d, want 1", scanned)
	}
	if got := heldAggregate(t, client, ref); got != 1 {
		t.Fatalf("aggregate after reconcile = %d, want 1", got)
	}
}

// TestHoldEndsAtDeadlineDespiteQtyGrace: the hold ends at the ZSET
// deadline (HasInventoryHold false, admission re-opens) even though the
// quantity key outlives the deadline by the 60 s PERF-04 grace.
func TestHoldEndsAtDeadlineDespiteQtyGrace(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()
	ref := InventoryHoldRef{ProductID: fmt.Sprintf("grace-%d", time.Now().UnixNano())}
	order := fmt.Sprintf("grace-order-%d", time.Now().UnixNano())
	if err := client.HoldInventory(ctx, order, ref.ProductID, "", 1, 1, 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	held, err := client.HasInventoryHold(ctx, order, ref.ProductID, "", 1)
	if err != nil || held {
		t.Fatalf("hold should end at its deadline, held=%v err=%v", held, err)
	}
	// The quantity key is still present (grace) but the pool admits again.
	if err := client.HoldInventory(ctx, order+"-2", ref.ProductID, "", 1, 1, time.Minute); err != nil {
		t.Fatalf("pool should admit after deadline: %v", err)
	}
}
