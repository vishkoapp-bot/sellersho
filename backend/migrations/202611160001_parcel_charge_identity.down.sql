DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM parcel_charge_receipts) OR EXISTS(SELECT 1 FROM supplier_return_financial_events WHERE charge_component IS NOT NULL) THEN
 RAISE EXCEPTION 'parcel charge evidence exists; down migration refused'; END IF;
END $$;
DROP TRIGGER parcel_charge_events_immutable ON supplier_return_financial_events;
DROP TABLE parcel_charge_keys;
DROP TABLE parcel_charge_receipts;
DROP FUNCTION parcel_charge_append_only();
DROP INDEX supplier_parcel_original_charge;
ALTER TABLE supplier_return_financial_events DROP CONSTRAINT parcel_charge_component;
ALTER TABLE supplier_return_financial_events DROP COLUMN charge_component;
