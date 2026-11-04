-- O3 LIVE PROPOSAL (deploy-time only — DO NOT run from staging work, and
-- NEVER run without a full database backup first).
--
-- What it does:
--   Backfills return_window_ends_at for pre-existing delivered_confirmed
--   orders that have no window, using confirmed_at + the platform's
--   return_window_days setting. Fix69's stage-1 auto-confirm stamps this
--   window for new confirmations, but old rows confirmed before that fix
--   lack it, and stage 2 (ReleaseClosedReturnWindows) only releases orders
--   with a non-NULL window — so NULL-window rows could never release.
--   This proposal stamps the window only; it does NOT run any release
--   sweep and does NOT change profit states (profit_released,
--   profit_released_at and referral flags are untouched).
--
-- Why it is safe:
--   * Guarded: only status='delivered_confirmed' AND
--     return_window_ends_at IS NULL AND confirmed_at IS NOT NULL.
--   * The window length is read from platform_settings at run time
--     (COALESCE(return_window_days, 7), clamped to 1..365), mirroring
--     Fix69 exactly (order_repo returnWindowDaysFromTx /
--     GetReturnWindowDays and worker scheduler stage 1) — it is NOT a
--     hardcoded 7 unless the setting row/column is missing or out of range.
--   * Idempotent: re-running it affects 0 rows, because backfilled rows no
--     longer have a NULL window.
--   * Rows with confirmed_at IS NULL are deliberately LEFT UNTOUCHED:
--     no timestamp is invented for them. They must be resolved separately
--     (see the residual count step below) and are not released by this.
--
-- Before running on live, re-count first (staging had 0 such rows, so the
-- live counts must be measured on live, not copied from staging):
--   SELECT COUNT(*) FROM orders WHERE status='delivered_confirmed';
--   SELECT COUNT(*) FROM orders WHERE status='delivered_confirmed'
--     AND return_window_ends_at IS NULL;
--   SELECT COUNT(*) FROM orders WHERE status='delivered_confirmed'
--     AND return_window_ends_at IS NULL AND confirmed_at IS NOT NULL;
--   SELECT COUNT(*) FROM orders WHERE status='delivered_confirmed'
--     AND return_window_ends_at IS NULL AND confirmed_at IS NULL;
--     -- residuals: leave untouched, report separately.
--
-- Staging result (for reference only): 0 rows backfilled — staging had
-- 8 delivered_confirmed orders, all 8 already with a window, 0 with a
-- NULL window, hence 0 NULL-confirmed_at residuals in the NULL-window set.

UPDATE orders
SET return_window_ends_at = confirmed_at +
  ((SELECT CASE
      WHEN COALESCE(return_window_days, 7) BETWEEN 1 AND 365
      THEN COALESCE(return_window_days, 7)
      ELSE 7
    END
    FROM platform_settings ORDER BY id LIMIT 1) * INTERVAL '1 day')
WHERE status = 'delivered_confirmed'
  AND return_window_ends_at IS NULL
  AND confirmed_at IS NOT NULL;
