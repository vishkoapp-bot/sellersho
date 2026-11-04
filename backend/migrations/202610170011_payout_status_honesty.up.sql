-- Migration 202610170009: honest payout status flow in treasury views.
--
-- BUGFIX (W2 [MEDIUM] "Approved counted as transferred"): payout_requests in
-- status 'approved' or 'processing' were counted as money-out / paid, but the
-- money has NOT left the platform at that point. The real status flow is:
--   pending -> approved -> processing -> completed   ('rejected' is terminal)
-- Only 'completed' means the Paya transfer actually happened.
--
--  * v_fin_daily_cashflow: money_out counts only 'completed' payouts
--    (cash that really left). Approved/in-flight amounts stay visible as
--    liabilities in the treasury cash-position endpoint instead of being
--    silently booked as spent.
--  * v_fin_supplier_paid: paid counts only 'completed' payouts, so the
--    supplier aging report no longer treats approved-but-unsent payouts as
--    settled.

CREATE OR REPLACE VIEW v_fin_daily_cashflow AS
WITH days AS (
    SELECT (CURRENT_DATE - s)::date AS day FROM generate_series(0, 90) s
),
ins AS (
    SELECT date_trunc('day', created_at)::date AS day,
           COALESCE(SUM(payable_amount), 0)::bigint AS money_in
    FROM orders
    WHERE payment_status = 'paid'
    GROUP BY 1
),
outs AS (
    SELECT COALESCE(date_trunc('day', processed_at), date_trunc('day', created_at))::date AS day,
           COALESCE(SUM(amount), 0)::bigint AS money_out
    FROM payout_requests
    WHERE status = 'completed'
    GROUP BY 1
)
SELECT d.day,
       COALESCE(i.money_in, 0)::bigint  AS money_in,
       COALESCE(o.money_out, 0)::bigint AS money_out
FROM days d
LEFT JOIN ins  i ON i.day = d.day
LEFT JOIN outs o ON o.day = d.day;

-- Per-supplier cash actually transferred (completed payouts only).
CREATE OR REPLACE VIEW v_fin_supplier_paid AS
SELECT supplier_id,
       COALESCE(SUM(amount), 0)::bigint AS paid
FROM payout_requests
WHERE recipient_type = 'supplier'
  AND status = 'completed'
  AND supplier_id IS NOT NULL
GROUP BY 1;
