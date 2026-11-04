-- 202611040011 (Fix69 A4c): ledger_entries.target_type gains 'warehouse'.
-- ResolveDispute maps refund_warehouse (and the warehouse party labels)
-- to target_type 'warehouse' instead of silently defaulting to carrier,
-- but the initial-schema CHECK only allows seller/supplier/carrier/
-- platform, so without this migration a warehouse resolution would fail
-- on the CHECK. The constraint is dropped by looked-up name (not a
-- hardcoded name) and re-added with the extended set, so existing rows
-- (all in the old set) stay valid. Idempotent on re-apply: the DO block
-- re-adds the same constraint name each time.
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
    CHECK (target_type IN ('seller', 'supplier', 'carrier', 'platform', 'warehouse'));
END $$;
