-- Per-variant reference / supplier cost (Cluster A, 2026-11-01; Andrew
-- ruling, system-wide: a variant's purchase cost from the supplier may
-- differ from the product's, e.g. black costs more than white).
--
-- COLUMN NAME NOTE: staging3/migrations/
-- 202610280004_variant_supplier_base_cost.up.sql already defines this
-- exact column (product_variants.supplier_base_cost, BIGINT, NULL =
-- inherit products.supplier_base_cost) with the same >0 CHECK. This
-- root migration is the deploy-set copy of that column, written
-- IDEMPOTENTLY (ADD COLUMN IF NOT EXISTS + constraint only if absent)
-- so applying it after staging3 is a no-op, and applying it alone
-- (a root-only deploy set) still creates the column. DEPLOY ORDER
-- CAVEAT: staging3 must be applied BEFORE this file — the staging3
-- file has no IF NOT EXISTS guard, so applying it after this file
-- would fail on the already-existing column/constraint.
-- NULL = unset (variant inherits the product reference exactly as
-- before); a set value must be > 0 (the pre-existing CHECK). Cost 0
-- is therefore rejected at the API layer (400), not stored.
ALTER TABLE product_variants
    ADD COLUMN IF NOT EXISTS supplier_base_cost BIGINT;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'product_variants_supplier_base_cost_positive'
    ) THEN
        ALTER TABLE product_variants
            ADD CONSTRAINT product_variants_supplier_base_cost_positive
            CHECK (supplier_base_cost IS NULL OR supplier_base_cost > 0);
    END IF;
END $$;
COMMENT ON COLUMN product_variants.supplier_base_cost IS
    'Supplier cost C for this variant, Toman (integer). NULL = inherit products.supplier_base_cost (product reference). Set only by the variant cost endpoint (supplier-own products).';
