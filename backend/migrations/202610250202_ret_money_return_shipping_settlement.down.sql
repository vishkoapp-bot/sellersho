-- Reverts 202610250202 (money stream only). The core-stream columns
-- (warehouse_id, prepaid, tracking_number, shipped_at) are NOT dropped
-- here — they belong to the core stream's migration.
DROP TABLE IF EXISTS customer_return_shipping_charges;
ALTER TABLE customer_returns DROP COLUMN IF EXISTS return_shipping_settled_at;
ALTER TABLE customer_returns DROP COLUMN IF EXISTS return_shipping_bearer;
ALTER TABLE customer_returns DROP COLUMN IF EXISTS return_shipping_amount;
ALTER TABLE customer_returns DROP COLUMN IF EXISTS cod_refused;
ALTER TABLE customer_returns DROP COLUMN IF EXISTS return_code;
DROP SEQUENCE IF EXISTS customer_return_code_seq;
