-- Register the Digikala-inspired storefront template (tpl-digikala-1):
-- visual design inspired by Digikala (red #ef4056, prominent search,
-- circular categories, discount-badge cards, amazing-deals rail).
-- Features are our own; only the look & feel is Digikala-like.
INSERT INTO store_templates (id, name, description, thumbnail_url, is_active)
VALUES (
    'tpl-digikala-1',
    'ویترین دیجی‌استایل',
    'قالب فروشگاهی الهام‌گرفته از دیجی‌کالا؛ قرمز، جستجوی برجسته، کارت‌های تخفیف‌دار و ریل پیشنهاد شگفت‌انگیز',
    '/templates/digikala-thumb.svg',
    TRUE
)
ON CONFLICT (id) DO NOTHING;
