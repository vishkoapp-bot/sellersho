-- Migration 202610160008: warehouse tariffs persistence (Group A audit).
--
-- The super-admin "Warehouses" tab had a warehouse tariff form (inbound fee
-- per carton, storage fee per m3/day, picking & packing fee, depot buffer
-- multiplier, per-category base depot quotas) that only lived in React
-- state: it claimed "saved to the system" but vanished on reload and
-- affected nothing. This column makes the tariffs real: GET/PUT
-- /api/v1/admin/settings/warehouse-tariffs reads/writes this JSONB blob.
-- No billing logic reads it yet; it is the persisted source of truth for
-- the admin UI until settlement deductions are wired to it.

ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS warehouse_tariffs JSONB NOT NULL DEFAULT '{}'::jsonb;
