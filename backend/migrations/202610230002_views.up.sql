-- 202610230002: Product/store view counting (Seller Growth Phase 3).
--
-- Why: the platform recorded NO product or storefront views, so the seller
-- analytics tab could not show a conversion rate (views -> orders). This
-- migration adds the daily-aggregated view counters plus the daily dedupe
-- table the view recorder (repository.ViewRecorder) writes through.
--
-- Privacy (same rule as Phase 2 search log, user decision): ONLY the store
-- slug, the product id, the Tehran calendar day and a SHA-256 HASH of the
-- client's daily token are stored. Deliberately NO ip column, NO user id,
-- NO raw token — the raw token never leaves the browser except as the
-- bearer of the event POST and is hashed before it is stored.
--
-- Dedupe rule (coordinator determination 5): one daily token counts at
-- most ONE product view per product and ONE store view per store per
-- Tehran day; product and store events are counted separately. The worker
-- deletes dedupe rows older than 2 days (they can never dedupe again once
-- the client token has rotated).

CREATE TABLE IF NOT EXISTS product_view_daily (
  store_slug TEXT NOT NULL,
  product_id TEXT NOT NULL,
  day DATE NOT NULL,
  views INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (store_slug, product_id, day)
);

CREATE TABLE IF NOT EXISTS store_view_daily (
  store_slug TEXT NOT NULL,
  day DATE NOT NULL,
  views INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (store_slug, day)
);

CREATE TABLE IF NOT EXISTS view_dedupe_daily (
  token_hash TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('product', 'store')),
  target_id TEXT NOT NULL,
  day DATE NOT NULL,
  PRIMARY KEY (token_hash, kind, target_id, day)
);

CREATE INDEX IF NOT EXISTS idx_view_dedupe_day ON view_dedupe_daily (day);
