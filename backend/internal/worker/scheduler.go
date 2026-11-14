package worker

import (
	"context"
	"database/sql"
	"log"

	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
	"time"
)

// BackgroundWorker runs scheduled cron tasks in Go.
//
// Live-parity notes (Team 09, MASTER-AUDIT D34/D43/D51/D73):
//
//   - Cadence is exactly the live one: the delivery-lock-releaser runs
//     once on startup and then every 60 seconds. The startup message
//     below reproduces the live startup message verbatim, including
//     its "search rollup hourly" phrase.
//   - Profit release is STAGED in live, unlike the old source worker
//     which released profit in the same UPDATE that confirmed delivery.
//     Stage 1 (confirmed) only moves an order to delivered_confirmed
//     after 20 days from shipped_at, and stamps confirmed_at plus
//     return_window_ends_at (Fix69 O3) so stage 2 can release it.
//     Stage 2 (released) only flips
//     profit_released once the return window has closed
//     (return_window_ends_at <= NOW()). The two counters are recorded
//     on every cycle in job_runs (migration 202610310008), with
//     job_id = "delivery-lock-releaser".
//   - TODO(U19/U26): orders in delivered_confirmed whose
//     return_window_ends_at is NULL are deliberately NOT released by
//     stage 2. Live has delivered_confirmed orders with a NULL window
//     that are already released, so the NULL-window rule is
//     unverified; no fallback (for example confirmed_at + 7 days)
//     is guessed here.
//   - The ledger pair for a release (seller/credit + platform/debit,
//     agent system:delivery-lock-releaser, D34) is NOT written by this
//     worker. Ledger writes belong to the finance/order teams; this
//     worker only flips the profit flags and records the job run.
//   - TODO(U73): the hourly search rollup destination is unverified.
//     runSearchRollup is therefore a documented no-op: the hourly
//     tick exists so the cadence matches live, but it writes nowhere
//     until U73 is resolved. Do NOT assume search_term_daily is the
//     hourly destination: the source migration comment for
//     search_queries names search_term_daily as the archival target
//     for rows older than 90 days and references a
//     repository.RollupSearchQueries that does not exist in source
//     Go, while live search_term_daily holds 0 rows and live
//     job_runs holds no rollup job.
//   - Monthly settlement (Team Settlement, Phase 3, on top of
//     live-parity): the AUTOMATIC sweep is now wired here, in
//     worker/settlement.go (RunMonthlySettlementIfDue), on the
//     same 60s ticker and once on startup. It is NOT live
//     behaviour (MASTER-AUDIT section 3b / U02: live had only
//     the manual trigger); it is the user-approved automatic
//     layer. It stays inert while settlement_day is unset,
//     runs only on the admin-configured settlement_day, and
//     sweeps at most once per month (cycle period is the primary
//     key). The settlement tables themselves already exist via
//     migration 202611020002; no new migration was added.
//   - The view-recorder and search-log writers (D51, D43) are NOT in
//     this worker: their tables already exist via migrations
//     202610230002 (store_view_daily / product_view_daily /
//     view_dedupe_daily) and 202610220003 + 202610310009
//     (search_queries / catalog_result_count). The HTTP recorder
//     handlers belong to the API/catalog teams.
//   - Redis key patterns are untouched by this file (D73): otp:*,
//     catalog:*, jwt:revoked:* and sms:bulk:* are neither renamed
//     nor flushed anywhere in this change.
type BackgroundWorker struct {
	db          *sql.DB
	stopCh      chan struct{}
	orderRepo   *repository.OrderRepository
	finRepo     *repository.FinanceRepository
	redisClient *repository.RedisClient
}

func (w *BackgroundWorker) SetRedisClient(r *repository.RedisClient) { w.redisClient = r }

func NewBackgroundWorker(db *sql.DB) *BackgroundWorker {
	return &BackgroundWorker{
		db:     db,
		stopCh: make(chan struct{}),
	}
}

// Start begins periodic background audit loops
// SetOrderRepository wires the staged-release point (Team 03, D34):
// when set, stage 2 calls OrderRepository.ReleaseClosedReturnWindows,
// which flips the flags AND writes the seller/credit + platform/debit
// ledger pair. Without it, stage 2 falls back to the flag-only UPDATE
// (Team 09 behaviour, used by tests that construct the worker bare).
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

func (w *BackgroundWorker) Start() {
	ticker := time.NewTicker(60 * time.Second)
	rollupTicker := time.NewTicker(time.Hour)
	operationsTicker := time.NewTicker(2 * time.Minute)
	go func() {
		log.Println("[INFO] Background Worker started (delivery-lock-releaser every 60s, search rollup hourly)")
		// Run once on startup
		w.runDeliveryLockReleaserCycle()
		w.RunMonthlySettlementIfDue()
		w.RunOperationsCycle()

		for {
			select {
			case <-ticker.C:
				w.runDeliveryLockReleaserCycle()
				w.RunMonthlySettlementIfDue()
			case <-rollupTicker.C:
				// TODO(U73): hourly search rollup; see runSearchRollup.
				w.runSearchRollup()
			case <-operationsTicker.C:
				w.RunOperationsCycle()
			case <-w.stopCh:
				ticker.Stop()
				rollupTicker.Stop()
				operationsTicker.Stop()
				return
			}
		}
	}()
}

// Stop gracefully shuts down background worker
func (w *BackgroundWorker) Stop() {
	close(w.stopCh)
}

// ReleaseExpiredDeliveryLocks runs one delivery-lock-releaser cycle.
//
// Kept with its original name and signature for existing callers.
// It now performs the live staged cycle (confirm, then release after
// the return window closes) and records the cycle in job_runs, instead
// of the old single-stage confirm-and-release in one UPDATE.
func (w *BackgroundWorker) ReleaseExpiredDeliveryLocks() {
	w.runDeliveryLockReleaserCycle()
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
	ctx, cancel := context.WithTimeout(observability.NewContext(context.Background()), 30*time.Second)
	defer cancel()

	// Stage 1 (confirmed): 20-day auto-confirm. This step changes
	// ONLY the status plus the confirmation timestamps; profit stays
	// locked until stage 2. It does not set profit_released,
	// profit_released_at or referral_commission_released (those move
	// with the release).
	// Fix69 O3: confirmed_at and return_window_ends_at MUST be set
	// here — stage 2 (ReleaseClosedReturnWindows) only releases
	// orders with a non-NULL window, so an auto-confirm that set
	// only the status could never release. The window length is
	// the same source the manual confirm path uses:
	// platform_settings.return_window_days (COALESCE 7, clamped
	// 1..365, same as order_repo returnWindowDaysFromTx /
	// GetReturnWindowDays), read via the repo when wired and by
	// the identical query otherwise — never a hardcoded 7 here
	// when the setting row exists.
	windowDays := 7
	if w.orderRepo != nil {
		windowDays = w.orderRepo.GetReturnWindowDays(ctx)
	} else {
		var days int
		if err := w.db.QueryRowContext(ctx, `SELECT COALESCE(return_window_days, 7) FROM platform_settings ORDER BY id LIMIT 1`).Scan(&days); err == nil && days >= 1 && days <= 365 {
			windowDays = days
		}
	}
	confirmRows, confirmErr := w.db.QueryContext(ctx, `SELECT id FROM orders WHERE status IN ('shipped','delivered_unconfirmed') AND shipped_at IS NOT NULL AND shipped_at<=NOW()-INTERVAL '20 days' AND profit_released=FALSE ORDER BY id`)
	if confirmErr != nil {
		w.recordJobRun(ctx, started, 0, 0, confirmErr)
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
	for _, id := range candidateIDs {
		tx, err := w.db.BeginTx(ctx, nil)
		if err != nil {
			return 0, int64(len(confirmedOrderIDs)), err
		}
		if err := repository.LockOrderAccountsTx(ctx, tx, id); err != nil {
			tx.Rollback()
			return 0, int64(len(confirmedOrderIDs)), err
		}
		result, err := tx.ExecContext(ctx, `UPDATE orders SET status='delivered_confirmed',confirmed_at=NOW(),return_window_ends_at=NOW()+($1*INTERVAL '1 day'),updated_at=NOW() WHERE id=$2 AND status IN ('shipped','delivered_unconfirmed') AND shipped_at IS NOT NULL AND shipped_at<=NOW()-INTERVAL '20 days' AND profit_released=FALSE`, windowDays, id)
		if err != nil {
			tx.Rollback()
			return 0, int64(len(confirmedOrderIDs)), err
		}
		n, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			return 0, int64(len(confirmedOrderIDs)), err
		}
		if err := tx.Commit(); err != nil {
			return 0, int64(len(confirmedOrderIDs)), err
		}
		if n == 1 {
			confirmedOrderIDs = append(confirmedOrderIDs, id)
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
		w.recordJobRun(ctx, started, 0, confirmed, releaseErr)
		return 0, confirmed, releaseErr
	}

	if released > 0 || confirmed > 0 {
		log.Printf("[INFO] Background Worker delivery-lock-releaser: confirmed %d, released %d", confirmed, released)
	}
	w.recordJobRun(ctx, started, released, confirmed, nil)
	return released, confirmed, nil
}

// recordJobRun inserts one row into job_runs. The table is defined
// by migration 202610310008; the CREATE TABLE IF NOT EXISTS here is
// belt-and-braces with the identical shape, so a worker started
// against a database whose migration directory was missing (the
// runner skips silently in that case) does not fail the cycle.
// A failed insert is logged, never fatal to the worker loop.
func (w *BackgroundWorker) recordJobRun(ctx context.Context, started time.Time, released, confirmed int64, runErr error) {
	outcome := "success"
	if runErr != nil {
		outcome = "error"
	}
	observability.LogOperation(ctx, "worker", "settlement", outcome, time.Since(started))
	if w.db == nil {
		return
	}
	_, err := w.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS job_runs (
		  id BIGSERIAL PRIMARY KEY,
		  job_id TEXT NOT NULL,
		  started_at TIMESTAMPTZ NOT NULL,
		  released BIGINT NOT NULL DEFAULT 0,
		  confirmed BIGINT NOT NULL DEFAULT 0,
		  duration_ms BIGINT NOT NULL DEFAULT 0,
		  error TEXT NOT NULL DEFAULT '',
		  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		log.Printf("[WARN] Background Worker failed to ensure job_runs table: %v", err)
		return
	}
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	_, err = w.db.ExecContext(ctx, `
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
