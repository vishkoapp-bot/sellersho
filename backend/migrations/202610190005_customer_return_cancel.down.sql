-- Rollback of 202610190005: remove 'cancelled' from the customer_returns
-- status CHECK (only safe when no 'cancelled' rows exist).
ALTER TABLE customer_returns DROP CONSTRAINT IF EXISTS customer_returns_status_check;
ALTER TABLE customer_returns ADD CONSTRAINT customer_returns_status_check
  CHECK (status IN ('requested', 'approved', 'rejected', 'received', 'refunded', 'completed'));
