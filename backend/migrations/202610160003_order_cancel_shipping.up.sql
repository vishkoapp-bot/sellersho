-- Worker 2 (Order lifecycle fixes): settings-driven shipping + order cancel support.
-- shipping_base_rate: flat base shipping fee in Rials charged per order.
-- shipping_per_kg: additional per-kg rate in Rials (applied on ceil(total_weight_grams/1000)).
-- orders.weight_grams: actual packed weight recorded by warehouse staff (optional).
ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS shipping_base_rate BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS shipping_per_kg BIGINT NOT NULL DEFAULT 0;

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS weight_grams INT;
