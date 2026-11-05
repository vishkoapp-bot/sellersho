-- 202611050003: a customer return's destination can be a supplier warehouse.
--
-- Owner rule (Oct 28, 2026): a return goes back to where the order
-- shipped FROM — the supplier's own warehouse when the parcel shipped
-- from there, the central warehouse otherwise. Until now
-- customer_returns.warehouse_id FK'd only to the central warehouses
-- table, so a supplier-warehouse destination (supplier_warehouses,
-- server-minted ids 'swh-…') was inexpressible and the destination was
-- free text the customer typed into the return form.
--
-- The destination is now DERIVED server-side at return creation from
-- the persisted allocation (order_item_origins, migrations 202610240001
-- + 202610280001): central destinations keep using warehouse_id;
-- supplier destinations use this new column. A non-NULL value here is
-- exactly what makes a return's destination_type 'supplier'. All
-- pre-existing rows keep NULL: their destination stays whatever
-- warehouse_id already says (central / unrecorded), unchanged.
ALTER TABLE customer_returns
    ADD COLUMN IF NOT EXISTS destination_supplier_warehouse_id TEXT
        REFERENCES supplier_warehouses(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_customer_returns_dest_supplier_warehouse
    ON customer_returns (destination_supplier_warehouse_id);
