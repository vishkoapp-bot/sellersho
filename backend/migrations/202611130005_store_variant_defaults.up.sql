-- 202611130005: per-store default variant (P2(c)).
-- One row per (store, product) = one default. Absence = fall back to
-- the product-level default. Central store never has rows (it uses the
-- product-level default); the API rejects central writes.
-- No platform_pricing_settings change (standing rule).
-- Down: DROP TABLE (no guard needed: dropping restores product-level
-- defaults everywhere.)
CREATE TABLE IF NOT EXISTS store_variant_defaults (
    store_id   VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (store_id, product_id)
);
CREATE INDEX IF NOT EXISTS idx_store_variant_defaults_variant
    ON store_variant_defaults (variant_id);
