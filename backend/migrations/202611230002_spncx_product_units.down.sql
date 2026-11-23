DROP TABLE IF EXISTS spncx_failed_captures;
DROP TABLE IF EXISTS spncx_print_job_units;
DROP TABLE IF EXISTS spncx_print_jobs;
DROP TABLE IF EXISTS spncx_backfill_snapshot;
DROP TABLE IF EXISTS spncx_backfill_state;
DROP TABLE IF EXISTS order_item_units;
DROP TABLE IF EXISTS product_unit_events;
DROP TABLE IF EXISTS product_variant_unit_counters;
DROP TABLE IF EXISTS product_units;
ALTER TABLE inventory_balances DROP COLUMN IF EXISTS unit_tracked;
DELETE FROM inventory_balances WHERE status='sold' AND location_key LIKE 'spncx-sold:%';
ALTER TABLE inventory_balances DROP CONSTRAINT inventory_balances_status_check;
ALTER TABLE inventory_balances ADD CONSTRAINT inventory_balances_status_check
    CHECK (status IN ('physical','sellable','sales_reserved','return_requested','quarantine',
                      'in_transit','damaged','unreconciled','inbound_reserved','qc_accepted'));
