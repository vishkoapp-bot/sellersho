package worker

import (
	"context"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
	"log"
	"time"
)

// One bounded pass on startup and every two minutes. Mutations are idempotent
// and also work when two application replicas run a pass concurrently.
func (w *BackgroundWorker) RunOperationsCycle() {
	if w.db == nil || w.orderRepo == nil {
		return
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if _, e := w.db.ExecContext(cleanupCtx, `DELETE FROM ad_request_limits WHERE bucket<NOW()-INTERVAL '10 minutes'`); e != nil {
		log.Printf("[WARN] ad-rate cleanup: %v", e)
	}
	if _, e := w.db.ExecContext(cleanupCtx, `DELETE FROM ad_fraud_keys WHERE expires_at<NOW()-INTERVAL '1 hour'`); e != nil {
		log.Printf("[WARN] ad-proof cleanup: %v", e)
	}
	cleanupCancel()
	jobs := []struct {
		name string
		run  func(context.Context) (int64, error)
	}{
		{"unpaid-order-expiry", func(ctx context.Context) (int64, error) {
			orders, e := w.orderRepo.ExpireUnpaidOrders(ctx)
			if e != nil {
				return 0, e
			}
			for _, o := range orders {
				if w.redisClient != nil {
					if e = w.redisClient.ReleaseOrderInventory(ctx, o.ID, o.Products...); e != nil {
						log.Printf("[WARN] Release expired hold %s: %v (Redis TTL remains active)", o.ID, e)
					}
				}
			}
			return int64(len(orders)), nil
		}},
		{"qc-response-deadline", func(ctx context.Context) (int64, error) { return repository.EscalateQCDeadlines(ctx, w.db) }},
		{"supplier-sla", func(ctx context.Context) (int64, error) { return repository.RefreshSupplierSLA(ctx, w.db) }},
	}
	for _, j := range jobs {
		ctx, cancel := context.WithTimeout(observability.NewContext(context.Background()), 40*time.Second)
		started := time.Now()
		n, e := j.run(ctx)
		outcome := "success"
		if e != nil {
			outcome = "error"
		}
		operation := "reconciliation"
		if j.name == "unpaid-order-expiry" {
			operation = "expiry"
		}
		observability.LogOperation(ctx, "worker", operation, outcome, time.Since(started))
		cancel()
		logCtx, logCancel := context.WithTimeout(context.Background(), 5*time.Second)
		repository.RecordOperationsJob(logCtx, w.db, j.name, started, n, e)
		logCancel()
		if e != nil {
			log.Printf("[WARN] %s: %v", j.name, e)
		}
	}
}
