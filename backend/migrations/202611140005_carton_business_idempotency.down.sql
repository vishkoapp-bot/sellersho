DROP VIEW IF EXISTS carton_creation_reconciliation;
DROP INDEX IF EXISTS carton_variant_unique;
DROP INDEX IF EXISTS carton_creation_key_unique;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS carton_creation_identity_complete;
ALTER TABLE shipments DROP COLUMN IF EXISTS request_fingerprint;
ALTER TABLE shipments DROP COLUMN IF EXISTS creation_key;
