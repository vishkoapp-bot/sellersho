-- Rollback for 202610160003_order_cancel_shipping.
ALTER TABLE orders DROP COLUMN IF EXISTS weight_grams;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS shipping_per_kg;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS shipping_base_rate;
