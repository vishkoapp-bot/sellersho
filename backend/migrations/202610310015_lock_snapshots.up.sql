-- Lock snapshots (Ruling B, 2026-11-01): unlocking a margin lock
-- RESTORES the seller value(s) from before the lock was created.
--
-- STORAGE CHOICE: a separate table, not a column on
-- store_margin_locks — because one lock can snapshot MANY values (a
-- product lock snapshots the product-level override PLUS one row per
-- variant of that product). One row per snapshotted target keeps
-- restore a plain upsert/delete per row and keeps the lock row itself
-- unchanged.
--
-- Row presence IS the snapshot's existence: a lock created before this
-- migration has no rows, and unlock then falls back to keeping the
-- current margin (documented fallback, Ruling B). had_value=false
-- means "no override existed at lock time": unlock DELETES the target
-- override (an admin or seller value created during the lock does not
-- survive unlock — the snapshot is the truth). mode is the stored
-- override mode ('percent'; fixed rows are never applied, but the mode
-- is restored verbatim for fidelity).
-- target_kind 'store' snapshots the store-wide lock's prior
-- custom_store_margin_override (had_value=false means it was NULL).
CREATE TABLE IF NOT EXISTS store_margin_lock_snapshots (
    store_id       VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    scope          VARCHAR(10) NOT NULL CHECK (scope IN ('product', 'niche', 'variant', 'store')),
    scope_id       VARCHAR(100) NOT NULL,
    target_kind    VARCHAR(10) NOT NULL CHECK (target_kind IN ('product', 'niche', 'variant', 'store')),
    target_id      VARCHAR(100) NOT NULL,
    had_value      BOOLEAN NOT NULL,
    override_value NUMERIC(12,2),
    mode           VARCHAR(10),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (store_id, scope, scope_id, target_kind, target_id)
);
CREATE INDEX IF NOT EXISTS idx_lock_snapshots_store_scope
    ON store_margin_lock_snapshots (store_id, scope, scope_id);
