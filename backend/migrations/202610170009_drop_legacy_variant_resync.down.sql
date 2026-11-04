-- Rollback for 202610170009_drop_legacy_variant_resync.
--
-- Restores the legacy variant->products.stock resync trigger exactly as
-- migration 202610160006 created it. WARNING: this reintroduces the trigger
-- fight with trg_inventory_balances_sync_stock over products.stock — the
-- canonical ledger (202610170001) must be considered before rolling back.

CREATE OR REPLACE FUNCTION fn_variants_resync_product_stock() RETURNS trigger AS $$
DECLARE
  pid VARCHAR(50);
BEGIN
  pid := COALESCE(NEW.product_id, OLD.product_id);
  UPDATE products
  SET stock = COALESCE((SELECT SUM(stock) FROM product_variants WHERE product_id = pid), 0),
      updated_at = NOW()
  WHERE id = pid;
  RETURN COALESCE(NEW, OLD);
END $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_variants_resync_product_stock ON product_variants;
CREATE TRIGGER trg_variants_resync_product_stock
  AFTER INSERT OR UPDATE OF stock, product_id OR DELETE ON product_variants
  FOR EACH ROW EXECUTE FUNCTION fn_variants_resync_product_stock();
