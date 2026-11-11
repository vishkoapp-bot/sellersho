-- Store content intentionally remains editable and is not reset on downgrade.
-- The production release backup retains the previous presentation settings.
UPDATE store_templates
SET description = 'قالب فروشگاه پوشاک با تم تیره و مدرن؛ قرمز برای تخفیف، مناسب مد و لباس'
WHERE id = 'tpl-fashion-1'
  AND description = 'قالب روشن و مینیمال مد؛ بنر دو‌بخشی، مجموعه‌های تصویری و چیدمان ریسپانسیو';
