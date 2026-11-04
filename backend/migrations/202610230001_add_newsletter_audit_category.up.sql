-- 202610230001: add 'newsletter' to the audit_logs.category CHECK.
--
-- handleAdminNewsletterDelete (newsletter_handlers.go) audits admin
-- subscriber deletions with action 'newsletter_delete', but until now it
-- had to file them under the unrelated 'store_status' category: the closed
-- CHECK from 202610210001 (16 values) has no newsletter value, and filing
-- under 'newsletter' directly would be rejected with SQLSTATE 23514 while
-- auditAdminAction is best-effort (it swallows the error), silently
-- dropping the audit row. This migration adds the missing value so the
-- handler can log under its own category.
--
-- The set below is exactly the 202610210001 set plus 'newsletter'
-- (17 values); all existing values are kept, so all existing rows stay
-- valid.

ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_category_check;
ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_category_check
    CHECK (category IN ('approvals', 'auth', 'catalog', 'commission', 'dispute', 'finance', 'newsletter', 'settings', 'staff', 'store_status', 'stores', 'subscription', 'supplier', 'suppliers', 'users', 'warehouse', 'warehouse_capacity'));
