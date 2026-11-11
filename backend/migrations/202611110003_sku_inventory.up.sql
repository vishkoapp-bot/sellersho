-- Preserve historical product balances as explicitly unallocated (NULL SKU).
ALTER TABLE inventory_balances ADD COLUMN variant_id UUID REFERENCES product_variants(id) ON DELETE RESTRICT;
ALTER TABLE inventory_movements ADD COLUMN variant_id UUID REFERENCES product_variants(id) ON DELETE RESTRICT;
ALTER TABLE inventory_reservations ADD COLUMN variant_id UUID REFERENCES product_variants(id) ON DELETE RESTRICT;
ALTER TABLE supplier_return_requests ADD COLUMN variant_id UUID REFERENCES product_variants(id) ON DELETE RESTRICT;
-- PostgreSQL may truncate the generated constraint name; resolve by its columns.
DO $$ DECLARE c RECORD; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='inventory_balances'::regclass AND contype='u'
 AND pg_get_constraintdef(oid)='UNIQUE (product_id, owner_type, location_type, location_key, status)' LOOP
  EXECUTE format('ALTER TABLE inventory_balances DROP CONSTRAINT %I', c.conname);
 END LOOP;
END $$;
CREATE UNIQUE INDEX inventory_balances_unallocated_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL;
CREATE UNIQUE INDEX inventory_balances_sku_unique ON inventory_balances(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL;
CREATE INDEX inventory_balances_sku_sellable ON inventory_balances(variant_id,status);

CREATE TABLE order_inventory_captures (
 order_item_id TEXT PRIMARY KEY,
 order_id VARCHAR(50) NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
 product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
 variant_id UUID REFERENCES product_variants(id) ON DELETE RESTRICT,
 quantity INT NOT NULL CHECK(quantity>0),
 variant_quantity INT NOT NULL DEFAULT 0 CHECK(variant_quantity>=0),
 restored_quantity INT NOT NULL DEFAULT 0 CHECK(restored_quantity>=0 AND restored_quantity<=quantity),
 inventory_mode TEXT NOT NULL,
 allocations JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(allocations)='array'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX order_inventory_captures_order ON order_inventory_captures(order_id);
CREATE TABLE order_inventory_returns (
 return_item_id TEXT PRIMARY KEY,
 order_item_id TEXT NOT NULL REFERENCES order_inventory_captures(order_item_id) ON DELETE RESTRICT,
 quantity INT NOT NULL CHECK(quantity>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE shipment_items ADD COLUMN inventory_received_qty INT NOT NULL DEFAULT 0 CHECK(inventory_received_qty>=0);
ALTER TABLE putaway_tasks ADD COLUMN variant_id UUID REFERENCES product_variants(id) ON DELETE RESTRICT;
ALTER TABLE putaway_tasks ADD COLUMN inventory_source_location_key TEXT;

CREATE OR REPLACE FUNCTION sync_product_stock_projection() RETURNS trigger
LANGUAGE plpgsql AS $$ DECLARE pid TEXT; vid UUID; BEGIN
 pid := CASE WHEN TG_OP='DELETE' THEN OLD.product_id ELSE NEW.product_id END;
 vid := CASE WHEN TG_OP='DELETE' THEN OLD.variant_id ELSE NEW.variant_id END;
 IF vid IS NOT NULL THEN
  UPDATE product_variants SET stock=(SELECT COALESCE(SUM(quantity),0)::int FROM inventory_balances WHERE variant_id=vid AND status='sellable') WHERE id=vid AND inventory_mode='tracked';
 END IF;
 UPDATE products p SET stock=s.qty,updated_at=NOW()
 FROM (SELECT COALESCE(SUM(ib.quantity),0)::int qty FROM inventory_balances ib LEFT JOIN product_variants pv ON pv.id=ib.variant_id
       WHERE ib.product_id=pid AND ib.status='sellable'
       AND ((ib.variant_id IS NULL AND NOT EXISTS(SELECT 1 FROM products WHERE id=pid AND schema_version>=2)) OR pv.is_active)) s
 WHERE p.id=pid AND EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=pid);
 RETURN NULL;
END $$;

CREATE FUNCTION sync_sku_availability_projection() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 UPDATE products p SET stock=s.qty,updated_at=NOW()
 FROM (SELECT COALESCE(SUM(ib.quantity),0)::int qty FROM inventory_balances ib LEFT JOIN product_variants pv ON pv.id=ib.variant_id
       WHERE ib.product_id=NEW.product_id AND ib.status='sellable'
       AND ((ib.variant_id IS NULL AND NOT EXISTS(SELECT 1 FROM products WHERE id=NEW.product_id AND schema_version>=2)) OR pv.is_active)) s
 WHERE p.id=NEW.product_id AND EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=NEW.product_id);
 RETURN NULL; END $$;
CREATE TRIGGER trg_sku_availability_projection AFTER UPDATE OF is_active ON product_variants FOR EACH ROW EXECUTE FUNCTION sync_sku_availability_projection();
