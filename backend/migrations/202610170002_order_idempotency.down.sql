-- Rollback for 202610170002_order_idempotency.
--
-- Drops the idempotency column/index. Does NOT touch order rows themselves.
-- Note: after rollback, checkout retries can create duplicate orders again
-- (the frontend still sends idempotencyKey, the backend simply ignores it).

DROP INDEX IF EXISTS orders_idempotency_key_uidx;
ALTER TABLE orders DROP COLUMN IF EXISTS idempotency_key;
