-- P0 variant image + physical attributes (2026-11-13).
-- All new product_variants columns are NULLABLE: NULL = inherit from the
-- product (same convention as supplier_base_cost). No backfill; existing
-- rows resolve exactly as before.

ALTER TABLE product_variants
  ADD COLUMN weight_grams INT NULL CHECK (weight_grams IS NULL OR weight_grams > 0),
  ADD COLUMN length_mm   INT NULL CHECK (length_mm   IS NULL OR length_mm   > 0),
  ADD COLUMN width_mm    INT NULL CHECK (width_mm    IS NULL OR width_mm    > 0),
  ADD COLUMN height_mm   INT NULL CHECK (height_mm   IS NULL OR height_mm   > 0);

-- Variant images: one row per image, ordered; mirrors product_images
-- (202610160004) but scoped to the variant so variant photos never leak
-- into the product-level gallery.
CREATE TABLE IF NOT EXISTS product_variant_images (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
  image_url TEXT NOT NULL,
  sort_order INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_product_variant_images_variant
  ON product_variant_images(variant_id, sort_order);
