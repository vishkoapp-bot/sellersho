-- 202610170002_order_idempotency: checkout idempotency key for C1 (duplicate orders).
--
-- The frontend generates one UUID v4 per checkout attempt and sends it as
-- `idempotencyKey` in POST /api/v1/orders (also accepted as the
-- "Idempotency-Key" header). When a key repeats, the backend returns the
-- EXISTING order instead of creating a new one, so payment-callback failures
-- and user retries can never create duplicate orders.
--
-- Partial unique index: existing rows keep NULL and never collide; every
-- newly created order stores its key exactly once.

ALTER TABLE orders ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS orders_idempotency_key_uidx
    ON orders (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
