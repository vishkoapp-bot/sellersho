-- Down migration for 202610160007_finance_admin_settings.

DROP TABLE IF EXISTS supplier_promotions;

ALTER TABLE platform_settings
    DROP COLUMN IF EXISTS product_fee_overrides;
