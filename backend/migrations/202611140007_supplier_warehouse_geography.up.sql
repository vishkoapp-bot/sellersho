-- Existing Tapin origin readers require these fields on supplier warehouses.
-- Match central warehouse ID types. Unknown historical geography stays NULL;
-- no provider IDs are inferred from names and no live values are overwritten.
ALTER TABLE supplier_warehouses
  ADD COLUMN IF NOT EXISTS tapin_province_id INTEGER,
  ADD COLUMN IF NOT EXISTS tapin_city_id INTEGER;
