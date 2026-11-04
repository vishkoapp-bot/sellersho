-- 202611040020: per-product dropshipper margin ceilings (ISO-C margin model).
--
-- The user's margin model: a dropshipper sets a margin PER PRODUCT within
-- a platform-set ceiling FOR THAT PRODUCT. Until now only the global
-- max_profit_margin (35) existed. This table holds an optional per-product
-- ceiling; when a row exists it REPLACES the global ceiling for that
-- product (enforced at SET in SaveOwnedPricing / SetStoreVariantOverride
-- and at COMPUTE in capSellerMargin). Admin-managed via
-- PUT /api/v1/admin/products/<id>/pricing (maxMarginPct / clearMaxMargin).
-- Pattern mirrors the platform_pct_products ladder table (202610310005).
CREATE TABLE IF NOT EXISTS product_margin_ceilings (
    product_id TEXT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
    max_margin_pct DOUBLE PRECISION NOT NULL CHECK (max_margin_pct >= 0 AND max_margin_pct <= 1000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
