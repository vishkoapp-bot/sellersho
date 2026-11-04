-- Down migration for 202610250005: restore the old (misleading) column name.
-- Orphan rows deleted by the up migration are not restored.
ALTER TABLE supplier_shipping_methods RENAME COLUMN owner_user_id TO supplier_id;
ALTER INDEX IF EXISTS idx_ssm_owner_user RENAME TO idx_ssm_supplier;
