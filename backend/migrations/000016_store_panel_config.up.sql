ALTER TABLE stores ADD COLUMN IF NOT EXISTS panel_config JSONB NOT NULL DEFAULT '{}'::jsonb;
CREATE TABLE IF NOT EXISTS store_products (
    store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    position INT NOT NULL DEFAULT 0,
    PRIMARY KEY (store_id, product_id)
);