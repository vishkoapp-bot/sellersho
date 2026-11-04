-- Down migration for 202610280004 (per-variant supplier cost).
-- The column is NULL-by-default and only ever written at proposal
-- finalize, so dropping it loses no money state: live variants fall
-- back to products.supplier_base_cost exactly as before the feature.
ALTER TABLE product_variants
    DROP CONSTRAINT IF EXISTS product_variants_supplier_base_cost_positive;
ALTER TABLE product_variants
    DROP COLUMN IF EXISTS supplier_base_cost;
