-- 202610200001_supplier_signed_contract_version.down.sql
-- Manual rollback for supplier contract versioning.
-- Drops the signed-contract-version column added by the up migration;
-- suppliers fall back to session-only version tracking (the pre-202610200001
-- behavior), so platform contract updates stop triggering re-confirmation.

ALTER TABLE suppliers DROP COLUMN IF EXISTS signed_contract_version;
