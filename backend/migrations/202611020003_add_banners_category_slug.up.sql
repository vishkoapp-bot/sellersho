-- Search-dropdown category banners (2026-11-02, staging).
-- A banner with category_slug set is a SEARCH-dropdown banner only:
-- it never renders in homepage hero/secondary/tile/promo slots (the
-- public handler excludes it from no-category queries), and it is
-- shown under the search dropdown only when the searched category
-- matches. NULL/empty = today's global banner, unchanged.
-- No FK to categories.slug by design: the admin write path validates
-- the slug exists (400 otherwise), and if a category is later
-- deleted/renamed the banner simply stops matching — fail-closed to
-- NO banner, never to a wrong banner. No seed rows: banners start
-- EMPTY; only admin-created rows ever render.
-- All statements are IF NOT EXISTS / guarded so a re-apply is a no-op.
ALTER TABLE banners
  ADD COLUMN IF NOT EXISTS category_slug TEXT;

CREATE INDEX IF NOT EXISTS idx_banners_category_active
  ON banners(category_slug, sort_order) WHERE is_active;
