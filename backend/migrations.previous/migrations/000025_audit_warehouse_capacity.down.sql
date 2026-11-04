-- Keep rollback explicit: existing warehouse_capacity audit records must be archived
-- before restoring the narrower legacy category constraint.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM audit_logs WHERE category = 'warehouse_capacity') THEN
    RAISE EXCEPTION '000025 rollback blocked: warehouse_capacity audit records exist';
  END IF;
  ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_category_check;
  ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_category_check CHECK (category IN (
    'commission', 'catalog', 'store_status', 'warehouse',
    'finance', 'subscription', 'dispute', 'supplier', 'auth'
  ));
END $$;