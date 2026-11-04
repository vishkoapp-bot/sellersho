package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestInventoryHoldWindow(t *testing.T) {
	client, err := NewRedisClient("redis://:drup_redis_pass_2026@localhost:6379/0")
	if err != nil {
		t.Skipf("Redis integration test unavailable: %v", err)
	}

	ctx := context.Background()
	productID := fmt.Sprintf("test-product-%d", time.Now().UnixNano())
	orderA := fmt.Sprintf("test-order-a-%d", time.Now().UnixNano())
	orderB := fmt.Sprintf("test-order-b-%d", time.Now().UnixNano())
	defer client.ReleaseOrderInventory(ctx, orderA, productID)
	defer client.ReleaseOrderInventory(ctx, orderB, productID)

	if err := client.HoldInventory(ctx, orderA, productID, 1, 1, 10*time.Minute); err != nil {
		t.Fatalf("first payment-window hold should succeed: %v", err)
	}

	if err := client.HoldInventory(ctx, orderB, productID, 1, 1, 10*time.Minute); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("second hold should fail with ErrInsufficientInventory, got %v", err)
	}

	held, err := client.HasInventoryHold(ctx, orderA, productID, 1)
	if err != nil || !held {
		t.Fatalf("first hold should remain active, held=%v err=%v", held, err)
	}

	if err := client.ReleaseOrderInventory(ctx, orderA, productID); err != nil {
		t.Fatalf("releasing the first hold should succeed: %v", err)
	}
	if err := client.HoldInventory(ctx, orderB, productID, 1, 1, 10*time.Minute); err != nil {
		t.Fatalf("second hold should succeed after release: %v", err)
	}
}

func TestConcurrentInventoryHoldsCannotOversell(t *testing.T) {
	client, err := NewRedisClient("redis://:drup_redis_pass_2026@localhost:6379/0")
	if err != nil {
		t.Skipf("Redis integration test unavailable: %v", err)
	}
	ctx := context.Background()
	productID := fmt.Sprintf("last-unit-%d", time.Now().UnixNano())
	orderIDs := []string{fmt.Sprintf("last-unit-a-%d", time.Now().UnixNano()), fmt.Sprintf("last-unit-b-%d", time.Now().UnixNano())}
	t.Cleanup(func() {
		for _, id := range orderIDs {
			_ = client.ReleaseOrderInventory(ctx, id, productID)
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
			results <- client.HoldInventory(ctx, id, productID, 1, 1, time.Minute)
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
