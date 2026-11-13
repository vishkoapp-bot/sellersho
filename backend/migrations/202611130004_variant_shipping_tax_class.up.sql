-- 202611130004: shipping/tax class per variant, inheriting product level (P2(b)).
-- Stored vocabulary only in v1: NOT consumed by pricing, Tapin quotes,
-- or any tax calculation (no tax engine exists; do not invent one).
-- Effective value: variant class ?? product class ?? 'standard'.
-- No platform_pricing_settings change (standing rule).
-- Backfill: none needed (products default 'standard'; variants NULL = inherit).
-- Down: DROP COLUMN on both tables (safe: no consumer reads them yet).
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS shipping_class TEXT NOT NULL DEFAULT 'standard'
    CHECK (shipping_class IN ('standard','fragile','oversized','heavy','cold_chain')),
  ADD COLUMN IF NOT EXISTS tax_class TEXT NOT NULL DEFAULT 'standard'
    CHECK (tax_class IN ('standard','reduced','zero','exempt'));
ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS shipping_class TEXT
    CHECK (shipping_class IS NULL OR shipping_class IN ('standard','fragile','oversized','heavy','cold_chain')),
  ADD COLUMN IF NOT EXISTS tax_class TEXT
    CHECK (tax_class IS NULL OR tax_class IN ('standard','reduced','zero','exempt'));
