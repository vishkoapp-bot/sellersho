-- Register the Fashion storefront template (tpl-fashion-1):
-- dark-mode navy/indigo theme inspired by the Fosn Figma community file.
-- For clothing / fashion / apparel storefronts.
INSERT INTO store_templates (id, name, description, thumbnail_url, is_active)
VALUES (
    'tpl-fashion-1',
    'ویترین مد',
    'قالب فروشگاه پوشاک با تم تیره و مدرن؛ قرمز برای تخفیف، مناسب مد و لباس',
    '/templates/fashion-thumb.svg',
    TRUE
)
ON CONFLICT (id) DO NOTHING;
