-- 202611130003: per-store variant visibility (P2(a)).
-- Absence of a row = visible (backward compatible, no backfill).
-- A row with is_visible=false hides the variant in that store only.
-- Rows with is_visible=true are equivalent to absence; writers DELETE
-- the row instead of storing true (table stays sparse).
-- No platform_pricing_settings change (standing rule).
-- Down: DROP TABLE (no guard needed: visibility is display state;
-- hiding rows have no ledger references. Dropping the table restores
-- all-visible, which is the pre-feature state.)
CREATE TABLE IF NOT EXISTS store_variant_visibility (
    store_id   VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    is_visible BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (store_id, product_id, variant_id)
);
CREATE INDEX IF NOT EXISTS idx_store_variant_visibility_store_product
    ON store_variant_visibility (store_id, product_id);
