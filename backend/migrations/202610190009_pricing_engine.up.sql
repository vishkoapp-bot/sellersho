-- 3-tier pricing engine: manual > supplier agreement > global default.
-- Platform markup (fixed OR percent) creates the base price from supplier cost.

-- 1. Products: per-product manual pricing (HIGHEST priority)
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS base_price_override BIGINT,
    ADD COLUMN IF NOT EXISTS markup_type VARCHAR(10) CHECK (markup_type IN ('fixed','percent')),
    ADD COLUMN IF NOT EXISTS markup_value NUMERIC(10,2);

-- 2. Suppliers: per-supplier agreement markup (MEDIUM priority)
ALTER TABLE suppliers
    ADD COLUMN IF NOT EXISTS default_markup_type VARCHAR(10) CHECK (default_markup_type IN ('fixed','percent')),
    ADD COLUMN IF NOT EXISTS default_markup_value NUMERIC(10,2);

-- 3. Platform settings: global defaults (LOWEST priority) + central store + max margin
ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS default_markup_type VARCHAR(10) NOT NULL DEFAULT 'percent',
    ADD COLUMN IF NOT EXISTS default_markup_value NUMERIC(10,2) NOT NULL DEFAULT 30,
    ADD COLUMN IF NOT EXISTS central_store_markup_percent NUMERIC(5,2) NOT NULL DEFAULT 10,
    ADD COLUMN IF NOT EXISTS max_dropshipper_margin_percent NUMERIC(5,2) NOT NULL DEFAULT 100;
