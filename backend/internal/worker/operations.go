package worker

import (
	"context"
	"errors"
	"github.com/drup/platform/internal/jobrunner"
	"github.com/drup/platform/internal/repository"
	"time"
)

func (w *BackgroundWorker) jobs() []jobrunner.Job {
	return []jobrunner.Job{
		{ID: "unpaid-order-expiry", Period: 5 * time.Second, Timeout: 3 * time.Second, Expiry: true, Run: w.expireOrders},
		{ID: "ad-cleanup", Period: 10 * time.Second, Timeout: 2 * time.Second, Run: func(ctx context.Context) (jobrunner.Result, error) {
			n, e := repository.CleanupExpiredAds(ctx, w.db)
			return jobrunner.Result{Affected: n}, e
		}},
		{ID: "qc-response-deadline", Period: 30 * time.Second, Timeout: 2 * time.Second, Run: func(ctx context.Context) (jobrunner.Result, error) {
			n, e := repository.EscalateQCDeadlines(ctx, w.db)
			return jobrunner.Result{Affected: n}, e
		}},
		{ID: "supplier-sla", Period: 30 * time.Second, Timeout: 3 * time.Second, Run: func(ctx context.Context) (jobrunner.Result, error) {
			n, e := repository.RefreshSupplierSLA(ctx, w.db)
			return jobrunner.Result{Affected: n}, e
		}},
		{ID: "delivery-lock-releaser", Period: 60 * time.Second, Timeout: 10 * time.Second, Run: func(ctx context.Context) (jobrunner.Result, error) {
			n, c, e := w.deliveryCycle(ctx)
			return jobrunner.Result{Affected: n, Confirmed: c}, e
		}},
		{ID: "monthly-settlement", Period: 60 * time.Second, Timeout: 4 * time.Second, Run: w.monthlySettlement},
	}
}
func (w *BackgroundWorker) expireOrders(ctx context.Context) (jobrunner.Result, error) {
	if w.orderRepo == nil {
		return jobrunner.Result{}, errors.New("order repository unavailable")
	}
	orders, err := w.orderRepo.ExpireUnpaidOrders(ctx)
	for _, o := range orders {
		if w.redisClient != nil {
			err = errors.Join(err, w.redisClient.ReleaseOrderInventory(ctx, o.ID, o.Products...))
		}
	}
	return jobrunner.Result{Affected: int64(len(orders))}, err
}

// Compatibility entrypoint; every pass shares the nonqueued admission.
func (w *BackgroundWorker) RunOperationsCycle() {
	if w.db == nil {
		return
	}
	for _, id := range []string{"unpaid-order-expiry", "ad-cleanup", "qc-response-deadline", "supplier-sla"} {
		_, _ = w.runner().Run(context.Background(), id)
	}
}
