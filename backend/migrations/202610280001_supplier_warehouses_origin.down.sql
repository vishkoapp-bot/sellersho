-- Down for 202610280001: remove the origin/inbound columns, restore the
-- old inbound status CHECK, drop supplier_warehouses. No historical data
-- is lost: the new columns are references only; legs/origins/inbounds
-- keep their original columns untouched.
ALTER TABLE inbound_shipments DROP CONSTRAINT IF EXISTS inbound_shipments_status_check;
ALTER TABLE inbound_shipments ADD CONSTRAINT inbound_shipments_status_check
  CHECK (status IN ('in_transit', 'pending', 'receiving', 'received',
                    'putaway_completed', 'qc_rejected', 'completed'));

ALTER TABLE inbound_shipments
  DROP COLUMN IF EXISTS dispatched_at,
  DROP COLUMN IF EXISTS dispatch_at,
  DROP COLUMN IF EXISTS warehouse_id;
ALTER TABLE order_item_origins DROP COLUMN IF EXISTS supplier_warehouse_id;
ALTER TABLE order_shipping_legs DROP COLUMN IF EXISTS supplier_warehouse_id;
ALTER TABLE products DROP COLUMN IF EXISTS origin_supplier_warehouse_id;
DROP TABLE IF EXISTS supplier_warehouses;
