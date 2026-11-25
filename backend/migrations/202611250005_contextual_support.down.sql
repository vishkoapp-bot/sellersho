DROP INDEX IF EXISTS support_tickets_supplier_account_idx;

ALTER TABLE support_tickets
    DROP CONSTRAINT IF EXISTS support_tickets_context_check,
    DROP COLUMN IF EXISTS reference_label,
    DROP COLUMN IF EXISTS reference_id,
    DROP COLUMN IF EXISTS reference_type,
    DROP COLUMN IF EXISTS category,
    DROP COLUMN IF EXISTS requester_account_id;
