package worker

import (
	"context"
	"testing"

	"github.com/drup/platform/internal/repository"
)

// FIN-013 §2.2 — the refund reconciler finally has a production
// caller. The job is in jobs() (60 s period, 10 s timeout, bounded
// batch of 25) and a tick with no PSP adapter (FIN-010 blocked) is a
// recorded no-op: 0 completed, no error, nothing reconciled, nothing
// hidden. Execution activates the moment FIN-010 wires the adapter
// via SetRefundProvider.
func TestFIN013RefundReconciliationNilProviderIsNoop(t *testing.T) {
	db := jobDB(t)
	w := NewBackgroundWorker(db)
	w.SetOrderRepository(repository.NewOrderRepository(db))

	found := false
	for _, job := range w.jobs() {
		if job.ID == "refund-reconciliation" {
			found = true
			if job.Period.String() != "1m0s" {
				t.Fatalf("refund-reconciliation period=%s, want 1m0s", job.Period)
			}
		}
	}
	if !found {
		t.Fatal("refund-reconciliation job missing from jobs()")
	}

	result, err := w.refundReconciliation(context.Background())
	if err != nil {
		t.Fatalf("nil-provider tick: %v", err)
	}
	if result.Affected != 0 {
		t.Fatalf("nil-provider tick affected=%d, want 0 (no adapter, nothing reconciled)", result.Affected)
	}
}
