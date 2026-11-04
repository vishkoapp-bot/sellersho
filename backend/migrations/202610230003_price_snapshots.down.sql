-- Down for 202610230003 (daily price snapshots): drop the table.
-- The collected price history is discarded by the rollback — that is the
-- point of rolling back the feature; no other table references it.

DROP TABLE IF EXISTS product_price_daily;
