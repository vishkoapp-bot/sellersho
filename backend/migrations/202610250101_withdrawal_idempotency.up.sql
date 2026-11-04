-- 202610250101_withdrawal_idempotency.up.sql
-- MONEY-FIX W3 (2026-10-25): withdrawals have no idempotency protection —
-- a double-click / retried POST created two pending withdrawals (two holds).
-- A client idempotency key (Idempotency-Key header, or body idempotencyKey)
-- is stored per request and unique per customer: replaying the same key
-- with the same amount (Toman) + destination (Sheba) returns the ORIGINAL
-- request; the same key with a different amount/destination is a 409.
-- Additive only; existing rows keep idempotency_key NULL (no key = legacy
-- behavior, unchanged).
ALTER TABLE customer_withdrawal_requests
    ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(128);

-- Partial unique index: NULL keys (legacy / key-less creates) never collide.
CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_withdrawals_idempotency_key
    ON customer_withdrawal_requests (customer_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
