-- 202610280005: category hierarchy columns + import audit log.
--
-- WHY: the categories table is FLAT (no parent_id/depth/sort_order), so the
-- 5-level Digikala tree cannot be stored in it. This migration adds the
-- minimal hierarchy support the import needs, plus the audit log table
-- that import.sql writes and rollback.sql reads.
--
-- DESIGN (Worker A, mapping-summary.md §6-3 — settled Oct 28, 2026):
--   parent_id  VARCHAR(50) REFERENCES categories(id) ON DELETE SET NULL
--              NULL for roots.
--   depth      SMALLINT NOT NULL DEFAULT 0, CHECK 0..5 (Digikala max depth 5)
--   sort_order INT NOT NULL DEFAULT 0 — sibling order in Digikala API order
--              (ORDER BY id is wrong for cat-dk-<number> ids).
--   + CHECK parent_id <> id (no self-parent).
-- Existing rows (the 5 live categories) get parent_id NULL, depth 0,
-- sort_order 0: they stay top-level roots, unchanged in every other way.
--
-- REVIEW NOTE: this file applies DDL. It is NOT applied by import.sql —
-- import.sql only ASSERTS these columns exist. Apply it separately, only
-- after the user confirms. Do NOT apply it before the ambiguous-roots
-- decision (mapping.json) is resolved — the import cannot run before that
-- anyway (generate_scripts.py refuses while ambiguous rows remain).

ALTER TABLE categories
    ADD COLUMN IF NOT EXISTS parent_id VARCHAR(50) REFERENCES categories(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS depth SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_categories_parent_id ON categories(parent_id);
ALTER TABLE categories ADD CONSTRAINT categories_depth_range CHECK (depth BETWEEN 0 AND 5);
ALTER TABLE categories ADD CONSTRAINT categories_no_self_parent CHECK (parent_id IS NULL OR parent_id <> id);

-- Audit log for the Digikala import. One row per Digikala node that the
-- mapping covers (creates AND merges). rollback.sql deletes exactly the
-- rows with action='create' AND rolled_back_at IS NULL.
--   our_id / our_slug : what the node became in our categories table
--   our_parent_id     : parent linkage in OUR id space
--   dk_id             : Digikala node id (UNIQUE — re-import revives the row)
--   dk_code / title_fa: source values, for audit and spot-checks
--   action            : 'create' (new row) or 'merge' (existing category kept)
--   rolled_back_at    : NULL while live; set by rollback.sql
CREATE TABLE IF NOT EXISTS category_import_log (
    our_id        VARCHAR(50) NOT NULL,
    our_slug      VARCHAR(100) NOT NULL,
    our_parent_id VARCHAR(50),
    dk_id         INT NOT NULL UNIQUE,
    dk_code       VARCHAR(100) NOT NULL,
    depth         SMALLINT NOT NULL CHECK (depth BETWEEN 0 AND 5),
    title_fa      VARCHAR(100) NOT NULL,
    action        VARCHAR(10) NOT NULL CHECK (action IN ('create', 'merge')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rolled_back_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_category_import_log_our_id ON category_import_log(our_id);
CREATE INDEX IF NOT EXISTS idx_category_import_log_live ON category_import_log(action, rolled_back_at);
