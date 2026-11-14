-- New requests only. Historical documents/effects are not rewritten.
ALTER TABLE finance_adjustments ADD COLUMN IF NOT EXISTS request_key TEXT;
ALTER TABLE finance_adjustments ADD COLUMN IF NOT EXISTS request_fingerprint TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS finance_adjustment_request_identity
ON finance_adjustments(target_kind,target_id,request_key) WHERE request_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS finance_command_requests (
 principal_type TEXT NOT NULL CHECK(principal_type IN ('seller','supplier')),
 principal_id TEXT NOT NULL,
 operation TEXT NOT NULL,
 client_key TEXT NOT NULL CHECK(length(client_key) BETWEEN 1 AND 128),
 fingerprint TEXT NOT NULL,
 result JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(principal_type,principal_id,operation,client_key)
);

-- Sparse identity is populated only for newly posted parcel charges. Old rows
-- remain intact; the guarded writer recognizes their parcel identity too.
ALTER TABLE supplier_return_financial_events ADD COLUMN IF NOT EXISTS event_identity TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS supplier_return_charge_business_identity
ON supplier_return_financial_events(event_identity) WHERE event_identity IS NOT NULL;
