package worker

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/drup/platform/internal/jobrunner"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
)

// RunMonthlySettlementIfDue runs the free automatic monthly sweep
// on the admin-configured settlement day and during the 3 following
// days (D1 catch-up, N=3), and only once per
// month (the cycle period is the primary key, and
// RunMonthlySettlement returns the existing cycle on a second run).
// It is inert when the worker has no finance repository (tests that
// construct the worker bare) and when settlement is not configured:
// an unset settlement_day is the designed safe default, so that case
// is deliberately NOT logged every tick. Any other failure is
// logged as a warning; the next tick retries. U02: the automatic
// trigger itself is a user decision for Phase 3, not a verified live
// behaviour — live-parity had only the manual trigger.
func (w *BackgroundWorker) RunMonthlySettlementIfDue() {
	if w.db == nil || w.finRepo == nil {
		return
	}
	if _, err := w.runner().Run(context.Background(), "monthly-settlement"); err != nil && !errors.Is(err, jobrunner.ErrBusy) && !errors.Is(err, jobrunner.ErrStopped) {
		log.Print("[WARN] Monthly settlement sweep failed")
	}
}

func (w *BackgroundWorker) monthlySettlement(ctx context.Context) (jobrunner.Result, error) {
	if w.finRepo == nil {
		return jobrunner.Result{}, errors.New("finance repository unavailable")
	}
	_, err := w.finRepo.RunMonthlySettlement(ctx, time.Now(), "background-worker")
	if errors.Is(err, repository.ErrSettlementNotConfigured) {
		return jobrunner.Result{Disabled: true}, nil
	}
	// The repository returns a cumulative cycle snapshot, not this attempt's
	// count. Keep financial totals in its existing settlement panel; never sum
	// the same cumulative count on every scheduler tick.
	return jobrunner.Result{}, err
}

// refundReconciliation is the FIN-013 production caller for
// ReconcileOrderRefunds (audit §2.2): the unknown-outcome work queue
// finally drains on a schedule instead of only in its test. The batch
// is bounded (25 obligations/run) and the provider call happens
// outside any SQL transaction (ProcessOrderRefund's own contract), so
// a provider timeout stalls at most this tick. A nil provider (no PSP
// adapter until FIN-010) returns 0 completed and no error — the tick
// still records last-success, so a stalled adapter is visible as a
// growing backlog, not as a failing job. Retries are counted, never
// hidden: a failed obligation stays in the queue for the next tick.
func (w *BackgroundWorker) refundReconciliation(ctx context.Context) (jobrunner.Result, error) {
	if w.orderRepo == nil {
		return jobrunner.Result{}, errors.New("order repository unavailable")
	}
	started := time.Now()
	completed, err := w.orderRepo.ReconcileOrderRefunds(ctx, w.refundProvider, 25)
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	observability.LogOperation(ctx, "worker", "refund-reconciliation", outcome, time.Since(started))
	if err != nil {
		return jobrunner.Result{Affected: int64(completed)}, err
	}
	return jobrunner.Result{Affected: int64(completed)}, nil
}
