-- 202610190004_profit_lock_view — fix staged-release semantics of v_fin_store_profit_lock.
--
-- The old definition (from 202610160012) used the retired 20-day auto-release
-- semantics: any order with confirmed_at set (even one still INSIDE its return
-- window) was labelled "releasable", though the ReleaseClosedReturnWindows
-- scheduler job would correctly refuse to release it. That made the finance
-- "locked vs releasable" figures dishonest.
--
-- Corrected classification, matching the staged-release engine:
--   Releasable  = profit_released = FALSE AND status = 'delivered_confirmed'
--                 AND return_window_ends_at IS NOT NULL
--                 AND return_window_ends_at <= NOW()
--   Locked      = everything else paid-but-unreleased (same row filter as
--                 before: payment_status='paid', not cancelled, store present).
-- Column list and FROM/JOINs are unchanged; only the CASE / FILTER logic moves.

CREATE OR REPLACE VIEW v_fin_store_profit_lock AS
SELECT o.store_id,
       SUM(CASE
             WHEN o.profit_released = FALSE
              AND NOT (o.status = 'delivered_confirmed'
                       AND o.return_window_ends_at IS NOT NULL
                       AND o.return_window_ends_at <= NOW())
             THEN o.dropshipper_profit ELSE 0
           END)::bigint AS locked,
       SUM(CASE
             WHEN o.profit_released = FALSE
              AND o.status = 'delivered_confirmed'
              AND o.return_window_ends_at IS NOT NULL
              AND o.return_window_ends_at <= NOW()
             THEN o.dropshipper_profit ELSE 0
           END)::bigint AS releasable,
       COUNT(*) FILTER (
         WHERE o.profit_released = FALSE
           AND NOT (o.status = 'delivered_confirmed'
                    AND o.return_window_ends_at IS NOT NULL
                    AND o.return_window_ends_at <= NOW())
       )::int AS locked_count,
       COUNT(*) FILTER (
         WHERE o.profit_released = FALSE
           AND o.status = 'delivered_confirmed'
           AND o.return_window_ends_at IS NOT NULL
           AND o.return_window_ends_at <= NOW()
       )::int AS releasable_count
FROM orders o
WHERE o.payment_status = 'paid'
  AND o.status NOT IN ('cancelled')
  AND o.store_id IS NOT NULL
GROUP BY o.store_id;
