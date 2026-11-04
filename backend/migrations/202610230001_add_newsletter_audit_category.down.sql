-- Down for 202610230001 (audit_logs category CHECK): restore the 16-value
-- CHECK from migration 202610210001.
--
-- Fail-closed by design (same pattern as the 202610210001 down migration):
-- the rollback is blocked while audit records exist in the 'newsletter'
-- category, instead of failing obscurely or orphaning rows; archive those
-- records first, then roll back.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM audit_logs WHERE category IN ('newsletter')) THEN
    RAISE EXCEPTION '202610230001 rollback blocked: audit records exist in category newsletter';
  END IF;
  ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_category_check;
  ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_category_check
    CHECK (category IN ('approvals', 'auth', 'catalog', 'commission', 'dispute', 'finance', 'settings', 'staff', 'store_status', 'stores', 'subscription', 'supplier', 'suppliers', 'users', 'warehouse', 'warehouse_capacity'));
END $$;
