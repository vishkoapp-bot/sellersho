-- Down for 202610220002 (supplier Tapin credentials): drop both columns.
-- Stored Tapin tokens/shop ids are discarded by the rollback — that is
-- the point of rolling back the feature; no other table references
-- these columns.

ALTER TABLE suppliers
  DROP COLUMN IF EXISTS tapin_shop_id,
  DROP COLUMN IF EXISTS tapin_token;
