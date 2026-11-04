-- Down for 202610280002: drop the three capacity/receiving columns.
-- No other data is affected.
ALTER TABLE warehouses
  DROP COLUMN IF EXISTS capacity_units_total,
  DROP COLUMN IF EXISTS capacity_m3_total,
  DROP COLUMN IF EXISTS is_receiving_open;
