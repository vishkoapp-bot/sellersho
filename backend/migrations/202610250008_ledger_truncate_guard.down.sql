-- 202610250008_ledger_truncate_guard.down.sql
-- Reverts 202610250008: drops the statement-level TRUNCATE guard.
-- (The row-level UPDATE/DELETE guard from 202610160003 is untouched.)
-- NOTE: the guarded REVOKE in the up migration is not reversed here —
-- re-granting TRUNCATE to the app role is a deliberate security decision,
-- not a schema rollback.
DROP TRIGGER IF EXISTS trg_ledger_entries_no_truncate ON ledger_entries;
