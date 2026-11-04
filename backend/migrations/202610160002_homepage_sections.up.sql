-- Homepage sections CMS: each row is a renderable section on the homepage.
-- section_type determines which component renders; config holds type-specific settings as JSON.
CREATE TABLE IF NOT EXISTS homepage_sections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    section_type VARCHAR(50) NOT NULL CHECK (section_type IN (
        'secondary_slider',
        'product_carousel',
        'tile_grid',
        'promo_banner',
        'store_carousel',
        'suppliers_showcase',
        'product_grid',
        'trust_features'
    )),
    title VARCHAR(200),
    config JSONB NOT NULL DEFAULT '{}',
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_homepage_sections_active
    ON homepage_sections(sort_order) WHERE is_active;

-- Seed the original homepage structure (all content now database-driven)
INSERT INTO homepage_sections (section_type, title, config, sort_order, is_active) VALUES
('secondary_slider', NULL, '{}', 10, TRUE),
('product_carousel', 'لوازم تحریر', '{"categorySlug":"culture"}', 20, TRUE),
('suppliers_showcase', NULL, '{}', 30, TRUE),
('product_carousel', 'کالای دیجیتال', '{"categorySlug":"digital"}', 40, TRUE),
('store_carousel', 'محبوب‌ترین کالکشن‌ها', '{}', 50, TRUE),
('promo_banner', NULL, '{"index":0}', 60, TRUE),
('promo_banner', NULL, '{"index":1}', 70, TRUE),
('tile_grid', NULL, '{"layout":"quad"}', 80, TRUE),
('tile_grid', NULL, '{"layout":"double"}', 90, TRUE),
('promo_banner', NULL, '{"index":2}', 100, TRUE),
('tile_grid', NULL, '{"layout":"double","offset":2}', 110, TRUE),
('store_carousel', 'کالکشن‌های منتخب', '{"offset":1}', 120, TRUE),
('promo_banner', NULL, '{"index":3}', 130, TRUE),
('tile_grid', NULL, '{"layout":"quad","offset":4}', 140, TRUE),
('tile_grid', NULL, '{"layout":"triple"}', 150, TRUE),
('tile_grid', NULL, '{"layout":"double","offset":6}', 160, TRUE),
('product_grid', NULL, '{}', 170, TRUE),
('tile_grid', NULL, '{"layout":"triple","offset":8}', 180, TRUE),
('store_carousel', 'جدیدترین کالکشن‌ها', '{"offset":2}', 190, TRUE),
('store_carousel', 'کالکشن‌های دارای تخفیف', '{"offset":3}', 200, TRUE),
('promo_banner', NULL, '{"index":4}', 210, TRUE),
('trust_features', NULL, '{}', 220, TRUE);
