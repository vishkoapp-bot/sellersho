-- Storage-fee settings (gap 7): the owner's recorded Oct 30 decision —
-- storage fees exist as a system with tariffs defaulting to 2,000 Toman
-- per kg per day + 8,000 Toman per m3 per day and 30 free days, and the
-- feature ships with its flag DEFAULT OFF; activation is a separate
-- owner decision. The assessment engine (repository
-- RunStorageFeeAssessment) reads these columns; the charges themselves
-- land in warehouse_storage_fee_charges (migration 202610280005) and
-- are deducted from the supplier balance on read. All amounts are
-- integer Tomans.
ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS storage_fee_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS storage_fee_free_days INT NOT NULL DEFAULT 30,
    ADD COLUMN IF NOT EXISTS storage_fee_per_kg_day BIGINT NOT NULL DEFAULT 2000,
    ADD COLUMN IF NOT EXISTS storage_fee_per_m3_day BIGINT NOT NULL DEFAULT 8000;
