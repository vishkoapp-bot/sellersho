-- 202610250008_ledger_truncate_guard.up.sql
-- Money-fix W1 (2026-10-25), P0-7 / audit M-P0-5 (ledger F2):
-- TRUNCATE TABLE ledger_entries bypassed the append-only protection because
-- the existing trigger is row-level (BEFORE UPDATE OR DELETE ... FOR EACH
-- ROW) and row-level triggers never fire on TRUNCATE. A statement-level
-- BEFORE TRUNCATE trigger reuses the same guard function and raises the
-- same exception, so the whole financial history cannot be wiped silently.
--
-- Defense in depth: REVOKE TRUNCATE from the application role when it
-- exists. The DO block guards the revoke so a missing role (deployments
-- that connect under a different role name) never fails the migration.
-- The trigger above is the enforcement either way: it fires for every
-- session, including the table owner (REVOKE alone cannot bind an owner).
CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only: % is not allowed (use a reversing journal entry instead)', TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_ledger_entries_no_truncate ON ledger_entries;

CREATE TRIGGER trg_ledger_entries_no_truncate
    BEFORE TRUNCATE ON ledger_entries
    FOR EACH STATEMENT
    EXECUTE FUNCTION prevent_ledger_mutation();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'drup_admin') THEN
        EXECUTE 'REVOKE TRUNCATE ON ledger_entries FROM drup_admin';
    END IF;
END $$;
