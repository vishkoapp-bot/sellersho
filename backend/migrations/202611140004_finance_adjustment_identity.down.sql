DROP INDEX IF EXISTS supplier_return_charge_business_identity;
ALTER TABLE supplier_return_financial_events DROP COLUMN IF EXISTS event_identity;
DROP INDEX IF EXISTS finance_adjustment_request_identity;
DROP TABLE IF EXISTS finance_command_requests;
ALTER TABLE finance_adjustments DROP COLUMN IF EXISTS request_fingerprint;
ALTER TABLE finance_adjustments DROP COLUMN IF EXISTS request_key;
