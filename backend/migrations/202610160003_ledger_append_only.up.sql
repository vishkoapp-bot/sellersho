-- Migration 202610160003: DB-level append-only protection for ledger_entries.
-- The double-entry ledger is immutable by law of the system: any UPDATE or
-- DELETE on ledger_entries raises an exception. Corrections are made only via
-- reversing journal entries (see finance.PostBalancedJournal), never by
-- mutating history.

CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only: % is not allowed (use a reversing journal entry instead)', TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_ledger_entries_append_only ON ledger_entries;

CREATE TRIGGER trg_ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW
    EXECUTE FUNCTION prevent_ledger_mutation();
