-- 202610190004_profit_lock_view (down) — restore the pre-fix view definition
-- verbatim, as created by migration 202610160012.

-- 4) Per-store dropshipper profit: locked vs releasable-right-now.
-- Locked      = paid, not released, and NOT yet eligible
--               (no customer confirmation AND not past the 20-day rule).
-- Releasable  = paid, not released, AND eligible
--               (confirmed OR past the 20-day rule — exactly the worker's rule).
CREATE OR REPLACE VIEW v_fin_store_profit_lock AS
SELECT o.store_id,
       SUM(CASE
             WHEN o.profit_released = FALSE
              AND o.confirmed_at IS NULL
              AND NOT (o.status IN ('shipped', 'delivered_unconfirmed')
                       AND o.shipped_at IS NOT NULL
                       AND o.shipped_at <= NOW() - INTERVAL '20 days')
             THEN o.dropshipper_profit ELSE 0
           END)::bigint AS locked,
       SUM(CASE
             WHEN o.profit_released = FALSE
              AND (o.confirmed_at IS NOT NULL
                   OR (o.status IN ('shipped', 'delivered_unconfirmed')
                       AND o.shipped_at IS NOT NULL
                       AND o.shipped_at <= NOW() - INTERVAL '20 days'))
             THEN o.dropshipper_profit ELSE 0
           END)::bigint AS releasable,
       COUNT(*) FILTER (
         WHERE o.profit_released = FALSE
           AND o.confirmed_at IS NULL
           AND NOT (o.status IN ('shipped', 'delivered_unconfirmed')
                    AND o.shipped_at IS NOT NULL
                    AND o.shipped_at <= NOW() - INTERVAL '20 days')
       )::int AS locked_count,
       COUNT(*) FILTER (
         WHERE o.profit_released = FALSE
           AND (o.confirmed_at IS NOT NULL
                OR (o.status IN ('shipped', 'delivered_unconfirmed')
                    AND o.shipped_at IS NOT NULL
                    AND o.shipped_at <= NOW() - INTERVAL '20 days'))
       )::int AS releasable_count
FROM orders o
WHERE o.payment_status = 'paid'
  AND o.status NOT IN ('cancelled')
  AND o.store_id IS NOT NULL
GROUP BY o.store_id;
