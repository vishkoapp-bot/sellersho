DROP TABLE IF EXISTS return_price_reviews;
DROP TABLE IF EXISTS tapin_tariff_surcharges;
DROP TABLE IF EXISTS tapin_tariff_rates;
DROP TABLE IF EXISTS tapin_tariff_versions;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS tapin_tariff_url;
