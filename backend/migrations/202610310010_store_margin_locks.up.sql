-- Granular admin margin locks (Andrew rulings, 2026-11-01).
--
-- The store-wide lock (stores.is_margin_locked_by_admin) stays, but it
-- is no longer the only lock: an admin can now lock or unlock a single
-- pricing scope — one product, one niche, or one variant — mirroring
-- the dropshipper's own override granularity (product > niche > ladder,
-- with variant most specific). A locked scope's value beats the
-- seller's override for THAT scope only; other scopes are unaffected.
-- Unlocking a scope restores the seller's override for that scope.
--
-- Each lock has one of two modes:
--   freeze    — the seller's current override value for that scope is
--               frozen: the seller cannot save changes to that scope
--               while frozen, and the frozen value applies. If the
--               seller has no value for that scope, the freeze
--               contributes nothing and resolution falls through to
--               broader scopes (documented edge, not an error).
--   admin_set — an admin percent that replaces the seller's override
--               for that scope (lock_value is REQUIRED here).
--
-- scope_id holds the product_id, niche_id, or variant_id (UUIDs stored
-- as text so one column serves all three scopes; variant ids are
-- globally unique, so no product column is needed for lookup).
CREATE TABLE IF NOT EXISTS store_margin_locks (
    store_id   VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    scope      VARCHAR(10) NOT NULL CHECK (scope IN ('product', 'niche', 'variant')),
    scope_id   VARCHAR(100) NOT NULL,
    lock_mode  VARCHAR(10) NOT NULL CHECK (lock_mode IN ('freeze', 'admin_set')),
    lock_value NUMERIC(12,2),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (store_id, scope, scope_id),
    CHECK ((lock_mode = 'admin_set' AND lock_value IS NOT NULL) OR (lock_mode = 'freeze'))
);
CREATE INDEX IF NOT EXISTS idx_store_margin_locks_store
    ON store_margin_locks (store_id);
