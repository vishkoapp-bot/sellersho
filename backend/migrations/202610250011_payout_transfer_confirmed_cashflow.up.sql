-- Migration 202610250011: count manually-confirmed payouts as money-out.
--
-- The 20261017 honesty migration counts only status 'completed' as money
-- that left the platform — the Paya-batch terminal state. But the manual
-- bank-transfer flow (20261019 fix) ends in 'transfer_confirmed'
-- (finance_repo.go ConfirmManualTransfer: journal posted, money moved),
-- and NO code path ever transitions transfer_confirmed -> completed.
-- So every manual transfer was invisible to cash-out reporting:
-- allTimeOut stayed 0 after a real 2,500,000 Toman transfer, and
-- owedToStores/owedToSuppliers kept counting already-paid money.
-- safeToSpend was accidentally correct (the two errors cancel exactly)
-- except when the proMax64(0, ...) clamp binds (over-payout), where it
-- IS overstated.
--
-- Fix (Option A, status-IN-list): both 'completed' (Paya) and
-- 'transfer_confirmed' (manual) mean the money actually left. The
-- in-flight buckets ('approved','processing') are untouched —
-- transfer_confirmed lands in `transferred`, never in both.
-- No transition is added: transfer_confirmed stays the honest terminal
-- state of the manual flow, and the journal remains the source of truth
-- (posted in the same tx as the status flip).

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
    WHERE status IN ('completed', 'transfer_confirmed')
    GROUP BY 1
)
SELECT d.day,
       COALESCE(i.money_in, 0)::bigint  AS money_in,
       COALESCE(o.money_out, 0)::bigint AS money_out
FROM days d
LEFT JOIN ins  i ON i.day = d.day
LEFT JOIN outs o ON o.day = d.day;

-- Per-supplier cash actually transferred (completed + transfer_confirmed).
CREATE OR REPLACE VIEW v_fin_supplier_paid AS
SELECT supplier_id,
       COALESCE(SUM(amount), 0)::bigint AS paid
FROM payout_requests
WHERE recipient_type = 'supplier'
  AND status IN ('completed', 'transfer_confirmed')
  AND supplier_id IS NOT NULL
GROUP BY 1;
