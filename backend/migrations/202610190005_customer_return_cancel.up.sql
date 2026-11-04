-- 2026-10-19 (Team 3, customer flows): the customer return-cancel endpoint
-- (POST /api/v1/returns/:id/cancel, added with the Returns Fix Trio) sets
-- status='cancelled', but the status CHECK never admitted that value — every
-- cancel attempt failed with a 500 on prod. Admit 'cancelled'.
ALTER TABLE customer_returns DROP CONSTRAINT IF EXISTS customer_returns_status_check;
ALTER TABLE customer_returns ADD CONSTRAINT customer_returns_status_check
  CHECK (status IN ('requested', 'approved', 'rejected', 'received', 'refunded', 'completed', 'cancelled'));
