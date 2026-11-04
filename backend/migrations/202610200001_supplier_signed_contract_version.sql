-- Supplier contract versioning (parity with sellers/stores):
-- the signed contract version was only kept in the frontend session, so
-- platform contract updates never triggered re-confirmation for suppliers.
-- Existing suppliers accepted the onboarding terms (v2 era), so backfill to 2;
-- the re-confirmation modal fires only when the platform version is newer.
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS signed_contract_version INTEGER NOT NULL DEFAULT 1;
UPDATE suppliers SET signed_contract_version = 2 WHERE signed_contract_version < 2;
