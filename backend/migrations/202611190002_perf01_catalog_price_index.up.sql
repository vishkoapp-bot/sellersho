-- 202611190002: PERF-01 public catalog price index (paged catalog selection).
--
-- The public list used to fetch the whole filtered catalog and price every
-- row in Go before slicing (`handleProductsList` + `GetProducts`). This
-- migration adds the lean public-price index the paging contract requires
-- (`docs/architecture/catalog-pagination.md`): one row per (store, product)
-- carrying ONLY displayed public prices — main price, original price,
-- discount percent and the active-variant price range — never supplier cost.
--
-- Freshness model (see `internal/repository/catalog_price_index.go`):
--   * `input_fingerprint` is a versioned md5 over the raw price inputs,
--     computed by ONE SQL expression used both by the staleness probe and
--     at refresh time. Any pricing-input change makes the row stale; the
--     next read refreshes exactly the stale rows (bounded batches, Go
--     engine only) instead of re-pricing the catalog.
--   * `valid_until` is the earliest upcoming time-windowed boundary
--     (campaign start/end, widget endsAt, variant sale from/to) affecting
--     the row, or NULL when none applies. A row whose boundary passed is
--     stale even though no table row changed.
--   * `catalog_scope_revisions.revision` is a per-store build counter,
--     bumped by a refresh transaction that changes >= 1 row price. Cursor
--     bindings carry it, so a price change mid-paging forces a restart
--     instead of a silently misordered page.
-- The index is a cache of the authoritative Go engine, never a second
-- pricing implementation: the only writer is
-- `CatalogRepository.RefreshCatalogPricesTx`, and the list/bounds handlers
-- always price the served page fresh in Go. Deleting index rows only costs
-- a rebuild, never correctness.
--
-- Search: `pg_trgm` (trusted extension since PG 13; creatable by the
-- database owner without superuser) backs GIN trigram indexes on
-- `lower(title)` / `lower(description)` so the existing wildcard
-- `LOWER(...) LIKE '%term%'` predicate can use an index instead of a
-- sequential scan. The predicate text is unchanged — only the index is new.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS catalog_public_prices (
  store_id          TEXT NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
  product_id        TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  main_price        BIGINT NOT NULL CHECK (main_price >= 0),
  original_price    BIGINT NOT NULL CHECK (original_price >= 0),
  discount_pct      INT NOT NULL DEFAULT 0 CHECK (discount_pct >= 0 AND discount_pct <= 100),
  price_min         BIGINT CHECK (price_min IS NULL OR price_min >= 0),
  price_max         BIGINT CHECK (price_max IS NULL OR price_max >= 0),
  input_fingerprint TEXT NOT NULL,
  valid_until       TIMESTAMPTZ,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (store_id, product_id)
);

CREATE TABLE IF NOT EXISTS catalog_scope_revisions (
  store_id   TEXT PRIMARY KEY REFERENCES stores(id) ON DELETE CASCADE,
  revision   BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Page selection by displayed price / discount / range. The product_id
-- tie-breaker keeps every ordering total (cursor paging needs it).
CREATE INDEX IF NOT EXISTS idx_catalog_prices_main
  ON catalog_public_prices (store_id, main_price, product_id);
CREATE INDEX IF NOT EXISTS idx_catalog_prices_discount
  ON catalog_public_prices (store_id, discount_pct DESC, product_id);
CREATE INDEX IF NOT EXISTS idx_catalog_prices_range
  ON catalog_public_prices (store_id, price_min, product_id);
-- Staleness probe: rows whose time boundary already passed.
CREATE INDEX IF NOT EXISTS idx_catalog_prices_valid_until
  ON catalog_public_prices (store_id, valid_until);

-- Trigram search on the exact expressions the catalog predicate uses.
CREATE INDEX IF NOT EXISTS idx_products_title_trgm
  ON products USING gin (lower(title) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_products_description_trgm
  ON products USING gin (lower(description) gin_trgm_ops);

-- Candidate selection (non-price sorts) ordering support.
CREATE INDEX IF NOT EXISTS idx_products_featured_created
  ON products (is_featured DESC, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_products_created
  ON products (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_store_products_position
  ON store_products (store_id, position, product_id);
