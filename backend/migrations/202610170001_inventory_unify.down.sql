-- Rollback for 202610170001_inventory_unify.
--
-- Drops the projection-maintenance trigger/function and the canonical read
-- view. Does NOT touch inventory_balances rows or products.stock values.
--
-- NOTE: after rollback, the Go helpers in
-- internal/repository/inventory_unify.go (syncProductStockTx, called from the
-- order, supplier and warehouse write paths) still maintain the projection
-- explicitly; only the automatic safety net for unwrapped writers is removed.

DROP VIEW IF EXISTS v_product_canonical_stock;
DROP TRIGGER IF EXISTS trg_inventory_balances_sync_stock ON inventory_balances;
DROP FUNCTION IF EXISTS sync_product_stock_projection();
-- Restore the original payment_status CHECK (without 'refund_pending').
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_check
    CHECK ((payment_status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'failed'::character varying, 'cancelled'::character varying])::text[]));
