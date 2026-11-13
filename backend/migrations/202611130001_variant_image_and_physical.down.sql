-- Refuse to erase variant images silently: downgrade is only safe when
-- no variant images exist (same fail-closed style as 202611110003).
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM product_variant_images) THEN
    RAISE EXCEPTION 'Variant images exist; archive or delete them before downgrade';
  END IF;
END $$;
DROP TABLE IF EXISTS product_variant_images;
ALTER TABLE product_variants
  DROP COLUMN IF EXISTS weight_grams,
  DROP COLUMN IF EXISTS length_mm,
  DROP COLUMN IF EXISTS width_mm,
  DROP COLUMN IF EXISTS height_mm;
