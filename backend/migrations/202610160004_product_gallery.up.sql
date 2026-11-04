-- Product gallery: multiple images per product
-- 202610160004

CREATE TABLE IF NOT EXISTS product_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    image_url TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_product_images_product ON product_images(product_id, sort_order);

-- Migrate existing single image_url values into the gallery as cover (sort_order=0)
INSERT INTO product_images (product_id, image_url, sort_order)
SELECT id, image_url, 0
FROM products
WHERE image_url IS NOT NULL AND TRIM(image_url) <> ''
ON CONFLICT DO NOTHING;
