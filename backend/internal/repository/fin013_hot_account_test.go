package repository

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// FIN-013 §3.1 — hot-account re-run at repository level.
//
// The CAP-01 hot-account profile FAILED in b907: 100 paid, 0 cancelled
// (cancel p95 8,630 ms, none completed in-window), so the
// duplicate-cancel probe never exercised a completed cancel. This test
// is the repo-level re-run of the same shape — 100 paid orders on ONE
// shared store, concurrent paid-cancel + duplicate-cancel retry —
// against the disposable DB, so the cancel-latency cause can be
// measured without the HTTP/TLS/driver path in the loop.
//
// Synchronization (audit §1 d): 100 goroutines released simultaneously
// from a `start` channel + sync.WaitGroup (the P8 pattern); the pool
// is the repository's own (prod cap 25), so connection queueing is
// part of the measurement, not hidden. Assertions read the durable
// obligation rows after wg.Wait().
//
// Pass: 100 obligations, 0 duplicates, 0 missing, duplicate retries
// return the stored outcome — and cancel p95 within the mutation SLO
// (750 ms). If p95 breaches, the test fails with the measured
// latencies so the cause is investigated, not averaged away.
// TestFIN013HotAccountCancelSequentialBaseline measures one cancel at
// a time on the same shared-store/shared-product shape, so the
// concurrent test's queue can be attributed: if a single cancel takes
// ~T, 100 serialized cancels take ~100*T against the 5 s
// lock-acquisition budget and the 10 s cancel timeout — the tail
// fails by queueing, not by any single slow step.
func TestFIN013HotAccountCancelSequentialBaseline(t *testing.T) {
	db, f, warehouse, _, _ := inv06Placed(t, false, 10)
	ctx := context.Background()
	repo := NewOrderRepository(db)
	var total time.Duration
	for i := 0; i < 10; i++ {
		order, token := opsOrder(t, db, f, false)
		opsExec(t, db, `UPDATE order_items SET wholesale_cost=10000,seller_margin=90000,final_unit_price=100000 WHERE order_id=$1`, order)
		opsExec(t, db, `UPDATE orders SET total_amount=100000,payable_amount=100000,gateway_amount=100000 WHERE id=$1`, order)
		opsExec(t, db, `UPDATE payment_transactions SET amount=100000 WHERE order_id=$1`, order)
		seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
		if _, err := repo.MarkPaymentPaid(ctx, token, "verified-"+uuid.NewString()[:12], "mask"); err != nil {
			t.Fatalf("seed paid order %d: %v", i, err)
		}
		begin := time.Now()
		if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
			t.Fatalf("sequential cancel %d: %v", i, err)
		}
		d := time.Since(begin)
		total += d
		t.Logf("sequential cancel %d: %s", i, d)
	}
	t.Logf("sequential cancel mean: %s over 10 cancels (shared store/product, disposable DB)", total/10)
}

func TestFIN013HotAccountConcurrentCancel(t *testing.T) {
	// FIN-013 supervisor R1/R5 (2026-11-20): QUARANTINED — this test
	// is left SKIPPED on purpose, not deleted, so the hot-account
	// finding stays executable the moment the follow-up decision
	// lands. Do NOT un-skip it without that decision.
	//
	// Diagnosis (measured, repo level, 100 paid orders on ONE shared
	// store, concurrent cancel + duplicate retry): only 58–64/100
	// cancels succeed across runs (worker 58/100; supervisor reruns
	// 59/100 and 64/100); cancel p50 ~5.2s, p95 ~6.4s, max ~6.5s.
	// Dominant cause: 57014 "canceling statement due to user request"
	// on the shared store account-guard row under the 5s bounded
	// lock-acquisition budget — CancelOrder → lockOrderFinanceTx
	// locks the shared store account-guard row (LockAccountsTx) plus
	// the shared product/balance rows, so 100 cancels serialize on
	// that guard and the queue needs ~7s. Raising the 5s budget is
	// refuted by the numbers: serial cost ~73ms/cancel × 100 ≈ 7.3s,
	// so p95 would still be ~7–10s against the 750ms mutation SLO.
	// Guard serialization itself is correctness-preserving (FIN-001:
	// same-account money moves must serialize); the failure mode is
	// the problem — a cancel that times out waiting for the guard
	// returns a 500 instead of queueing or being admitted gracefully.
	// Follow-up (finance reviewer + supervisor decision, NOT part of
	// FIN-013): per-account admission/queueing with a retryable
	// response, or carving the hot-account shape out of the mutation
	// SLO. The HTTP hot-account re-run stays deferred until that
	// decision lands.
	//
	// Secondary mode (supervisor rerun 2: 4 of 36 failures): 53300
	// "remaining connection slots are reserved for roles with the
	// SUPERUSER attribute" (~1.2s) — a disposable-DB harness artifact
	// (100 concurrent connections vs PG max_connections/superuser
	// reserve), distinct from the guard-serialization cause, which
	// makes the raw success count noisy (58–64 across runs).
	t.Skip("FIN-013 hot-account finding, quarantined per supervisor R1/R5: 58–64/100 concurrent cancels succeed (p50 ~5.2s, p95 ~6.4s, max ~6.5s); 57014 on the shared store account-guard row under the 5s lock-acquisition budget (100 cancels serialize, queue needs ~7s; serial cost ~73ms/cancel ≈ 7.3s, so raising the budget still misses the 750ms mutation SLO); secondary 53300 pool-exhaustion failures are a disposable-DB harness artifact. Follow-up decision owed: per-account admission/queueing with a retryable response, or carving the hot-account shape out of the mutation SLO; HTTP re-run deferred until then.")
	db, f, warehouse, _, _ := inv06Placed(t, false, 100)
	ctx := context.Background()
	repo := NewOrderRepository(db)

	orders := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		order, token := opsOrder(t, db, f, false)
		opsExec(t, db, `UPDATE order_items SET wholesale_cost=10000,seller_margin=90000,final_unit_price=100000 WHERE order_id=$1`, order)
		opsExec(t, db, `UPDATE orders SET total_amount=100000,payable_amount=100000,gateway_amount=100000 WHERE id=$1`, order)
		opsExec(t, db, `UPDATE payment_transactions SET amount=100000 WHERE order_id=$1`, order)
		seedCanonicalWarehouseOrderOrigins(t, db, order, warehouse)
		if _, err := repo.MarkPaymentPaid(ctx, token, "verified-"+uuid.NewString()[:12], "mask"); err != nil {
			t.Fatalf("seed paid order %d: %v", i, err)
		}
		orders = append(orders, order)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	lat := make([]time.Duration, 100)
	errs := make([]error, 100)
	for i := range orders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			begin := time.Now()
			errs[i] = repo.CancelOrder(ctx, orders[i], f.user, false)
			lat[i] = time.Since(begin)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Logf("cancel %d (%s) FAILED after %s: %v", i, orders[i], lat[i], err)
		}
	}
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	sorted := append([]time.Duration(nil), lat...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	p50, p95, max := sorted[49], sorted[94], sorted[99]
	t.Logf("hot-account concurrent cancel: n=100 succeeded=%d p50=%s p95=%s max=%s (sequential mean was ~73ms; 100 cancels serialize on the shared store account-guard row in LockAccountsTx plus the shared product/balance rows, so the queue needs ~7s against the 5s lock-acquisition budget in lockOrderFinanceTx and the 10s cancel timeout)", succeeded, p50, p95, max)
	if succeeded != 100 {
		t.Fatalf("concurrent cancel: %d/100 succeeded — the b907 hot-account failure reproduces at repository level (shared store account guard serializes the cancels; the tail exceeds the lock/timeout budgets)", succeeded)
	}

	var count, distinct int
	var sum int64
	if err := db.QueryRow(`SELECT COUNT(*),COUNT(DISTINCT order_id),COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE purpose='cancellation' AND order_id = ANY($1)`, pq.Array(orders)).Scan(&count, &distinct, &sum); err != nil {
		t.Fatal(err)
	}
	if count != 100 || distinct != 100 || sum != 10000000 {
		t.Fatalf("obligation oracle: count=%d distinct=%d sum=%d, want 100/100/10000000 (Toman)", count, distinct, sum)
	}
	var missing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders WHERE id = ANY($1) AND payment_status='paid' AND status='cancelled' AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id=orders.id AND r.purpose='cancellation')`, pq.Array(orders)).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("paid-cancelled orders without an obligation: %d, want 0", missing)
	}

	// Duplicate-cancel retry: re-cancel 10 of the orders; the stored
	// outcome must return and no second obligation may appear.
	for _, order := range orders[:10] {
		if err := repo.CancelOrder(ctx, order, f.user, false); err != nil {
			t.Fatalf("duplicate cancel %s: %v", order, err)
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_refund_obligations WHERE purpose='cancellation' AND order_id = ANY($1)`, pq.Array(orders)).Scan(&count); err != nil || count != 100 {
		t.Fatalf("duplicate-cancel oracle: count=%d, want 100 (err=%v)", count, err)
	}

	if p95 > 750*time.Millisecond {
		t.Errorf("cancel p95=%s exceeds the mutation SLO (750 ms) — the b907 hot-account failure is NOT fixed at repository level; investigate before the HTTP re-run", p95)
	}
}
