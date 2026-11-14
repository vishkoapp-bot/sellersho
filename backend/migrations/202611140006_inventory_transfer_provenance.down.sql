-- Refuse destructive rollback while dedicated reservations exist.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM inbound_inventory_lines) THEN RAISE EXCEPTION 'provenance in use; rollback requires audited reconciliation'; END IF; END $$;
DROP VIEW inbound_transfer_reconciliation;
DROP TABLE inbound_inventory_lines;
DROP FUNCTION inbound_inventory_identity_immutable();
ALTER TABLE inventory_balances DROP CONSTRAINT inventory_balances_status_check;
ALTER TABLE inventory_balances ADD CONSTRAINT inventory_balances_status_check CHECK (status IN ('physical','sellable','sales_reserved','return_requested','quarantine','in_transit','damaged','unreconciled'));

ALTER TABLE shipments DROP CONSTRAINT shipments_status_check;
ALTER TABLE shipments ADD CONSTRAINT shipments_status_check CHECK(status IN ('draft','sent','received','partially_received'));
