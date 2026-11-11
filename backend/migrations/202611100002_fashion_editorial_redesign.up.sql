-- Refresh template metadata and the recognizable, product-image demo preset.
-- Seller-created hero images, slides and banners do not match this signature.
UPDATE store_templates
SET description = 'قالب روشن و مینیمال مد؛ بنر دو‌بخشی، مجموعه‌های تصویری و چیدمان ریسپانسیو'
WHERE id = 'tpl-fashion-1';

UPDATE stores
SET template_config = template_config || jsonb_build_object(
    'accentColor', '#793f3c',
    'sections', '[{"key":"announcement","enabled":true},{"key":"hero","enabled":true},{"key":"categoryRail","enabled":true},{"key":"products","enabled":true},{"key":"lookbook","enabled":true},{"key":"about","enabled":true}]'::jsonb,
    'custom', (template_config->'custom') || jsonb_build_object(
        'heroSlides', '[]'::jsonb,
        'heroImage', '/templates/fashion/hero.webp',
        'heroTitle', 'سادگی، به سبک تو',
        'heroSubtitle', 'انتخاب‌هایی برای استایل روزمره و خاص؛ با رنگ‌های آرام و جزئیاتی که ماندگار می‌شوند.',
        'heroCtaText', 'کشف کالکشن',
        'heroCtaLink', '#products',
        'heroAutoplay', false,
        'productsTitle', 'تازه رسیده‌ها',
        'banners', '[{"image":"/templates/fashion/categories.webp","title":"پوشاک زنانه","link":"#products"},{"image":"/templates/fashion/categories.webp","title":"پوشاک مردانه","link":"#products"},{"image":"/templates/fashion/categories.webp","title":"کیف و اکسسوری","link":"#products"}]'::jsonb,
        'lookbookImage', '/templates/fashion/knitwear.webp',
        'lookbookTitle', 'برای روزهای خنک',
        'lookbookSubtitle', 'انتخاب‌هایی گرم، بافت‌هایی لطیف و استایلی ماندگار.',
        'lookbookLink', '#products',
        'aboutImage', CASE
            WHEN template_config#>>'{custom,aboutImage}' = '/static/uploads/products/democat-20261101-extra-virgin-olive-oil-1.webp' THEN ''
            ELSE COALESCE(template_config#>>'{custom,aboutImage}', '')
        END
    )
), updated_at = NOW()
WHERE template_id = 'tpl-fashion-1'
  AND template_config#>>'{custom,heroImage}' = '/static/uploads/products/democat-20261101-gold-bracelet-18k-1.webp'
  AND template_config#>>'{custom,heroSlides,0,title}' = 'دستبند طلا ۱۸ عیار'
  AND template_config#>>'{custom,banners,0,image}' = '/static/uploads/products/img-demo-men-gift-set.webp';
