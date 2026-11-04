-- 202610230003: Daily final-price snapshots per product x store
-- (Seller Growth Phase 4, user decision A).
--
-- Why: discount percentages were computed from the synthetic anchor price
-- (products.original_price) with NO real price history, so a "discount"
-- could never be proven real. The user decided (option A) that history is
-- a DAILY SNAPSHOT of the final price P (after ResolvePricing) that the
-- customer of that exact store actually sees, taken by a daily job in the
-- existing background worker (same pattern as the Phase 2 search rollup).
--
-- The 30-day display guard (user decision C) reads this table: a
-- product x store pair needs >= 30 distinct snapshot days before ANY
-- discount percent / strikethrough price may be displayed for it, and a
-- campaign percent additionally requires the campaign price to be below
-- the pair's average snapshot price of the last 30 days.
--
-- History starts the day this system is switched on; past sales are NOT
-- backfilled into history (user decision A).
--
-- One row per (store, product, Tehran day); the daily job is idempotent
-- (ON CONFLICT DO NOTHING) so a re-run on the same day adds no rows.

CREATE TABLE IF NOT EXISTS product_price_daily (
  id BIGSERIAL PRIMARY KEY,
  store_id TEXT NOT NULL,
  product_id TEXT NOT NULL,
  day DATE NOT NULL,
  final_price BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (store_id, product_id, day)
);

CREATE INDEX IF NOT EXISTS idx_product_price_daily_pair_day
  ON product_price_daily (store_id, product_id, day DESC);
