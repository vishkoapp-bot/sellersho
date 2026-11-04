-- Migration 202610160007: finance admin settings completeness (Group C audit).
--
-- 1) product_fee_overrides JSONB: makes the 3-tier platform-fee formula
--    (product override -> supplier override -> global base F) fully persistent.
--    The pricing engine already reads ProductFeeOverrides, but no column or
--    API existed, so product overrides were local-only fantasy in the admin UI.
-- 2) supplier_promotions table: persists supplier incentive campaigns created
--    in the commission tab (previously local-only state that vanished on
--    reload and affected nothing).

ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS product_fee_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS supplier_promotions (
    id VARCHAR(50) PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    supplier_id VARCHAR(50) NOT NULL DEFAULT 'all',
    discount_type VARCHAR(40) NOT NULL CHECK (discount_type IN ('platform_fee_discount', 'warehouse_free_storage')),
    discount_value BIGINT NOT NULL CHECK (discount_value >= 0),
    valid_until VARCHAR(20) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_promotions_status ON supplier_promotions(status);
