-- Refuse destructive rollback after any accepted disposition, even all-failed.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM inbound_qc_dispositions) OR EXISTS(SELECT 1 FROM inventory_balances WHERE status='qc_accepted') THEN
  RAISE EXCEPTION 'inbound QC evidence requires forward reconciliation';
 END IF;
END $$;
DROP TABLE inbound_qc_dispositions;
DROP FUNCTION inbound_qc_disposition_immutable();
ALTER TABLE inventory_balances DROP CONSTRAINT IF EXISTS inventory_balances_status_check;
ALTER TABLE inventory_balances ADD CONSTRAINT inventory_balances_status_check CHECK (status IN ('physical','sellable','sales_reserved','return_requested','quarantine','in_transit','damaged','unreconciled','inbound_reserved'));
