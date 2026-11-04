-- Reverse 202610310016_order_dispute_reason. Dispute reasons recorded
-- while the column existed are dropped with it; dispute statuses and
-- disputed_at timestamps are untouched.
ALTER TABLE orders
    DROP COLUMN IF EXISTS dispute_reason;
