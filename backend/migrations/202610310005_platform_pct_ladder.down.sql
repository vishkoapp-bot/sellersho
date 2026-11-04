-- Down for 202610310005: drops ONLY the two tables this migration
-- creates. No pre-existing object is touched. Rolling back removes the
-- per-variant/per-category percentages; the resolver then falls back
-- to the global platform_pct (never to the legacy absolute-F ladder,
-- which stays disconnected from the price path after the switch).
DROP TABLE IF EXISTS platform_pct_products;
DROP TABLE IF EXISTS platform_pct_categories;
