-- 202610210001: widen the audit_logs.category CHECK to the full set the
-- platform code actually writes (audit-filter wave finding, Oct 21 2026).
--
-- The constraint on prod (000005 as amended by 000025) allows only 10 values:
-- commission, catalog, store_status, warehouse, warehouse_capacity, finance,
-- subscription, dispute, supplier, auth. But the backend also writes 6 more:
--   stores    - admin_handlers (store_review, store_margin_lock)
--   users     - admin_handlers (user_active_status)
--   staff     - staff_handlers (staff_create, staff_update, staff_delete)
--   settings  - ippanel_handlers (sms_settings_update)
--   approvals - admin_handlers (approval_review)
--   suppliers - supplier_handlers (supplier_approve, supplier_reject)
-- Those inserts are rejected with SQLSTATE 23514 and auditAdminAction is
-- best-effort (it swallows the error), so the audit rows were silently
-- dropped: prod audit_logs holds only 3 rows, all 'finance' (verified by
-- read-only prod queries, Oct 21 2026; >=7 supplier decisions provably lost
-- their audit rows). The rows already lost are NOT recoverable; this
-- migration stops the ongoing loss.
--
-- The complete written set was verified against every INSERT INTO audit_logs
-- and audit helper call site in the codebase (admin_repo.GetAuditLogs matches
-- category by exact equality): exactly these 16 values. The union below
-- keeps all 10 existing values, so all existing rows stay valid.

ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_category_check;
ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_category_check
    CHECK (category IN ('approvals', 'auth', 'catalog', 'commission', 'dispute', 'finance', 'settings', 'staff', 'store_status', 'stores', 'subscription', 'supplier', 'suppliers', 'users', 'warehouse', 'warehouse_capacity'));
