-- Down for 202610200003 (product brand + rating aggregates + product_reviews).

DROP INDEX IF EXISTS idx_products_brand;
DROP INDEX IF EXISTS idx_product_reviews_customer;
DROP INDEX IF EXISTS idx_product_reviews_product;
DROP TABLE IF EXISTS product_reviews;
ALTER TABLE products DROP COLUMN IF EXISTS reviews_count;
ALTER TABLE products DROP COLUMN IF EXISTS rating;
ALTER TABLE products DROP COLUMN IF EXISTS brand;
