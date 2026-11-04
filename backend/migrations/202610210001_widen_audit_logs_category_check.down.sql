-- Down for 202610210001 (audit_logs category CHECK): restore the 10-value
-- CHECK from migration 000025.
--
-- Fail-closed by design (same pattern as the 000025 down migration):
-- the rollback is blocked while audit records exist in any of the 6
-- widened categories, instead of failing obscurely or orphaning rows;
-- archive those records first, then roll back.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM audit_logs WHERE category IN ('approvals', 'settings', 'staff', 'stores', 'suppliers', 'users')) THEN
    RAISE EXCEPTION '202610210001 rollback blocked: audit records exist in widened categories (approvals/settings/staff/stores/suppliers/users)';
  END IF;
  ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_category_check;
  ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_category_check CHECK (category IN (
    'commission', 'catalog', 'store_status', 'warehouse', 'warehouse_capacity',
    'finance', 'subscription', 'dispute', 'supplier', 'auth'
  ));
END $$;
