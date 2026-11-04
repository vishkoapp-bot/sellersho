package worker

import (
	"context"
	"errors"
	"log"
	"time"

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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cycle, err := w.finRepo.RunMonthlySettlement(ctx, time.Now(), "background-worker")
	if err != nil {
		if errors.Is(err, repository.ErrSettlementNotConfigured) {
			return
		}
		log.Printf("[WARN] Monthly settlement sweep failed: %v", err)
		return
	}
	if cycle != nil {
		log.Printf("[INFO] Monthly settlement completed for %s: %d stores swept, %d skipped (no Sheba), gross %d", cycle.Period, cycle.TotalStores, cycle.SkippedStores, cycle.TotalGross)
	}
}
