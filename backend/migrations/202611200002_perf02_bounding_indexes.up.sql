-- 202611200002: PERF-02 bounding indexes (audit-proven on disposable PG16).
--
-- PERF-02 audit (~/workspace/perf02-audit/PERF-02-audit.md §3b) measured
-- these 10 indexes (+ 7 cursor composites + 3 statement legs added in
-- Step 10, 20 total) on a 10,006-product / 20,001-order / 100,002-ledger-entry
-- seed and collapsed every worst path by 100-1000x with NO app-code change:
--   ledger by related_order_or_dispute  14.3 ms -> 0.026 ms
--   order_items by order_id              6.6 ms -> 0.027 ms
--   payment_transactions by order_id     3.3 ms -> 0.020 ms
--   seller report (unchanged SQL)       20.8 s  -> 21.5 ms
--   reconciliation report page 200       8.7 s  -> 28-47 ms
--   single-product review row           37.9 ms -> 0.91 ms
-- The origins cast join is NOT fixed by indexes alone (40-49 ms -> 27-28 ms);
-- the cast removal is a code change in finance_balance.go (PERF-02 Step 1).
--
-- Two deliberate deviations from the audit's literal candidate list:
--   * idx_orders_store_created carries an `id DESC` tiebreaker so the
--     PERF-02 cursor pages (ORDER BY created_at DESC, id DESC) are fully
--     index-ordered instead of sorting the filtered set.
--   * idx_inventory_reservations_product is (product_id, status, created_at)
--     (audit §4 Step 0 recommendation): it covers the report's
--     `status='active'` filter as well as the ordering.
--
-- CORRECTION (audit §3a, vs CAP-01 wording): payment_transactions(order_id)
-- is not simply "missing" — a partial unique index
-- (payment_transactions_one_active_per_order) exists but is unusable for a
-- bare order_id predicate, so a plain (order_id) index is still needed.

-- Release-cohort lookups (FIN-013 release path; also U1/U2 item loading).
CREATE INDEX IF NOT EXISTS idx_perf02_order_items_order_id
    ON order_items (order_id);
CREATE INDEX IF NOT EXISTS idx_perf02_ledger_related
    ON ledger_entries (related_order_or_dispute);
CREATE INDEX IF NOT EXISTS idx_perf02_payment_transactions_order_id
    ON payment_transactions (order_id);

-- Store order list (U2): was a full seq scan of orders.
CREATE INDEX IF NOT EXISTS idx_perf02_orders_store_created
    ON orders (store_id, created_at DESC, id DESC);

-- Reconciliation report laterals (U16): one index per nested evidence list.
CREATE INDEX IF NOT EXISTS idx_perf02_inventory_movements_product
    ON inventory_movements (product_id, created_at);
CREATE INDEX IF NOT EXISTS idx_perf02_inventory_reservations_product
    ON inventory_reservations (product_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_perf02_product_measurement_events_product
    ON product_measurement_events (product_id, created_at);
CREATE INDEX IF NOT EXISTS idx_perf02_historical_reconciliation_reviews_product
    ON historical_reconciliation_reviews (product_id);

-- Payout queues (U9/U10): status queue + per-store history.
CREATE INDEX IF NOT EXISTS idx_perf02_payout_requests_status
    ON payout_requests (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_payout_requests_store
    ON payout_requests (store_id, created_at DESC);

-- Cursor-pagination support for the newly bounded lists (PERF-02 Steps 3-7).
-- Each paged query orders by (created_at DESC, id DESC) — or (date DESC,
-- id DESC) for the ledger — inside one scope predicate; these composites
-- make the page selection index-ordered instead of sorting the filtered
-- set. Measured on the same disposable seed: ledger LIMIT 50 went from
-- ~29 ms (sort of 100,002 rows) to <1 ms; shipments LIMIT 50 from ~11 ms
-- to <1 ms (see ~/workspace/perf02-impl/measure2.out).
CREATE INDEX IF NOT EXISTS idx_perf02_ledger_date
    ON ledger_entries (date DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_shipments_supplier_created
    ON supplier_shipments (supplier_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_tickets_requester_created
    ON support_tickets (requester_user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_suppliers_created
    ON suppliers (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_returns_created
    ON customer_returns (created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_returns_customer_created
    ON customer_returns (customer_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_monthly_items_period
    ON monthly_settlement_items (cycle_period, created_at DESC, id DESC);

-- Statement page legs (PERF-02 Step 10, measured at 100k-order scale):
-- the 11-leg statement UNION runs 3+ times per page and the Step-0
-- indexes alone left it at 43-97 ms warm (no-index 106-197 ms). EXPLAIN
-- showed two legs seq-scanning: the released_profit leg (orders by
-- store_id, planner prefers a parallel seq scan over the (store_id,
-- created_at) index — the leg orders by profit_released_at) and the
-- finance_adjustment leg (ledger_entries has no target_account index).
-- The referral_commission leg (referrer_seller_id) seq-scanned too
-- (19-52 ms per totals pass). These three indexes — a partial index on
-- released orders, a target_account composite, and a partial index on
-- released referral commissions — collapse the probe to 8-10 ms warm at
-- 100,005 orders / 5,830 Oct-2026 released rows / 2,000 adjustments.
-- No summary table is needed; the FIN-009-adjacent decision is NOT
-- triggered (see ~/workspace/perf02-impl/measure_statement_100k.out).
CREATE INDEX IF NOT EXISTS idx_perf02_orders_store_released
    ON orders (store_id, profit_released_at DESC, id DESC)
    WHERE profit_released;
CREATE INDEX IF NOT EXISTS idx_perf02_ledger_target
    ON ledger_entries (target_account, target_type, date DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_perf02_orders_referrer
    ON orders (referrer_seller_id, profit_released_at DESC, id DESC)
    WHERE referral_commission_released;
