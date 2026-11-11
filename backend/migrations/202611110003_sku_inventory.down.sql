-- Refuse to erase SKU accounting or guess an allocation on downgrade.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM inventory_balances WHERE variant_id IS NOT NULL) OR EXISTS(SELECT 1 FROM order_inventory_captures)
 OR EXISTS(SELECT 1 FROM inventory_movements WHERE variant_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM inventory_reservations WHERE variant_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM supplier_return_requests WHERE variant_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM putaway_tasks WHERE variant_id IS NOT NULL) THEN
  RAISE EXCEPTION 'SKU inventory is in use; reconcile and archive its accounting before downgrade';
 END IF;
END $$;
DROP TRIGGER trg_sku_availability_projection ON product_variants;
DROP FUNCTION sync_sku_availability_projection();
CREATE OR REPLACE FUNCTION sync_product_stock_projection() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE pid TEXT; BEGIN
 pid:=CASE WHEN TG_OP='DELETE' THEN OLD.product_id ELSE NEW.product_id END;
 UPDATE products p SET stock=s.qty,updated_at=NOW() FROM (SELECT COALESCE(SUM(quantity),0)::int qty FROM inventory_balances WHERE product_id=pid AND status='sellable') s
 WHERE p.id=pid AND EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=pid); RETURN NULL; END $$;
ALTER TABLE putaway_tasks DROP COLUMN variant_id;
ALTER TABLE putaway_tasks DROP COLUMN inventory_source_location_key;
ALTER TABLE shipment_items DROP COLUMN inventory_received_qty;
DROP TABLE order_inventory_returns;
DROP TABLE order_inventory_captures;
DROP INDEX inventory_balances_sku_sellable;
DROP INDEX inventory_balances_sku_unique;
DROP INDEX inventory_balances_unallocated_unique;
ALTER TABLE supplier_return_requests DROP COLUMN variant_id;
ALTER TABLE inventory_reservations DROP COLUMN variant_id;
ALTER TABLE inventory_movements DROP COLUMN variant_id;
ALTER TABLE inventory_balances DROP COLUMN variant_id;
ALTER TABLE inventory_balances ADD UNIQUE(product_id,owner_type,location_type,location_key,status);
