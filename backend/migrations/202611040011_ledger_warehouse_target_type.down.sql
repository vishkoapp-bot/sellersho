-- 202611040011 down: restore the pre-Fix69 ledger_entries target_type set.
DO $$
DECLARE
  cname text;
BEGIN
  SELECT conname INTO cname FROM pg_constraint
  WHERE conrelid = 'ledger_entries'::regclass AND contype = 'c'
    AND pg_get_constraintdef(oid) LIKE '%target_type%';
  IF cname IS NOT NULL THEN
    EXECUTE 'ALTER TABLE ledger_entries DROP CONSTRAINT ' || quote_ident(cname);
  END IF;
  ALTER TABLE ledger_entries ADD CONSTRAINT ledger_entries_target_type_check
    CHECK (target_type IN ('seller', 'supplier', 'carrier', 'platform'));
END $$;
