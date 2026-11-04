-- Banners for homepage and promotional placements (managed via super-admin panel)
CREATE TABLE IF NOT EXISTS banners (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(200) NOT NULL,
    subtitle TEXT,
    image_url TEXT NOT NULL,
    link_url VARCHAR(500),
    position VARCHAR(50) NOT NULL DEFAULT 'hero'
        CHECK (position IN ('hero', 'secondary', 'tile', 'promo')),
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_banners_position_active
    ON banners(position, sort_order) WHERE is_active;
