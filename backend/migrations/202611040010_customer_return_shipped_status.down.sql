-- 202611040010 down: restore the pre-Fix69 customer_returns CHECK.
-- Fails if any row currently sits in 'shipped'; move such rows back
-- to 'approved' before rolling back.
ALTER TABLE customer_returns DROP CONSTRAINT IF EXISTS customer_returns_status_check;
ALTER TABLE customer_returns ADD CONSTRAINT customer_returns_status_check
  CHECK (status IN ('requested', 'approved', 'rejected', 'received', 'refunded', 'completed', 'cancelled'));
