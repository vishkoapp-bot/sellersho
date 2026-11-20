package repository

import (
	"context"
	"testing"
)

// FIN-013 §2.4/§5 — the unknown-outcome work queue as an admin surface.
// A paid cancellation creates its obligation with status 'unknown',
// so the queue lists it oldest-first with its age; the reconciler
// with a nil provider (FIN-010 blocked) is a recorded no-op that
// leaves the obligation untouched and visible.
//
// Synchronization: sequential disposable-DB assertions; no
// concurrency is involved, so no barrier is needed (audit §1 d).
func TestFIN013RefundUnknownQueueListsCancellation(t *testing.T) {
	db, f, _, order := fin003PaidOrder(t)
	ctx := context.Background()
	repo := NewOrderRepository(db)
	if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
		t.Fatal(err)
	}

	items, err := repo.ListUnknownRefunds(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	// The queue is global, oldest-first, and capped by the limit — so
	// the window must be wide enough to include this test's own row
	// even when sibling tests (e.g. the hot-account cancels, ~58
	// unknown obligations) ran first on the shared disposable DB.
	// Assert on this test's own order, not the absolute length.
	var mine []UnknownRefundQueueItem
	for _, it := range items {
		if it.OrderID == order {
			mine = append(mine, it)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("unknown queue entries for this order=%d, want 1 (the paid cancellation; %d total listed)", len(mine), len(items))
	}
	it := mine[0]
	if it.OrderID != order || it.AmountToman != 10000000 || it.Purpose != "cancellation" {
		t.Fatalf("queue item=%+v, want order=%s amount=10000000 purpose=cancellation (Toman)", it, order)
	}
	if it.AgeSeconds < 0 {
		t.Fatalf("queue item age=%d, want >= 0", it.AgeSeconds)
	}
	if it.CurrentState != "unknown" && it.CurrentState != "pending" {
		t.Fatalf("queue item state=%q, want unknown or pending (latest-event resolution)", it.CurrentState)
	}

	// Nil-provider reconciler tick: 0 completed, no error, obligation
	// stays unknown and stays listed.
	completed, err := repo.ReconcileOrderRefunds(ctx, nil, 25)
	if err != nil || completed != 0 {
		t.Fatalf("nil-provider reconcile: completed=%d err=%v, want 0/nil (recorded no-op until FIN-010)", completed, err)
	}
	again, err := repo.ListUnknownRefunds(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	still := false
	for _, listed := range again {
		if listed.ObligationID == it.ObligationID {
			still = true
		}
	}
	if !still {
		t.Fatalf("queue after nil-provider tick: obligation %s is no longer listed (want it still queued and visible)", it.ObligationID)
	}
}
