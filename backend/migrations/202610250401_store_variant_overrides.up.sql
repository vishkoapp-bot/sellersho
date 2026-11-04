-- Per-variant seller margin overrides (complementary mission, Oct 25, 2026).
--
-- Variants (تنوع) carry no price of their own: one B (base cost) and one
-- price per product. A dropshipper may now set a different margin for one
-- variant of a product (e.g. the red one earns 25% while the blue one
-- earns the product-level 15%). Resolution tier (pricing.ResolvePricing):
-- variant override HIGHEST, then product override, then niche override,
-- then the store/platform default. A discount campaign still replaces the
-- resolved tier (same as it does for product overrides today).
--
-- One row per (store, product, variant). product_id is stored alongside
-- variant_id so a variant can never be read back under the wrong product
-- (a variant belongs to exactly one product; the FK on product_variants
-- already enforces that, this column makes the override self-describing
-- and lets the seller pricing endpoint group overrides per product).
CREATE TABLE IF NOT EXISTS store_variant_overrides (
    store_id       VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id     VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id     UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    override_value NUMERIC(12,2) NOT NULL,
    mode           VARCHAR(10) NOT NULL DEFAULT 'percent' CHECK (mode IN ('percent', 'fixed')),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (store_id, product_id, variant_id)
);
CREATE INDEX IF NOT EXISTS idx_store_variant_overrides_store_product
    ON store_variant_overrides (store_id, product_id);
