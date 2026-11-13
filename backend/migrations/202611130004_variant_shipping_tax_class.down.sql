-- Down for 202611130004: drop the P2(b) shipping/tax class columns.
ALTER TABLE products
  DROP COLUMN IF EXISTS shipping_class,
  DROP COLUMN IF EXISTS tax_class;
ALTER TABLE product_variants
  DROP COLUMN IF EXISTS shipping_class,
  DROP COLUMN IF EXISTS tax_class;
