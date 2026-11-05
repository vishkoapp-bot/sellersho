-- Down for 202611050005: drop the storage-fee settings columns.
ALTER TABLE platform_settings
    DROP COLUMN IF EXISTS storage_fee_per_m3_day,
    DROP COLUMN IF EXISTS storage_fee_per_kg_day,
    DROP COLUMN IF EXISTS storage_fee_free_days,
    DROP COLUMN IF EXISTS storage_fee_enabled;
