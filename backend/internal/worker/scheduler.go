package worker

import (
	"context"
	"database/sql"
	"errors"
	"github.com/drup/platform/internal/jobrunner"
	"log"
	"sync"

	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
	"time"
)

// BackgroundWorker runs fixed independent bounded jobs. Search rollup remains an
// unscheduled compatibility no-op until its write destination is established.
type BackgroundWorker struct {
	db          *sql.DB
	engineOnce  sync.Once
	engine      *jobrunner.Runner
	orderRepo   *repository.OrderRepository
	finRepo     *repository.FinanceRepository
	redisClient *repository.RedisClient
}

func (w *BackgroundWorker) SetRedisClient(r *repository.RedisClient) { w.redisClient = r }

func NewBackgroundWorker(db *sql.DB) *BackgroundWorker {
	return &BackgroundWorker{
		db: db,
	}
}

// SetOrderRepository is startup wiring, snapshotted before loops start.
func (w *BackgroundWorker) SetOrderRepository(r *repository.OrderRepository) {
	w.orderRepo = r
}

// SetFinanceRepository wires the automatic monthly-settlement point
// (Team Settlement, Phase 3): when set, the 60s ticker also runs
// RunMonthlySettlementIfDue. Without it, the settlement tick is
// inert (used by tests that construct the worker bare).
func (w *BackgroundWorker) SetFinanceRepository(r *repository.FinanceRepository) {
	w.finRepo = r
}

func (w *BackgroundWorker) runner() *jobrunner.Runner {
	w.engineOnce.Do(func() {
		snapshot := &BackgroundWorker{db: w.db, orderRepo: w.orderRepo, finRepo: w.finRepo, redisClient: w.redisClient}
		w.engine = jobrunner.New(repository.JobStore{DB: w.db}, snapshot.jobs())
	})
	return w.engine
}
func (w *BackgroundWorker) Start() {
	if w.db != nil {
		w.runner().Start()
	}
}

// Stop cancels all business work and drains within the component's 5s budget.
func (w *BackgroundWorker) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.StopContext(ctx); err != nil {
		log.Printf("[WARN] background worker drain exceeded budget")
	}
}

// StopContext cancels admission and all business work within the caller budget.
func (w *BackgroundWorker) StopContext(ctx context.Context) error { return w.runner().Stop(ctx) }

// RunDeliveryJob shares admission/ownership with the scheduled delivery pass.
func (w *BackgroundWorker) RunDeliveryJob(ctx context.Context) error {
	_, err := w.runner().Run(ctx, "delivery-lock-releaser")
	return err
}

// ReleaseExpiredDeliveryLocks runs one delivery-lock-releaser cycle.
//
// Kept with its original name and signature for existing callers.
// It now performs the live staged cycle (confirm, then release after
// the return window closes) and records the cycle in job_runs, instead
// of the old single-stage confirm-and-release in one UPDATE.
func (w *BackgroundWorker) ReleaseExpiredDeliveryLocks() {
	if w.db == nil {
		return
	}
	_ = w.RunDeliveryJob(context.Background())
}

// runDeliveryLockReleaserCycle runs the two live stages and records
// one job_runs row (job_id = "delivery-lock-releaser") carrying the
// released and confirmed counters, the duration and any error.
// A failed stage still records a row so a silent stall is visible
// in job_runs instead of disappearing.
func (w *BackgroundWorker) runDeliveryLockReleaserCycle() (released int64, confirmed int64, err error) {
	if w.db == nil {
		return 0, 0, nil
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(observability.NewContext(context.Background()), 10*time.Second)
	defer cancel()
	released, confirmed, err = w.deliveryCycle(ctx)
	w.recordJobRun(ctx, started, released, confirmed, err)
	return
}

func (w *BackgroundWorker) deliveryCycle(ctx context.Context) (released int64, confirmed int64, err error) {
	// Stage 1 (confirmed): 20-day auto-confirm. This step changes
	// ONLY the status plus the confirmation timestamps; profit stays
	// locked until stage 2. It does not set profit_released,
	// profit_released_at or referral_commission_released (those move
	// with the release).
	confirmRows, confirmErr := w.db.QueryContext(ctx, `SELECT id FROM orders WHERE status IN ('shipped','delivered_unconfirmed') AND shipped_at IS NOT NULL AND shipped_at<=NOW()-INTERVAL '20 days' AND profit_released=FALSE AND NOT EXISTS(SELECT 1 FROM supplier_shipments s WHERE s.order_id=orders.id AND s.status='failed') AND NOT EXISTS(SELECT 1 FROM background_job_candidate_retry r WHERE r.job='delivery-confirm' AND r.candidate=orders.id AND r.next_retry_at>clock_timestamp()) ORDER BY shipped_at,id LIMIT 100`)
	if confirmErr != nil {
		return 0, 0, confirmErr
	}
	var candidateIDs []string
	for confirmRows.Next() {
		var id string
		if err := confirmRows.Scan(&id); err != nil {
			confirmRows.Close()
			return 0, 0, err
		}
		candidateIDs = append(candidateIDs, id)
	}
	confirmErr = confirmRows.Err()
	confirmRows.Close()
	if confirmErr != nil {
		return 0, 0, confirmErr
	}
	var confirmedOrderIDs []string
	var failures []error
	for _, id := range candidateIDs {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		candidateCtx, candidateCancel := context.WithTimeout(ctx, 300*time.Millisecond)
		ok, err := w.confirmDelivery(candidateCtx, id)
		candidateCancel()
		if ok {
			confirmedOrderIDs = append(confirmedOrderIDs, id)
		}
		if err != nil {
			failures = append(failures, err)
			if retryErr := repository.RecordJobCandidateRetry(ctx, w.db, "delivery-confirm", id); retryErr != nil {
				failures = append(failures, retryErr)
			}
		}
	}
	confirmed = int64(len(confirmedOrderIDs))
	// P1-3 (best-effort): 20-day auto-confirm — customer + store owner
	// + suppliers, per auto-confirmed order.
	for _, oid := range confirmedOrderIDs {
		repository.NotifyDeliveryConfirmed(ctx, w.db, oid, true)
	}

	// Stage 2 (released): return-window-close releaser. Only orders
	// whose recorded window has actually closed are released.
	// TODO(U19/U26): NULL-window orders are skipped here; see the
	// package comment. No fallback window is invented.
	var releaseErr error
	if w.orderRepo != nil {
		// Integration wiring (D34): the release point owns both the flag
		// flip and the ledger pair, guarded by profit_released = FALSE,
		// so it can never double-write.
		released, releaseErr = w.orderRepo.ReleaseClosedReturnWindows(ctx)
	} else {
		released, releaseErr = repository.NewOrderRepository(w.db).ReleaseClosedReturnWindows(ctx)
	}
	if releaseErr != nil {
		return released, confirmed, errors.Join(append(failures, releaseErr)...)
	}

	if released > 0 || confirmed > 0 {
		log.Printf("[INFO] Background Worker delivery-lock-releaser: confirmed %d, released %d", confirmed, released)
	}
	return released, confirmed, errors.Join(failures...)
}

func (w *BackgroundWorker) confirmDelivery(ctx context.Context, id string) (bool, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err == nil {
		defer tx.Rollback()
	}
	if err != nil {
		return false, err
	}
	if err := repository.LockOrderAccountsTx(ctx, tx, id); err != nil {
		tx.Rollback()
		return false, err
	}
	// Read the configured window in the same guarded transaction as confirmation.
	var configured int
	settingErr := tx.QueryRowContext(ctx, `SELECT COALESCE(return_window_days,7) FROM platform_settings ORDER BY id LIMIT 1`).Scan(&configured)
	if settingErr != nil && !errors.Is(settingErr, sql.ErrNoRows) {
		return false, settingErr
	}
	windowDays := 7
	if configured >= 1 && configured <= 365 {
		windowDays = configured
	}
	result, err := tx.ExecContext(ctx, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW(),return_window_ends_at=NOW()+($1*INTERVAL '1 day'),updated_at=NOW() WHERE id=$2 AND status IN ('shipped','delivered_unconfirmed') AND shipped_at IS NOT NULL AND shipped_at<=NOW()-INTERVAL '20 days' AND profit_released=FALSE AND NOT EXISTS(SELECT 1 FROM supplier_shipments s WHERE s.order_id=orders.id AND s.status='failed')`, windowDays, id)
	if err != nil {
		tx.Rollback()
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
}

// recordJobRun preserves the compatibility direct-cycle history with a fresh
// bounded metadata context. Scheduled runs are recorded atomically by JobStore.
func (w *BackgroundWorker) recordJobRun(ctx context.Context, started time.Time, released, confirmed int64, runErr error) {
	outcome := "success"
	if runErr != nil {
		outcome = "error"
	}
	// D-05: this direct cycle bypasses the runner, so it records its
	// own single observation — under its own job name
	// ("delivery-lock-releaser"), counted exactly once. The previous
	// "settlement" label was wrong twice over: it double-counted
	// monthly-settlement (which the runner already records) and
	// mislabelled a delivery-lock cycle as a settlement.
	observability.LogOperation(ctx, "worker", "delivery-lock-releaser", outcome, time.Since(started))
	if w.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	errText := ""
	if runErr != nil {
		errText = jobrunner.ErrorCode(runErr)
	}
	_, err := w.db.ExecContext(ctx, `
		INSERT INTO job_runs (job_id, started_at, released, confirmed, duration_ms, error)
		VALUES ('delivery-lock-releaser', $1, $2, $3, $4, $5)
	`, started, released, confirmed, time.Since(started).Milliseconds(), errText)
	if err != nil {
		log.Printf("[WARN] Background Worker failed to record job_runs row: %v", err)
	}
}

// runSearchRollup is the hourly search-rollup tick named in the live
// startup message.
//
// TODO(U73): the live hourly rollup destination is unverified, so
// this function intentionally writes nothing. Do not point it at
// search_term_daily (or any other table) until U73 is resolved;
// search_term_daily is 0 rows in live and no rollup job appears in
// live job_runs, so it is not established as the hourly destination.
func (w *BackgroundWorker) runSearchRollup() {
	// TODO(U73): implement the hourly search rollup once its
	// destination is confirmed from the merged tree. Until then,
	// this tick must not write to any table.
}
