-- Rollback for 202610160008.

ALTER TABLE platform_settings DROP COLUMN IF EXISTS warehouse_tariffs;
