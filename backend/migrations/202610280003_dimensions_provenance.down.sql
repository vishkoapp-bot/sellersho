-- Down for 202610280003: drop the three provenance/divisor columns.
-- Dimension values themselves (000024 columns) are untouched.
ALTER TABLE order_shipping_legs DROP COLUMN IF EXISTS volumetric_weight_grams;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS volumetric_divisor;
ALTER TABLE products DROP COLUMN IF EXISTS measurement_source;
