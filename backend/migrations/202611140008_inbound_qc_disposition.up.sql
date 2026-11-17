-- Additive only: never classify/debit historical QC or invent receipt provenance.
ALTER TABLE inventory_balances DROP CONSTRAINT IF EXISTS inventory_balances_status_check;
ALTER TABLE inventory_balances ADD CONSTRAINT inventory_balances_status_check CHECK (status IN ('physical','sellable','sales_reserved','return_requested','quarantine','in_transit','damaged','unreconciled','inbound_reserved','qc_accepted'));
-- Append-only subordinate event metadata, serialized by the existing shipment
-- work guard. No foreign keys introduce earlier implicit row guards.
CREATE TABLE inbound_qc_dispositions (
 id TEXT PRIMARY KEY,
 receipt_line_id TEXT NOT NULL,
 cumulative_disposed INT NOT NULL CHECK(cumulative_disposed>0),
 received_quantity INT NOT NULL CHECK(received_quantity>=cumulative_disposed),
 quantity_passed INT NOT NULL CHECK(quantity_passed>=0),
 quantity_failed INT NOT NULL CHECK(quantity_failed>=0),
 quantity_shortage INT NOT NULL CHECK(quantity_shortage>=0),
 passed_delta INT NOT NULL CHECK(passed_delta>=0),
 failed_delta INT NOT NULL CHECK(failed_delta>=0),
 reason TEXT NOT NULL DEFAULT '',
 actor_user_id TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK(quantity_passed::bigint+quantity_failed::bigint=cumulative_disposed),
 CHECK(passed_delta::bigint+failed_delta::bigint>0 AND passed_delta<=quantity_passed AND failed_delta<=quantity_failed),
 UNIQUE(receipt_line_id,cumulative_disposed)
);
CREATE FUNCTION inbound_qc_disposition_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'immutable inbound QC disposition';
END $$;
CREATE TRIGGER inbound_qc_disposition_identity BEFORE UPDATE OR DELETE ON inbound_qc_dispositions FOR EACH ROW EXECUTE FUNCTION inbound_qc_disposition_immutable();
