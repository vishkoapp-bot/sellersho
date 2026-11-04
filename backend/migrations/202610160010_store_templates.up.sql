CREATE TABLE IF NOT EXISTS store_templates (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    thumbnail_url TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE stores ADD COLUMN IF NOT EXISTS template_id VARCHAR(50) REFERENCES store_templates(id) ON DELETE SET NULL;
ALTER TABLE stores ADD COLUMN IF NOT EXISTS template_config JSONB NOT NULL DEFAULT '{}'::jsonb;
INSERT INTO store_templates (id, name, description, thumbnail_url, is_active)
VALUES ('tpl-modern-1', 'ویترین مدرن', 'قالب مینیمال و مدرن با چیدمان متفاوت از فروشگاه مرکزی؛ مناسب ویترین‌های شخصی', '/templates/modern-thumb.svg', TRUE)
ON CONFLICT (id) DO NOTHING;
