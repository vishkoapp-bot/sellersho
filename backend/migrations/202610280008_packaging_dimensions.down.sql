-- Down migration for 202610280008: drop the packaging-dimensions columns.
ALTER TABLE orders
  DROP COLUMN IF EXISTS packaging_height_mm,
  DROP COLUMN IF EXISTS packaging_width_mm,
  DROP COLUMN IF EXISTS packaging_length_mm;
ALTER TABLE products
  DROP COLUMN IF EXISTS packaging_height_mm,
  DROP COLUMN IF EXISTS packaging_width_mm,
  DROP COLUMN IF EXISTS packaging_length_mm;
