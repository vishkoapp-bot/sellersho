-- Down migration for 202611190002 (PERF-01 catalog price index).
-- Drops the index tables and the PERF-01 indexes. The catalog list falls
-- back to the pre-PERF-01 load-all path (which is why this down migration
-- must never be run while the paged handlers are deployed).
-- Only run on a disposable database.
DROP INDEX IF EXISTS idx_store_products_position;
DROP INDEX IF EXISTS idx_products_created;
DROP INDEX IF EXISTS idx_products_featured_created;
DROP INDEX IF EXISTS idx_products_description_trgm;
DROP INDEX IF EXISTS idx_products_title_trgm;
DROP TABLE IF EXISTS catalog_scope_revisions;
DROP TABLE IF EXISTS catalog_public_prices;
-- pg_trgm is left installed: other objects may use it, and dropping an
-- extension is a shared-database decision, not a migration rollback.
