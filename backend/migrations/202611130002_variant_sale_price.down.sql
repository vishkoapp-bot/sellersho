-- Down for 202611130002: drop the P1(c) per-variant sale columns.
ALTER TABLE product_variants
  DROP COLUMN IF EXISTS sale_price,
  DROP COLUMN IF EXISTS sale_from,
  DROP COLUMN IF EXISTS sale_to;
