-- Allow the canonical warehouse capacity audit category used by WMS capacity mutations.
ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_category_check;
ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_category_check CHECK (category IN (
  'commission', 'catalog', 'store_status', 'warehouse', 'warehouse_capacity',
  'finance', 'subscription', 'dispute', 'supplier', 'auth'
));