-- Migration 202610160009: marketing admin persistence (Group B tabs).
--
-- 1. platform_settings: trial catalog quota + central campaign budget columns.
--    Read/written by the super-admin "کمپین‌ها و پلن‌ها" and "شبکه ارجاع" tabs.
-- 2. subscription_plans: full CRUD table backing the dropshipper membership
--    plans managed in the super-admin "کمپین‌ها و پلن‌ها" tab.
--    Seeded with the platform's existing plan definitions.

ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS trial_catalog_quota INT NOT NULL DEFAULT 25,
    ADD COLUMN IF NOT EXISTS central_campaign_budget BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS subscription_plans (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    duration_days INT NOT NULL CHECK (duration_days >= 1),
    duration_label TEXT NOT NULL DEFAULT '',
    price BIGINT NOT NULL DEFAULT 0 CHECK (price >= 0),
    product_limit INT NOT NULL DEFAULT 25 CHECK (product_limit >= 1),
    allowed_custom_domains INT NOT NULL DEFAULT 0 CHECK (allowed_custom_domains >= 0),
    max_stores INT NOT NULL DEFAULT 1 CHECK (max_stores >= 1),
    features JSONB NOT NULL DEFAULT '[]'::jsonb,
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO subscription_plans (id, name, duration_days, duration_label, price, product_limit, allowed_custom_domains, max_stores, features, sort_order, is_active)
VALUES
    ('trial', 'آزمایشی رایگان', 14, '۱۴ روزه', 0, 25, 0, 1,
     '["۲۵ قلم کالا در کاتالوگ", "ویترین اختصاصی سلر", "لجستیک و انبارداری متمرکز", "تسویه حساب هفتگی پایا"]', 0, TRUE),
    ('standard', 'استاندارد ۳ ماهه', 90, '۳ ماهه', 890000, 100, 2, 1,
     '["۱۰۰ قلم کالا در کاتالوگ", "۲ دامنه اختصاصی", "شارژ پنل پیامکی هدیه", "تسویه حساب روزانه پایا"]', 10, TRUE),
    ('pro', 'حرفه‌ای ۶ ماهه', 180, '۶ ماهه', 1490000, 300, 5, 1,
     '["۳۰۰ قلم کالا در کاتالوگ", "۵ دامنه اختصاصی", "ربات تلگرام و اتوماسیون سفارش", "ویجت تبلیغاتی اختصاصی"]', 20, TRUE),
    ('vip', 'طلایی یک‌ساله VIP', 365, 'یک‌ساله VIP', 3900000, 9999, 10, 10,
     '["سقف کاتالوگ حداکثری (۹,۹۹۹ قلم کالا)", "۱۰ دامنه اختصاصی با اعتبارسنجی خودکار DNS", "ساخت و مدیریت تا ۱۰ فروشگاه با یک حساب", "کارمزد تسویه صفر", "مدیر حساب اختصاصی"]', 30, TRUE)
ON CONFLICT (id) DO NOTHING;
