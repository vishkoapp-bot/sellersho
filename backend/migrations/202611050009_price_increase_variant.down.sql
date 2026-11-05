-- 202611050009: revert the variant link on price-increase requests.
DROP INDEX IF EXISTS idx_supplier_price_increases_product_variant_status;
ALTER TABLE supplier_price_increases DROP COLUMN IF EXISTS variant_id;
