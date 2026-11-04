-- 202610260002: Supplier warehouse-city confirmation flag (L-04 step B, merged D-06).
--
-- Problem: suppliers.warehouse_city is NOT NULL DEFAULT 'تهران', so every
-- supplier reads as "Tehran" — including test rows that never declared a
-- city. Any surface that renders the city (origin badge, returns-warehouse
-- choice, admin lists, shipping legs) was presenting the column default
-- as fact (constitution art. 11 / art. 12).
--
-- This migration adds the provenance flag only; it changes NO city value:
--   warehouse_city_confirmed BOOLEAN NOT NULL DEFAULT FALSE
--   warehouse_city_source     VARCHAR(20) NULL, 'declared' | 'default'
--
-- Honest reset: every existing row gets confirmed = FALSE and
-- source = 'default'. The data cannot tell which 'تهران' was really
-- declared, so "we don't know" is recorded instead of confirming it.
-- The flag becomes TRUE only when a supplier or an admin EXPLICITLY
-- declares/edits the city (registration with an explicit city, or a
-- profile edit) — the repo sets confirmed=TRUE + source='declared'
-- on those two paths only.
--
-- Deliberately NOT done here (separate decision): removing
-- DEFAULT 'تهران' from the column. Old rows and the seed depend on it,
-- and the registration form no longer sends an empty city, so the
-- default cannot silently land on new rows anymore.
ALTER TABLE suppliers
    ADD COLUMN IF NOT EXISTS warehouse_city_confirmed BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE suppliers
    ADD COLUMN IF NOT EXISTS warehouse_city_source VARCHAR(20);

-- No IF NOT EXISTS for ADD CONSTRAINT: guard it so the file stays
-- re-runnable (the migrate binary re-applies every up file on each run).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'suppliers_warehouse_city_source_check'
    ) THEN
        ALTER TABLE suppliers
            ADD CONSTRAINT suppliers_warehouse_city_source_check
            CHECK (warehouse_city_source IS NULL OR warehouse_city_source IN ('declared', 'default'));
    END IF;
END $$;

-- Honest reset (L-04 §8.1 / D-06 §2): no existing 'تهران' is confirmed —
-- the rows predate any explicit declaration, so all of them are
-- 'default'-sourced until their owner (or an admin) declares the city.
UPDATE suppliers
    SET warehouse_city_confirmed = FALSE,
        warehouse_city_source = 'default';
