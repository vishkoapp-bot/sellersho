-- 202610250009_inventory_captured.down.sql
-- Reverts 202610250009: drops the inventory_captured flag.
-- WARNING: after this rollback CancelOrder returns to deriving "captured"
-- from payment_status='paid' (the phantom-stock defect) — re-apply the
-- up migration to restore the guard.
ALTER TABLE orders DROP COLUMN IF EXISTS inventory_captured;
