-- Register the Electronic storefront template (tpl-electronic-1):
-- pixel-exact from Figma "E-commerce Electronic App Design".
-- Light theme, vivid orange accent, for digital products / gadgets.
INSERT INTO store_templates (id, name, description, thumbnail_url, is_active)
VALUES (
    'tpl-electronic-1',
    'ویترین الکترونیک',
    'قالب فروشگاه الکترونیک از روی طرح فیگما؛ تم روشن با نارنجی، مخصوص گجت و کالای دیجیتال',
    '/templates/electronic-thumb.svg',
    TRUE
)
ON CONFLICT (id) DO NOTHING;
