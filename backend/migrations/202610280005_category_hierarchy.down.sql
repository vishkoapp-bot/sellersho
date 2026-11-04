-- 202610280005 DOWN — reverses the hierarchy migration.
--
-- ⚠️  GUARDS: refuses to run while any imported category still exists
-- (products/niches guards live in rollback.sql — run rollback.sql FIRST).
-- Dropping the log table would destroy the only record of what was
-- imported, so this file refuses while the log has ANY rows at all;
-- truncate the log explicitly (with a conscious decision) if it must go.

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM categories WHERE parent_id IS NOT NULL OR depth <> 0 OR sort_order <> 0) THEN
    RAISE EXCEPTION '202610280005 rollback blocked: categories still use parent_id/depth/sort_order — run rollback.sql first';
  END IF;
  IF EXISTS (SELECT 1 FROM category_import_log) THEN
    RAISE EXCEPTION '202610280005 rollback blocked: category_import_log still has rows (audit record) — clear it explicitly if the import history may be discarded';
  END IF;
END $$;

ALTER TABLE categories DROP CONSTRAINT IF EXISTS categories_no_self_parent;
ALTER TABLE categories DROP CONSTRAINT IF EXISTS categories_depth_range;
DROP INDEX IF EXISTS idx_categories_parent_id;
ALTER TABLE categories DROP COLUMN IF EXISTS sort_order;
ALTER TABLE categories DROP COLUMN IF EXISTS depth;
ALTER TABLE categories DROP COLUMN IF EXISTS parent_id;
DROP INDEX IF EXISTS idx_category_import_log_live;
DROP INDEX IF EXISTS idx_category_import_log_our_id;
DROP TABLE IF EXISTS category_import_log;
