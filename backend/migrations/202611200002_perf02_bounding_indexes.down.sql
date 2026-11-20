-- Down migration for 202611200002 (PERF-02 bounding indexes).
-- Drops only the PERF-02 indexes. The paged handlers keep working without
-- them (they degrade to seq scans / sorts); the release cohort regresses
-- to pre-PERF-02 per-candidate cost. Only run on a disposable database.
DROP INDEX IF EXISTS idx_perf02_orders_referrer;
DROP INDEX IF EXISTS idx_perf02_ledger_target;
DROP INDEX IF EXISTS idx_perf02_orders_store_released;
DROP INDEX IF EXISTS idx_perf02_monthly_items_period;
DROP INDEX IF EXISTS idx_perf02_returns_customer_created;
DROP INDEX IF EXISTS idx_perf02_returns_created;
DROP INDEX IF EXISTS idx_perf02_suppliers_created;
DROP INDEX IF EXISTS idx_perf02_tickets_requester_created;
DROP INDEX IF EXISTS idx_perf02_shipments_supplier_created;
DROP INDEX IF EXISTS idx_perf02_ledger_date;
DROP INDEX IF EXISTS idx_perf02_payout_requests_store;
DROP INDEX IF EXISTS idx_perf02_payout_requests_status;
DROP INDEX IF EXISTS idx_perf02_historical_reconciliation_reviews_product;
DROP INDEX IF EXISTS idx_perf02_product_measurement_events_product;
DROP INDEX IF EXISTS idx_perf02_inventory_reservations_product;
DROP INDEX IF EXISTS idx_perf02_inventory_movements_product;
DROP INDEX IF EXISTS idx_perf02_orders_store_created;
DROP INDEX IF EXISTS idx_perf02_payment_transactions_order_id;
DROP INDEX IF EXISTS idx_perf02_ledger_related;
DROP INDEX IF EXISTS idx_perf02_order_items_order_id;
