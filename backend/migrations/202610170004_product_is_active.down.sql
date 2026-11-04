-- Rollback for 202610170004_product_is_active.
--
-- Drops the visibility flag. After rollback every product row is publicly
-- listed again (the pre-migration behaviour).

DROP INDEX IF EXISTS idx_products_is_active;
ALTER TABLE products DROP COLUMN IF EXISTS is_active;
