-- 202610280010 DOWN: drop the packed-parcel packaging dimensions from
-- supplier_shipments (the values are a confirmation record of a
-- registration, not load-bearing for the shipment itself).
ALTER TABLE supplier_shipments
  DROP COLUMN IF EXISTS packaging_length_mm,
  DROP COLUMN IF EXISTS packaging_width_mm,
  DROP COLUMN IF EXISTS packaging_height_mm;
