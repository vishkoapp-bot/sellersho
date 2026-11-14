-- Exact physical identities; unknown history remains unknown.
-- A physical origin cannot disappear while recorded lots still reference it.
ALTER TABLE inventory_balances DROP CONSTRAINT IF EXISTS inventory_balances_warehouse_id_fkey;
ALTER TABLE inventory_balances ADD CONSTRAINT inventory_balances_warehouse_id_fkey FOREIGN KEY(warehouse_id) REFERENCES warehouses(id) ON DELETE RESTRICT;
ALTER TABLE inventory_balances ADD COLUMN supplier_warehouse_id TEXT REFERENCES supplier_warehouses(id) ON DELETE RESTRICT;
ALTER TABLE warehouse_bins ADD COLUMN warehouse_id TEXT REFERENCES warehouses(id) ON DELETE RESTRICT;
ALTER TABLE shipments ADD COLUMN supplier_warehouse_id TEXT REFERENCES supplier_warehouses(id) ON DELETE RESTRICT;
ALTER TABLE shipments ADD COLUMN warehouse_id TEXT REFERENCES warehouses(id) ON DELETE RESTRICT;
ALTER TABLE inbound_shipments ADD COLUMN supplier_warehouse_id TEXT REFERENCES supplier_warehouses(id) ON DELETE RESTRICT;
ALTER TABLE putaway_tasks ADD COLUMN warehouse_id TEXT REFERENCES warehouses(id) ON DELETE RESTRICT;
-- Only immutable inbound evidence can establish an existing task destination.
UPDATE putaway_tasks pt SET warehouse_id=i.warehouse_id FROM inbound_shipments i WHERE pt.inbound_id=i.id AND i.warehouse_id IS NOT NULL;
-- Bin ownership is inferred only where every completed task has the same
-- explicit immutable inbound destination. Names and current flags are excluded.
WITH evidence AS (
 SELECT pt.bin_location,MIN(i.warehouse_id) AS warehouse_id
 FROM putaway_tasks pt LEFT JOIN inbound_shipments i ON i.id=pt.inbound_id
 WHERE pt.status='completed' AND pt.bin_location IS NOT NULL
 GROUP BY pt.bin_location HAVING COUNT(DISTINCT i.warehouse_id)=1 AND COUNT(*)=COUNT(i.warehouse_id)
)
UPDATE warehouse_bins b SET warehouse_id=e.warehouse_id FROM evidence e WHERE b.bin_code=e.bin_location;
UPDATE inventory_balances b SET warehouse_id=i.warehouse_id FROM inbound_shipments i
 WHERE b.warehouse_id IS NULL AND i.warehouse_id IS NOT NULL
 AND b.location_key IN ('inbound:'||i.id,'warehouse:'||i.id);
UPDATE inventory_balances b SET warehouse_id=wb.warehouse_id FROM warehouse_bins wb
 WHERE b.warehouse_id IS NULL AND wb.warehouse_id IS NOT NULL AND b.location_key='bin:'||wb.bin_code;
CREATE INDEX inventory_origin_warehouse_sellable ON inventory_balances(product_id,variant_id,warehouse_id,id) INCLUDE(quantity) WHERE status='sellable' AND warehouse_id IS NOT NULL;
CREATE INDEX inventory_origin_supplier_sellable ON inventory_balances(product_id,variant_id,supplier_id,supplier_warehouse_id,id) INCLUDE(quantity) WHERE status='sellable' AND supplier_warehouse_id IS NOT NULL;
-- A durable review queue, never populated by guesses from today's product flags/names.
CREATE VIEW inventory_origin_reconciliation AS
 SELECT id,product_id,variant_id,location_type,location_key,quantity FROM inventory_balances
 WHERE quantity>0 AND warehouse_id IS NULL AND supplier_warehouse_id IS NULL;
CREATE VIEW order_origin_reconciliation AS
 SELECT oi.id,oi.order_id,oi.product_id,oi.quantity,o.payment_status,o.status
 FROM order_items oi JOIN orders o ON o.id=oi.order_id
 WHERE NOT EXISTS(SELECT 1 FROM order_item_origins x WHERE x.order_item_id=oi.id)
 OR EXISTS(SELECT 1 FROM order_item_origins x WHERE x.order_item_id=oi.id AND
 (x.origin_type='legacy' OR (x.origin_type='warehouse' AND x.warehouse_id IS NULL) OR (x.origin_type='supplier' AND x.supplier_warehouse_id IS NULL)));
CREATE VIEW inbound_origin_reconciliation AS SELECT id,supplier_id,warehouse_id,supplier_warehouse_id,status FROM inbound_shipments WHERE warehouse_id IS NULL OR supplier_warehouse_id IS NULL;
CREATE VIEW carton_origin_reconciliation AS SELECT id,supplier_id,warehouse_id,supplier_warehouse_id,status FROM shipments WHERE warehouse_id IS NULL OR supplier_warehouse_id IS NULL;
-- New location keys carry identity. Existing lots are never relocated on a
-- product origin edit. Bin mappings must be explicitly assigned an immutable ID.
CREATE FUNCTION inventory_assign_origin() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.warehouse_id IS NULL AND NEW.supplier_warehouse_id IS NULL THEN
  IF NEW.location_type='supplier' AND NEW.location_key LIKE 'supplier-warehouse:%' THEN
   SELECT id INTO NEW.supplier_warehouse_id FROM supplier_warehouses WHERE id=substring(NEW.location_key FROM 20) AND supplier_id=NEW.supplier_id;
  ELSIF NEW.location_key LIKE 'warehouse:%' THEN
   SELECT id INTO NEW.warehouse_id FROM warehouses WHERE id=split_part(NEW.location_key,':',2);
   IF NEW.warehouse_id IS NULL THEN SELECT warehouse_id INTO NEW.warehouse_id FROM inbound_shipments WHERE id=substring(NEW.location_key FROM 11); END IF;
  ELSIF NEW.location_key LIKE 'supplier_warehouse:%' THEN
   SELECT id INTO NEW.supplier_warehouse_id FROM supplier_warehouses WHERE id=split_part(NEW.location_key,':',2) AND supplier_id=NEW.supplier_id;
  ELSIF NEW.location_key LIKE 'bin:%' THEN
   SELECT warehouse_id INTO NEW.warehouse_id FROM warehouse_bins WHERE bin_code=substring(NEW.location_key FROM 5);
  ELSIF NEW.location_key LIKE 'shipment:%' THEN
   SELECT warehouse_id INTO NEW.warehouse_id FROM shipments WHERE id::text=substring(NEW.location_key FROM 10);
  ELSIF NEW.location_key LIKE 'inbound:%' THEN
   SELECT warehouse_id INTO NEW.warehouse_id FROM inbound_shipments WHERE id=substring(NEW.location_key FROM 9);
  END IF;
 END IF;
 IF NEW.warehouse_id IS NOT NULL AND NEW.supplier_warehouse_id IS NOT NULL THEN RAISE EXCEPTION 'inventory lot has conflicting physical origins'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER inventory_assign_origin BEFORE INSERT ON inventory_balances FOR EACH ROW EXECUTE FUNCTION inventory_assign_origin();
