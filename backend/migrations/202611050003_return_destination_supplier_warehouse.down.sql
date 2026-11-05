-- 202611050003: revert supplier-warehouse return destinations.
--
-- Dropping the column loses only the supplier-destination attribution
-- of returns created while it existed; those returns fall back to
-- their warehouse_id (central / unrecorded), the pre-feature shape.
DROP INDEX IF EXISTS idx_customer_returns_dest_supplier_warehouse;

ALTER TABLE customer_returns
    DROP COLUMN IF EXISTS destination_supplier_warehouse_id;
