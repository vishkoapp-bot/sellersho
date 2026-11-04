-- Down for 202610230004 (seller discount campaigns): drop the table.
-- Campaign definitions are discarded by the rollback — that is the point
-- of rolling back the feature; order snapshots already taken keep their
-- frozen financials and are not affected.

DROP TABLE IF EXISTS seller_discount_campaigns;
