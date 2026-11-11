-- This batch must execute transactionally (as the migration runner does).
-- Downgrading a live V2 model must never erase identities or requested registry
-- definitions. Empty/legacy-only databases can still be rolled back normally.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM products WHERE schema_version<>0 OR product_type<>'legacy' OR attribute_schema IS NOT NULL)
 OR EXISTS(SELECT 1 FROM product_variants WHERE inventory_mode<>'legacy' OR combination_key IS NOT NULL OR selection<>'{}'::jsonb OR NOT is_active)
 OR EXISTS(SELECT 1 FROM product_draft_sku_reservations)
 OR EXISTS(SELECT 1 FROM local_product_attributes)
 OR EXISTS(SELECT 1 FROM local_product_attribute_values)
 OR EXISTS(SELECT 1 FROM category_local_product_attributes)
 OR EXISTS(SELECT 1 FROM category_local_product_attribute_values)
 OR EXISTS(SELECT 1 FROM product_attribute_option_extensions)
 OR EXISTS(SELECT 1 FROM category_product_attribute_option_extensions)
 OR EXISTS(SELECT 1 FROM product_canonical_attribute_values) THEN
  RAISE EXCEPTION 'Product model V2 is in use; reconcile and archive its identities and registry before downgrade';
 END IF;
END $$;

DROP TABLE IF EXISTS product_canonical_attribute_values;
DROP TABLE IF EXISTS product_draft_sku_reservations;
DROP TABLE IF EXISTS category_product_attribute_option_extensions;
DROP TABLE IF EXISTS product_attribute_option_extensions;
DROP TABLE IF EXISTS category_local_product_attribute_values;
DROP TABLE IF EXISTS category_local_product_attributes;
DROP TABLE IF EXISTS local_product_attribute_values;
DROP TABLE IF EXISTS local_product_attributes;
DROP INDEX IF EXISTS product_variants_combination_unique;
ALTER TABLE product_variants DROP CONSTRAINT IF EXISTS product_variants_selection_object_check;
ALTER TABLE product_variants DROP CONSTRAINT IF EXISTS product_variants_inventory_mode_check;
ALTER TABLE product_variants DROP COLUMN IF EXISTS selection, DROP COLUMN IF EXISTS combination_key, DROP COLUMN IF EXISTS inventory_mode, DROP COLUMN IF EXISTS is_active;
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_product_model_v2_check;
ALTER TABLE products DROP COLUMN IF EXISTS product_type, DROP COLUMN IF EXISTS schema_version, DROP COLUMN IF EXISTS attribute_schema;
