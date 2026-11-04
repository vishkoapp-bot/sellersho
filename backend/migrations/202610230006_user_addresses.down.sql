-- Down for 202610230006 (customer address book + Tapin geo snapshots):
-- drop both tables. Saved customer addresses are discarded by the
-- rollback — that is the point of rolling the feature back; placed
-- orders keep their own frozen address columns and are not affected.

DROP TABLE IF EXISTS tapin_geo_snapshots;
DROP TABLE IF EXISTS user_addresses;
