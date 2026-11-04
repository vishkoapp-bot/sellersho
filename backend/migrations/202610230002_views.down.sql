-- Down for 202610230002 (view counting): drop all three tables.
-- The aggregated view statistics are discarded by the rollback — that is
-- the point of rolling back the feature; no other table references them.

DROP TABLE IF EXISTS view_dedupe_daily;
DROP TABLE IF EXISTS store_view_daily;
DROP TABLE IF EXISTS product_view_daily;
