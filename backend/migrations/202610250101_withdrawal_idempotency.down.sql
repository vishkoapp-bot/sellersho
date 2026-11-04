-- 202610250101_withdrawal_idempotency.down.sql
-- MONEY-FIX W3 (2026-10-25): drop the withdrawal idempotency key.
DROP INDEX IF EXISTS idx_customer_withdrawals_idempotency_key;
ALTER TABLE customer_withdrawal_requests
    DROP COLUMN IF EXISTS idempotency_key;
