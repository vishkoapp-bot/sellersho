-- 202610220002: Tapin (تاپین) carrier integration credentials per supplier.
--
-- Each supplier may connect their own Tapin account: the API token issued
-- from the Tapin panel (integration → token management) and their Tapin
-- shop id. The token is a secret: it is stored here only, is never added
-- to the public Supplier DTO, and API responses expose it masked only.
--
-- Both columns are nullable: NULL = supplier has not connected Tapin.

ALTER TABLE suppliers
  ADD COLUMN IF NOT EXISTS tapin_token TEXT,
  ADD COLUMN IF NOT EXISTS tapin_shop_id TEXT;
