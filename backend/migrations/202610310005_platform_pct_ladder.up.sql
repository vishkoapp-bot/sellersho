-- Pricing SWITCH (Andrew decisions, 2026-10-31): the platform percentage
-- is NOT global-only anymore. Effective platform % ladder:
--   per-variant (product) % > per-category % > global platform_pct.
-- There is no separate variant entity in this working copy: each product
-- row IS a variant (one supplier_base_cost per product), so the variant
-- level is stored per product_id. Categories are identified by the
-- product's category_slug string (there is no categories table here).
--
-- ADDITIVE ONLY: two new tables. Nothing existing is altered, so a
-- database without this migration still runs the old binary paths; the
-- new resolver degrades to the global platform_pct when these tables
-- are absent (see GetPlatformPct in pricing_system_repo.go), it does
-- not crash. Full ladder behaviour requires this migration.
-- Legacy absolute fee overrides (platform_settings.base_fee_f,
-- supplier_fee_overrides, product_fee_overrides) are NOT touched here:
-- their columns/data stay intact but nothing in the price path reads
-- them after the switch (see pricing.go deprecation notes).

CREATE TABLE IF NOT EXISTS platform_pct_products (
    product_id TEXT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
    pct DOUBLE PRECISION NOT NULL CHECK (pct >= 0 AND pct <= 1000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS platform_pct_categories (
    category_slug TEXT PRIMARY KEY,
    pct DOUBLE PRECISION NOT NULL CHECK (pct >= 0 AND pct <= 1000),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
