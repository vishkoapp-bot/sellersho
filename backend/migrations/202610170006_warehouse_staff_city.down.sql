-- 202610170006_warehouse_staff_city: rollback.

ALTER TABLE users DROP COLUMN IF EXISTS warehouse_city;
