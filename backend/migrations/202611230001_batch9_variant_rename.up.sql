-- Batch 9 §2.5: derive names for reserved-name variants on variable or multi-variant products.
UPDATE product_variants v
SET variant_name = (
  SELECT string_agg(t.value, ' / ' ORDER BY t.key)
  FROM jsonb_each_text(v.attributes) AS t
  WHERE t.value <> ''
)
FROM products p
WHERE v.product_id = p.id
  AND v.variant_name IN ('پیش‌فرض', 'پیشفرض')
  AND (COALESCE(p.product_type, 'legacy') = 'variable'
       OR (SELECT COUNT(*) FROM product_variants WHERE product_id = p.id) > 1)
  AND EXISTS (SELECT 1 FROM jsonb_each_text(v.attributes) AS t WHERE t.value <> '');
