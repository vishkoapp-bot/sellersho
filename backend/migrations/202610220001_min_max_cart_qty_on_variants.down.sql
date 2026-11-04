-- Down for 202610220001 (per-variant cart quantity limits): drop the
-- two CHECK constraints and both columns from product_variants.
-- Supplier-configured limits are discarded by the rollback — that is
-- the point of rolling back the feature; no other table references
-- these columns.

ALTER TABLE product_variants
  DROP CONSTRAINT IF EXISTS product_variants_max_cart_qty_check,
  DROP CONSTRAINT IF EXISTS product_variants_min_cart_qty_check;

ALTER TABLE product_variants
  DROP COLUMN IF EXISTS max_cart_qty,
  DROP COLUMN IF EXISTS min_cart_qty;
