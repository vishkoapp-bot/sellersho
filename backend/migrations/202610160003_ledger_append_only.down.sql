-- Rollback for 202610160003: remove append-only protection from ledger_entries.

DROP TRIGGER IF EXISTS trg_ledger_entries_append_only ON ledger_entries;
DROP FUNCTION IF EXISTS prevent_ledger_mutation();
