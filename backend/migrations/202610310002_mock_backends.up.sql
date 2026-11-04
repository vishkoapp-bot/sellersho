-- Backends for previously frontend-only / mock-only features (2026-10-31).
-- New tables and settings columns only; no existing table is altered in shape
-- beyond additive nullable columns on platform_settings.
CREATE TABLE IF NOT EXISTS customer_wishlists (
    user_id    TEXT NOT NULL,
    product_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, product_id)
);

CREATE TABLE IF NOT EXISTS customer_addresses (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    full_name   TEXT NOT NULL DEFAULT '',
    phone       TEXT NOT NULL DEFAULT '',
    province    TEXT NOT NULL DEFAULT '',
    city        TEXT NOT NULL DEFAULT '',
    address     TEXT NOT NULL DEFAULT '',
    postal_code TEXT NOT NULL DEFAULT '',
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_customer_addresses_user ON customer_addresses (user_id);

CREATE TABLE IF NOT EXISTS product_reviews (
    id         TEXT PRIMARY KEY,
    product_id TEXT NOT NULL,
    user_id    TEXT NOT NULL,
    rating     INT NOT NULL CHECK (rating >= 1 AND rating <= 5),
    comment    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'published',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (product_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_product_reviews_product ON product_reviews (product_id);

CREATE TABLE IF NOT EXISTS subscription_plans (
    id                     TEXT PRIMARY KEY,
    name                   TEXT NOT NULL,
    duration_days          INT NOT NULL DEFAULT 0,
    duration_label         TEXT NOT NULL DEFAULT '',
    price                  BIGINT NOT NULL DEFAULT 0,
    product_limit          INT NOT NULL DEFAULT 0,
    allowed_custom_domains INT NOT NULL DEFAULT 0,
    max_stores             INT NOT NULL DEFAULT 1,
    features               JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Seed from the plan catalogue the seller UI already renders (same prices
-- the server-side purchase map already charges for standard/pro/vip).
INSERT INTO subscription_plans (id, name, duration_days, duration_label, price, product_limit, allowed_custom_domains, max_stores, features) VALUES
    ('trial', 'آزمایشی', 14, '۱۴ روز', 0, 25, 0, 1, '[]'::jsonb),
    ('standard', 'استاندارد', 90, '۳ ماه', 890000, 100, 2, 1, '[]'::jsonb),
    ('pro', 'حرفه‌ای', 180, '۶ ماه', 1490000, 300, 5, 1, '[]'::jsonb),
    ('vip', 'ویژه', 365, '۱ سال', 3900000, 9999, 10, 10, '[]'::jsonb)
ON CONFLICT (id) DO NOTHING;

ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS product_fee_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS warehouse_tariffs JSONB;
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS packaging_tariffs JSONB;
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS trial_catalog_quota INT;
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS central_campaign_budget BIGINT;
