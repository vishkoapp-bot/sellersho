-- 202610200003: product brand + rating aggregates + product_reviews
-- (store-gaps spec §۱.۱). Brand is harvested from the ghost specs key
-- suppliers already typed ('برند'); rating/reviews_count are maintained
-- transactionally by the review repository (published reviews only).

ALTER TABLE products ADD COLUMN IF NOT EXISTS brand TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS rating NUMERIC(3,2) NOT NULL DEFAULT 0;
ALTER TABLE products ADD COLUMN IF NOT EXISTS reviews_count INT NOT NULL DEFAULT 0;

-- Harvest the ghost specs brands suppliers already typed (audit §1.3).
UPDATE products SET brand = TRIM(specs->>'برند')
WHERE COALESCE(TRIM(specs->>'برند'), '') <> '' AND brand = '';

CREATE TABLE IF NOT EXISTS product_reviews (
    id                    VARCHAR(50) PRIMARY KEY,          -- 'REV-<uuid>', app-generated
    product_id            VARCHAR(50) NOT NULL REFERENCES products(id),
    order_id              VARCHAR(50) NOT NULL REFERENCES orders(id),
    order_item_id         UUID NOT NULL UNIQUE REFERENCES order_items(id),
    customer_id           UUID NOT NULL REFERENCES users(id),
    customer_display_name VARCHAR(100) NOT NULL,
    rating                SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    title                 VARCHAR(150),
    body                  TEXT NOT NULL,
    status                VARCHAR(20) NOT NULL DEFAULT 'published'
                          CHECK (status IN ('published','hidden','deleted')),
    moderated_by          VARCHAR(100),
    moderated_at          TIMESTAMPTZ,
    moderation_reason     TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_product_reviews_product
    ON product_reviews (product_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_product_reviews_customer
    ON product_reviews (customer_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_products_brand ON products (brand) WHERE brand <> '';
