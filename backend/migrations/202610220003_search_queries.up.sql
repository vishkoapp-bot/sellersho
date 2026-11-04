-- 202610220003: Search query log + daily rollup (Seller Growth Phase 2).
--
-- Why: the platform had NO record of what customers search for, so sellers
-- and suppliers could not see unmet demand. This migration adds a
-- lightweight, privacy-first search log used only by the seller/supplier
-- search-insight endpoints (GET /api/v1/seller/search-insight and
-- GET /api/v1/supplier/search-insight).
--
-- Privacy (user decision 6, approved): ONLY the normalized search term,
-- the effective store slug, the result count and the timestamp are stored.
-- Deliberately NO ip column, NO user id, NO session id — no personal data
-- is ever written to this table.
--
-- Retention: raw rows live for 90 days. The background worker rolls rows
-- older than 90 days into search_term_daily (one row per term/store/day,
-- bucketed on the Asia/Tehran calendar day) and then deletes the raw rows,
-- so long-term storage stays aggregated (see
-- repository.RollupSearchQueries).

CREATE TABLE IF NOT EXISTS search_queries (
  id BIGSERIAL PRIMARY KEY,
  term_normalized TEXT NOT NULL,
  store_slug TEXT NOT NULL,
  result_count INTEGER NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_search_queries_created_at ON search_queries (created_at);
CREATE INDEX IF NOT EXISTS idx_search_queries_store_created ON search_queries (store_slug, created_at);
CREATE INDEX IF NOT EXISTS idx_search_queries_term ON search_queries (term_normalized);

CREATE TABLE IF NOT EXISTS search_term_daily (
  term_normalized TEXT NOT NULL,
  store_slug TEXT NOT NULL,
  day DATE NOT NULL,
  searches INTEGER NOT NULL,
  zero_result_searches INTEGER NOT NULL,
  PRIMARY KEY (term_normalized, store_slug, day)
);
