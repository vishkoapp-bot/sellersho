-- Down migration for 202610280007: drop the configurable default Tapin box,
-- the platform Tapin account store, and the creation-time return-quote snapshot.
ALTER TABLE customer_returns
  DROP COLUMN IF EXISTS return_shipping_quote_groups,
  DROP COLUMN IF EXISTS return_shipping_quote_source,
  DROP COLUMN IF EXISTS return_shipping_quote_at,
  DROP COLUMN IF EXISTS return_shipping_quote_toman;
ALTER TABLE platform_settings
  DROP COLUMN IF EXISTS tapin_platform_shop_id,
  DROP COLUMN IF EXISTS tapin_platform_token;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS tapin_default_box_id;
