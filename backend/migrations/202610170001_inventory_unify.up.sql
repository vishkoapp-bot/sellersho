-- 202610170001_inventory_unify: unify the two parallel inventory systems.
--
-- DECISION: inventory_balances is the canonical inventory ledger (quantities
-- per owner/location/status, reservations, movement audit trail).
-- products.stock is a maintained PROJECTION of
--   SUM(quantity) FROM inventory_balances WHERE status='sellable'
-- for products that have balance rows. Products with NO balance rows keep
-- products.stock as the direct authority (legacy path: supplier-entered
-- products that never moved through the warehouse).
--
-- This migration is purely additive and modifies NO existing data. The trigger
-- below only fires on FUTURE writes to inventory_balances, so legacy products
-- (no balance rows) are never touched by it.
--
-- Why a trigger in addition to the Go sync helpers
-- (internal/repository/inventory_unify.go :: syncProductStockTx):
-- warehouse receive/QC/putaway and supplier-return flows mutate balances in
-- many places; the trigger guarantees the projection invariant for every
-- present and future writer, including ones the Go helpers don't wrap.

CREATE OR REPLACE FUNCTION sync_product_stock_projection() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    pid TEXT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        pid := OLD.product_id;
    ELSE
        pid := NEW.product_id;
    END IF;
    UPDATE products p
    SET stock = s.sellable, updated_at = NOW()
    FROM (SELECT COALESCE(SUM(quantity), 0)::int AS sellable
          FROM inventory_balances
          WHERE product_id = pid AND status = 'sellable') s
    WHERE p.id = pid
      AND EXISTS (SELECT 1 FROM inventory_balances WHERE product_id = pid);
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_inventory_balances_sync_stock ON inventory_balances;

CREATE TRIGGER trg_inventory_balances_sync_stock
    AFTER INSERT OR UPDATE OR DELETE ON inventory_balances
    FOR EACH ROW
    EXECUTE FUNCTION sync_product_stock_projection();

-- Canonical read view: single place to compare the projection against the
-- ledger (also backs the admin reconciliation report if needed later).
CREATE OR REPLACE VIEW v_product_canonical_stock AS
SELECT p.id AS product_id,
       p.stock AS projected_stock,
       COALESCE(SUM(ib.quantity) FILTER (WHERE ib.status = 'sellable'), 0)::int AS canonical_sellable,
       COUNT(ib.id)::int AS balance_rows
FROM products p
LEFT JOIN inventory_balances ib ON ib.product_id = p.id
GROUP BY p.id, p.stock;

-- Companion fix (found while smoke-testing the unified cancel path):
-- CancelOrder marks paid-then-cancelled orders as payment_status='refund_pending'
-- (the refund is pending manual/PSP settlement), but the orders CHECK constraint
-- only allowed pending/paid/failed/cancelled, so EVERY paid-order cancellation
-- failed with a constraint violation and the stock-restore path never ran.
-- Widening the constraint is additive and changes no existing data.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_check
    CHECK ((payment_status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'failed'::character varying, 'cancelled'::character varying, 'refund_pending'::character varying])::text[]));
