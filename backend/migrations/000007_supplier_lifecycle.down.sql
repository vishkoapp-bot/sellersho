-- Rollback Supplier Lifecycle Schema Migration (000007)

DROP TABLE IF EXISTS qc_discrepancies CASCADE;
DROP TABLE IF EXISTS supplier_campaigns CASCADE;
DROP TABLE IF EXISTS supplier_pricing_tiers CASCADE;

ALTER TABLE inbound_shipments
    DROP COLUMN IF EXISTS expected_arrival,
    DROP COLUMN IF EXISTS notes,
    DROP COLUMN IF EXISTS items;

ALTER TABLE payout_requests
    DROP COLUMN IF EXISTS supplier_id;
