-- Dimensions provenance + volumetric divisor + leg volumetric weight
-- (2026-10-28, addendum §7). Dimension columns themselves already
-- exist (migration 000024: length_mm/width_mm/height_mm/weight_grams/
-- measurement_status/measurement_version + product_measurement_events)
-- so this migration only adds what is genuinely new:
--
-- (1) products.measurement_source — WHO last wrote the measurements:
--     'supplier_declared' (proposal), 'warehouse_measured' (authoritative
--     warehouse measurement), 'tapin' (RESERVED — no writer exists;
--     the Tapin client has no product lookup, addendum §1).
--     NULL = nothing ever recorded (same as today).
-- (2) platform_settings.volumetric_divisor — cm³ per kilogram for the
--     volumetric-weight rule (chargeable = max(actual, volumetric)).
--     Default 5000 (air standard, addendum §4.2 open question 1);
--     <= 0 disables volumetric weight (fail-safe: actual weight only).
--     Tunable without a deploy. Unit is in the column comment, not just
--     the admin label (the ×10 lesson: a number without a unit is a trap).
-- (3) order_shipping_legs.volumetric_weight_grams — the volumetric
--     weight that fed the max() for this leg (audit detail: WHY the leg
--     costs what it costs). leg.weight_grams is the CHARGEABLE weight
--     (max(actual, volumetric)); NULL here means "no volumetric weight
--     applied" (no dimensions, or divisor disabled).
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS measurement_source VARCHAR(20)
    CHECK (measurement_source IS NULL OR measurement_source IN
      ('supplier_declared', 'warehouse_measured', 'tapin'));

ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS volumetric_divisor INTEGER NOT NULL DEFAULT 5000
    CHECK (volumetric_divisor >= 0);
-- NOTE (deviation from the addendum §7 literal SQL, which wrote
-- CHECK (volumetric_divisor IS NULL OR volumetric_divisor > 0)): that
-- CHECK contradicts the addendum's own §4.2/§8 rule "divisor <= 0
-- disables volumetric weight" — a >0 CHECK makes 0 unsettable. Here
-- 0 IS allowed and means "volumetric weight disabled" (fail-safe:
-- actual weight only); the Go read path treats <= 0 as disabled.

ALTER TABLE order_shipping_legs
  ADD COLUMN IF NOT EXISTS volumetric_weight_grams INTEGER
    CHECK (volumetric_weight_grams IS NULL OR volumetric_weight_grams > 0);
