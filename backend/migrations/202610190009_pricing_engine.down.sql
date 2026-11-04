ALTER TABLE platform_settings
    DROP COLUMN IF EXISTS max_dropshipper_margin_percent,
    DROP COLUMN IF EXISTS central_store_markup_percent,
    DROP COLUMN IF EXISTS default_markup_value,
    DROP COLUMN IF EXISTS default_markup_type;

ALTER TABLE suppliers
    DROP COLUMN IF EXISTS default_markup_value,
    DROP COLUMN IF EXISTS default_markup_type;

ALTER TABLE products
    DROP COLUMN IF EXISTS markup_value,
    DROP COLUMN IF EXISTS markup_type,
    DROP COLUMN IF EXISTS base_price_override;
