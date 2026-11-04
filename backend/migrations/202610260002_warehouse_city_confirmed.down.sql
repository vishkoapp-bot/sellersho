-- 202610260002 (down): remove the warehouse-city confirmation flag.
--
-- Drops ONLY the two added columns (and their CHECK constraint).
-- warehouse_city values are untouched — the down migration loses no
-- city data, it only drops the confirmation provenance.
ALTER TABLE suppliers
    DROP CONSTRAINT IF EXISTS suppliers_warehouse_city_source_check;

ALTER TABLE suppliers
    DROP COLUMN IF EXISTS warehouse_city_source;

ALTER TABLE suppliers
    DROP COLUMN IF EXISTS warehouse_city_confirmed;
