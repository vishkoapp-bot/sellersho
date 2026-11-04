-- Reverse 202610310011_variant_cost: drop the per-variant cost column.
-- NOTE: this also removes the column created by staging3 migration
-- 202610280004 (same column); a down migration on a tree where only
-- staging3 created it removes staging3's column too. Only run as part
-- of a full rollback of the variant-cost feature.
ALTER TABLE product_variants
    DROP CONSTRAINT IF EXISTS product_variants_supplier_base_cost_positive;
ALTER TABLE product_variants
    DROP COLUMN IF EXISTS supplier_base_cost;
