-- Down for 202610220003 (search query log): drop both tables.
-- The aggregated statistics in search_term_daily and any raw rows still in
-- search_queries are discarded by the rollback — that is the point of
-- rolling back the feature; no other table references these tables.

DROP TABLE IF EXISTS search_term_daily;
DROP TABLE IF EXISTS search_queries;
