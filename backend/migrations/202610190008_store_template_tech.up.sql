-- Register the Tech storefront template (tpl-tech-1): a professional,
-- light, cool-toned storefront for digital products / gadgets.
INSERT INTO store_templates (id, name, description, thumbnail_url, is_active)
VALUES (
    'tpl-tech-1',
    'ویترین تکنو',
    'قالب حرفه‌ای و روشن با فضای خنک؛ مخصوص کالای دیجیتال، گجت و لوازم تکنولوژی',
    '/templates/tech-thumb.svg',
    TRUE
)
ON CONFLICT (id) DO NOTHING;
