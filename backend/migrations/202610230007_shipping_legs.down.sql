-- Down for 202610230007 (per-supplier shipping legs): drop the legs
-- table and the two added columns. orders.shipping_cost keeps its
-- frozen Σ-legs value on placed orders — rolling back the breakdown
-- does not change any charged amount.

ALTER TABLE supplier_shipments DROP COLUMN IF EXISTS send_price;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS default_item_weight_grams;
DROP TABLE IF EXISTS order_shipping_legs;
