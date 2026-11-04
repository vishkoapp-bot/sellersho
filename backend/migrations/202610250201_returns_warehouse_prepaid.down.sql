-- Down migration for 202610250201 (returns warehouse + prepaid tracking).
-- Drops only the four columns this migration added.

ALTER TABLE customer_returns
  DROP COLUMN IF EXISTS shipped_at,
  DROP COLUMN IF EXISTS tracking_number,
  DROP COLUMN IF EXISTS prepaid,
  DROP COLUMN IF EXISTS warehouse_id;
