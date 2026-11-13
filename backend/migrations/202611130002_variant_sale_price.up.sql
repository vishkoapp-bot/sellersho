-- 202611130002: per-variant sale price with optional window (P1(c)).
-- Sale price S is an ABSOLUTE final customer price in Toman that
-- REPLACES the resolved final price (lowest wins vs campaigns, floor
-- R = reference cost); it never stacks. All three columns NULL by
-- default => no sale; every existing price path is unchanged.
-- No platform_pricing_settings change (standing rule).
-- Down: DROP COLUMN sale_price, sale_from, sale_to (safe: a sale is
-- display-only state, no ledger references it).
ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS sale_price BIGINT
    CHECK (sale_price IS NULL OR sale_price > 0),
  ADD COLUMN IF NOT EXISTS sale_from TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS sale_to TIMESTAMPTZ;
-- Named constraints have no IF NOT EXISTS, so guard them with DO
-- blocks (repo convention, cf. 202610310007): a re-run after partial
-- application must not fail on "constraint already exists". What the
-- migration creates is unchanged.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'product_variants_sale_window_check'
  ) THEN
    ALTER TABLE product_variants
      ADD CONSTRAINT product_variants_sale_window_check
      CHECK (sale_from IS NULL OR sale_to IS NULL OR sale_from < sale_to);
  END IF;
END $$;
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'product_variants_sale_requires_price_check'
  ) THEN
    ALTER TABLE product_variants
      ADD CONSTRAINT product_variants_sale_requires_price_check
      CHECK (sale_price IS NOT NULL OR (sale_from IS NULL AND sale_to IS NULL));
  END IF;
END $$;
-- No backfill: all existing rows get NULL (no sale). No index: sale
-- columns are read with the variant row itself (GetVariantsByProductIDs),
-- never queried standalone.
