-- FIN-014 legacy defect fixture (synthetic only — NOT production history).
--
-- One loadable dataset with every audit §2 defect class, every
-- ambiguous case, and a negative control per class, plus the exact
-- expected report (see fin014_fixture_test.go: fin014- prefix scope
-- certain 10 / ambiguous 4 / unresolved 4, 18 rows; class 5 is
-- asserted by lot-id scope — one unresolved row, zero for the
-- verified lot).
--
-- All IDs carry the fin014- prefix (the Go loader replaces it with a
-- per-test prefix so each test loads an independent copy). All rows
-- are synthetic: dummy customer/owner names, the 09000000000 phone,
-- synthetic:true metadata where the schema allows it.
--
-- The Go loader splits this file on the part-two separator line
-- below and executes each part in its own transaction, because the
-- FIN-005 deferred consistency triggers read the CURRENT
-- order/dispute state at commit time:
--   part 1: base rows; disputes inserted PENDING with their evidence
--           rows while their orders are delivery_disputed;
--   part 2: disputes resolved (+ decision rows + the one dispute
--           refund obligation), orders moved to delivered_confirmed.
-- A single transaction cannot do both: resolving a dispute in the
-- same transaction that inserts it would make the evidence check see
-- the final order status and fail.
--
-- SHAPES THE CURRENT SCHEMA FORBIDS (detectors still cover them for
-- pre-fence legacy data, but the fixture cannot seed them — see the
-- test file notes):
--   * duplicate cancellation obligations (UNIQUE
--     (purpose,business_id,source_payment_id) on
--     order_refund_obligations);
--   * refund-verdict dispute with no obligation (FIN-005
--     decision_consistency requires obligations == claim);
--   * duplicate / non-positive storage charges (UNIQUE
--     (position_id,fee_date) + CHECK amount>0 on
--     warehouse_storage_fee_charges), and any receipt lot or fee
--     charge seeded by plain SQL (guard_storage_receipt_lot_insert
--     requires a full inbound-QC chain; guard_storage_daily_charge
--     requires a VERIFIED lot — the lots are built by the Go helper
--     seedFIN014StorageLots instead).
-- The fixture therefore covers those branches with the detector only.
--
-- No production data, no OTP, no secret. Amounts in Toman (FIN-012).
-- Writer fences are honoured: the FIN-005 / FIN-007 writer flags are
-- set transaction-locally (same pattern as the CAP-01 fixture), which
-- is what the app's own writers do — the fixture inserts legacy rows
-- through the fenced writers, never by disabling a trigger.

SELECT set_config('sellersho.fin005_writer','v1',true);
SELECT set_config('sellersho.fin007_writer','v1',true);

-- Base rows -----------------------------------------------------------
INSERT INTO suppliers (id, name, warehouse_city) VALUES
  ('fin014-supplier-1', 'Fin014 Synthetic Supplier', 'Qom');
INSERT INTO products (id, title, slug, supplier_id, original_price, supplier_base_cost, platform_margin, stock)
  VALUES ('fin014-product-1', 'Fin014 Synthetic Product', 'fin014-product-1', 'fin014-supplier-1', 100000, 80000, 10000, 100);
INSERT INTO stores (id, name, slug, owner_type, owner_name) VALUES
  ('fin014-store-a', 'Fin014 Store A', 'fin014-store-a', 'dropshipper', 'Fin014 Seller A'),
  ('fin014-store-b', 'Fin014 Store B', 'fin014-store-b', 'dropshipper', 'Fin014 Seller B'),
  ('fin014-store-c', 'Fin014 Store C', 'fin014-store-c', 'dropshipper', 'Fin014 Seller C'),
  ('fin014-store-d', 'Fin014 Store D', 'fin014-store-d', 'dropshipper', 'Fin014 Seller D');
INSERT INTO warehouses (id, name, tapin_province_id, tapin_city_id, province_name, city_name)
  VALUES ('fin014-warehouse-1', 'Fin014 Warehouse', 1, 1, 'Qom', 'Qom');

-- Orders --------------------------------------------------------------
-- Columns used: id, order_number, store_id, store_name, totals,
-- payment_status, status, tracking_code, profit fields, release
-- fields, window, disputed_at, created_at.
-- The d4 orders WERE delivered and confirmed before the dispute
-- (confirmed_at = shipped_at 2026-10-28, inside the window ending
-- 2026-11-01): the dispute held the profit, not the delivery. The
-- evidence rows carry the same prior values, so the FIN-014 repair
-- can run the staged release path; a NULL confirmation here would
-- make the release honestly refuse (confirmation_window_missing).
INSERT INTO orders (id, order_number, store_id, store_name, total_amount, discount_amount, payable_amount, payment_method,
  customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code,
  status, tracking_code, shipping_cost, dropshipper_profit, platform_profit, profit_released, profit_released_at,
  shipped_at, confirmed_at, disputed_at, return_window_ends_at, payment_status, created_at, updated_at,
  discount_code, seller_discount_amount, coupon_discount_amount) VALUES
  ('fin014-order-a1', 'FIN014-A1', 'fin014-store-a', 'Fin014 Store A', 5000000, 0, 5000000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'delivered_confirmed', 'FIN014-TRK-A1', 0, 1000000, 500000, TRUE, '2026-09-20 10:00:00+00', '2026-09-17 10:00:00+00',
   '2026-09-18 10:00:00+00', NULL, '2026-09-27 10:00:00+00', 'paid', '2026-09-15 10:00:00+00', '2026-09-20 10:00:00+00', NULL, 0, 0),
  ('fin014-order-b1', 'FIN014-B1', 'fin014-store-b', 'Fin014 Store B', 4000000, 0, 4000000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'delivered_confirmed', 'FIN014-TRK-B1', 0, 800000, 400000, TRUE, '2026-11-02 10:00:00+00', '2026-10-30 10:00:00+00',
   '2026-11-01 10:00:00+00', NULL, '2026-11-08 10:00:00+00', 'paid', '2026-10-29 10:00:00+00', '2026-11-02 10:00:00+00', NULL, 0, 0),
  ('fin014-order-d4a', 'FIN014-D4A', 'fin014-store-c', 'Fin014 Store C', 1000000, 0, 1000000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'delivery_disputed', 'FIN014-TRK-D4A', 0, 250000, 100000, FALSE, NULL, '2026-10-28 10:00:00+00',
   '2026-10-28 10:00:00+00', '2026-11-02 10:00:00+00', '2026-11-01 10:00:00+00', 'paid', '2026-10-25 10:00:00+00', '2026-11-02 10:00:00+00', NULL, 0, 0),
  ('fin014-order-d4b', 'FIN014-D4B', 'fin014-store-c', 'Fin014 Store C', 800000, 0, 800000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'delivery_disputed', 'FIN014-TRK-D4B', 0, 100000, 80000, FALSE, NULL, '2026-10-28 10:00:00+00',
   '2026-10-28 10:00:00+00', '2026-11-02 10:00:00+00', '2026-11-01 10:00:00+00', 'paid', '2026-10-25 10:00:00+00', '2026-11-02 10:00:00+00', NULL, 0, 0),
  ('fin014-order-d4c', 'FIN014-D4C', 'fin014-store-c', 'Fin014 Store C', 1000000, 0, 1000000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'delivery_disputed', 'FIN014-TRK-D4C', 0, 200000, 100000, FALSE, NULL, '2026-10-28 10:00:00+00',
   '2026-10-28 10:00:00+00', '2026-11-02 10:00:00+00', '2026-11-01 10:00:00+00', 'paid', '2026-10-25 10:00:00+00', '2026-11-02 10:00:00+00', NULL, 0, 0),
  ('fin014-order-d7', 'FIN014-D7', 'fin014-store-c', 'Fin014 Store C', 400000, 0, 400000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'delivery_disputed', 'FIN014-TRK-D7', 0, 400000, 0, FALSE, NULL, '2026-10-28 10:00:00+00',
   NULL, '2026-11-02 10:00:00+00', '2026-11-01 10:00:00+00', 'paid', '2026-10-25 10:00:00+00', '2026-11-02 10:00:00+00', NULL, 0, 0),
  ('fin014-order-d7b', 'FIN014-D7B', 'fin014-store-c', 'Fin014 Store C', 1500000, 0, 1500000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'shipped', 'FIN014-TRK-D7B', 0, 300000, 150000, TRUE, '2026-11-06 10:00:00+00', '2026-11-03 10:00:00+00',
   NULL, NULL, '2026-11-10 10:00:00+00', 'paid', '2026-11-01 10:00:00+00', '2026-11-06 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c2m', 'FIN014-C2M', 'fin014-store-d', 'Fin014 Store D', 500000, 0, 500000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'cancelled', 'FIN014-TRK-C2M', 0, 100000, 50000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-21 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c2u', 'FIN014-C2U', 'fin014-store-d', 'Fin014 Store D', 500000, 0, 500000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'cancelled', 'FIN014-TRK-C2U', 0, 100000, 50000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-21 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c2c', 'FIN014-C2C', 'fin014-store-d', 'Fin014 Store D', 500000, 0, 500000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'cancelled', 'FIN014-TRK-C2C', 0, 100000, 50000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-21 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c8a', 'FIN014-C8A', 'fin014-store-d', 'Fin014 Store D', 300000, 0, 300000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-C8A', 0, 60000, 30000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c8b', 'FIN014-C8B', 'fin014-store-d', 'Fin014 Store D', 300000, 0, 300000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-C8B', 0, 60000, 30000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c8c', 'FIN014-C8C', 'fin014-store-d', 'Fin014 Store D', 300000, 0, 300000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-C8C', 0, 60000, 30000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c8d', 'FIN014-C8D', 'fin014-store-d', 'Fin014 Store D', 300000, 0, 300000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-C8D', 0, 60000, 30000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', NULL, 0, 0),
  ('fin014-order-c8e', 'FIN014-C8E', 'fin014-store-d', 'Fin014 Store D', 300000, 0, 300000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-C8E', 0, 60000, 30000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', NULL, 0, 0),
  ('fin014-coupon-order-1', 'FIN014-CP1', 'fin014-store-d', 'Fin014 Store D', 100000, 0, 100000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-CP1', 0, 20000, 10000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'pending', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', 'FIN014ONE', 10000, 5000),
  ('fin014-coupon-order-2', 'FIN014-CP2', 'fin014-store-d', 'Fin014 Store D', 100000, 0, 100000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-CP2', 0, 20000, 10000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'pending', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', 'FIN014ONE', 10000, 5000),
  ('fin014-coupon-order-3', 'FIN014-CP3', 'fin014-store-d', 'Fin014 Store D', 100000, 0, 100000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-CP3', 0, 20000, 10000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'pending', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', 'FIN014TWO', 10000, 5000),
  ('fin014-coupon-order-4', 'FIN014-CP4', 'fin014-store-d', 'Fin014 Store D', 100000, 0, 100000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-CP4', 0, 20000, 10000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'pending', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', 'FIN014THREE', 10000, 5000),
  ('fin014-coupon-order-5', 'FIN014-CP5', 'fin014-store-d', 'Fin014 Store D', 100000, 0, 100000, 'cash',
   'Fin014 Synthetic', '09000000000', 'Qom', 'Qom', 'Fin014 synthetic address', '0000000000',
   'processing', 'FIN014-TRK-CP5', 0, 20000, 10000, FALSE, NULL, NULL,
   NULL, NULL, NULL, 'paid', '2026-10-20 10:00:00+00', '2026-10-20 10:00:00+00', 'FIN014THREE', 10000, 5000);

-- Payments (gateway chains: payment + verification + receipt) --------
-- Helper shape, repeated per order: payment (paid), verification
-- (gateway), receipt (gateway). Simulated chain for c8d only.
INSERT INTO payment_transactions (id, order_id, order_number, gateway_token, rrn, amount, status) VALUES
  ('fin014-pay-a1', 'fin014-order-a1', 'FIN014-A1', 'fin014-token-a1', 'FIN014-RRN-A1', 5000000, 'paid'),
  ('fin014-pay-b1', 'fin014-order-b1', 'FIN014-B1', 'fin014-token-b1', 'FIN014-RRN-B1', 4000000, 'paid'),
  ('fin014-pay-d4a', 'fin014-order-d4a', 'FIN014-D4A', 'fin014-token-d4a', 'FIN014-RRN-D4A', 1000000, 'paid'),
  ('fin014-pay-d4b', 'fin014-order-d4b', 'FIN014-D4B', 'fin014-token-d4b', 'FIN014-RRN-D4B', 800000, 'paid'),
  ('fin014-pay-d4c', 'fin014-order-d4c', 'FIN014-D4C', 'fin014-token-d4c', 'FIN014-RRN-D4C', 1000000, 'paid'),
  ('fin014-pay-d7', 'fin014-order-d7', 'FIN014-D7', 'fin014-token-d7', 'FIN014-RRN-D7', 400000, 'paid'),
  ('fin014-pay-d7b', 'fin014-order-d7b', 'FIN014-D7B', 'fin014-token-d7b', 'FIN014-RRN-D7B', 1500000, 'paid'),
  ('fin014-pay-c2m', 'fin014-order-c2m', 'FIN014-C2M', 'fin014-token-c2m', 'FIN014-RRN-C2M', 500000, 'paid'),
  ('fin014-pay-c2u', 'fin014-order-c2u', 'FIN014-C2U', 'fin014-token-c2u', 'FIN014-RRN-C2U', 500000, 'paid'),
  ('fin014-pay-c2c', 'fin014-order-c2c', 'FIN014-C2C', 'fin014-token-c2c', 'FIN014-RRN-C2C', 500000, 'paid'),
  ('fin014-pay-c8b', 'fin014-order-c8b', 'FIN014-C8B', 'fin014-token-c8b', 'FIN014-RRN-C8B', 300000, 'paid'),
  ('fin014-pay-c8c', 'fin014-order-c8c', 'FIN014-C8C', 'fin014-token-c8c', 'FIN014-RRN-C8C', 300000, 'paid'),
  ('fin014-pay-c8d', 'fin014-order-c8d', 'FIN014-C8D', 'fin014-token-c8d', 'FIN014-RRN-C8D', 300000, 'paid'),
  ('fin014-pay-c8e', 'fin014-order-c8e', 'FIN014-C8E', 'fin014-token-c8e', 'FIN014-RRN-C8E', 300000, 'paid'),
  ('fin014-pay-cp5', 'fin014-coupon-order-5', 'FIN014-CP5', 'fin014-token-cp5', 'FIN014-RRN-CP5', 100000, 'paid');
INSERT INTO order_payment_verifications (id, payment_id, order_id, customer_id, amount_toman, currency, funding_kind) VALUES
  ('fin014-ver-a1', 'fin014-pay-a1', 'fin014-order-a1', NULL, 5000000, 'TOMAN', 'gateway'),
  ('fin014-ver-b1', 'fin014-pay-b1', 'fin014-order-b1', NULL, 4000000, 'TOMAN', 'gateway'),
  ('fin014-ver-d4a', 'fin014-pay-d4a', 'fin014-order-d4a', NULL, 1000000, 'TOMAN', 'gateway'),
  ('fin014-ver-d4b', 'fin014-pay-d4b', 'fin014-order-d4b', NULL, 800000, 'TOMAN', 'gateway'),
  ('fin014-ver-d4c', 'fin014-pay-d4c', 'fin014-order-d4c', NULL, 1000000, 'TOMAN', 'gateway'),
  ('fin014-ver-d7', 'fin014-pay-d7', 'fin014-order-d7', NULL, 400000, 'TOMAN', 'gateway'),
  ('fin014-ver-d7b', 'fin014-pay-d7b', 'fin014-order-d7b', NULL, 1500000, 'TOMAN', 'gateway'),
  ('fin014-ver-c2c', 'fin014-pay-c2c', 'fin014-order-c2c', NULL, 500000, 'TOMAN', 'gateway'),
  ('fin014-ver-c8c', 'fin014-pay-c8c', 'fin014-order-c8c', NULL, 300000, 'TOMAN', 'gateway'),
  ('fin014-ver-c8d', 'fin014-pay-c8d', 'fin014-order-c8d', NULL, 300000, 'TOMAN', 'simulated'),
  ('fin014-ver-c8e', 'fin014-pay-c8e', 'fin014-order-c8e', NULL, 300000, 'TOMAN', 'gateway'),
  ('fin014-ver-cp5', 'fin014-pay-cp5', 'fin014-coupon-order-5', NULL, 100000, 'TOMAN', 'gateway');
-- Verification event: c8c's outcome is unknown (no receipt follows).
INSERT INTO order_payment_verification_events (id, verification_id, status, observed_amount_toman, provider_reference, reason)
  VALUES ('fin014-vere-c8c', 'fin014-ver-c8c', 'unknown', NULL, '', 'fin014: provider outcome unknown');
INSERT INTO order_payment_receipts (id, verification_id, funding_kind, payment_id, order_id, customer_id, amount_toman, currency, provider_reference, verified_at) VALUES
  ('fin014-rcpt-a1', 'fin014-ver-a1', 'gateway', 'fin014-pay-a1', 'fin014-order-a1', NULL, 5000000, 'TOMAN', 'fin014-ref-a1', NOW()),
  ('fin014-rcpt-b1', 'fin014-ver-b1', 'gateway', 'fin014-pay-b1', 'fin014-order-b1', NULL, 4000000, 'TOMAN', 'fin014-ref-b1', NOW()),
  ('fin014-rcpt-d4a', 'fin014-ver-d4a', 'gateway', 'fin014-pay-d4a', 'fin014-order-d4a', NULL, 1000000, 'TOMAN', 'fin014-ref-d4a', NOW()),
  ('fin014-rcpt-d4b', 'fin014-ver-d4b', 'gateway', 'fin014-pay-d4b', 'fin014-order-d4b', NULL, 800000, 'TOMAN', 'fin014-ref-d4b', NOW()),
  ('fin014-rcpt-d4c', 'fin014-ver-d4c', 'gateway', 'fin014-pay-d4c', 'fin014-order-d4c', NULL, 1000000, 'TOMAN', 'fin014-ref-d4c', NOW()),
  ('fin014-rcpt-d7', 'fin014-ver-d7', 'gateway', 'fin014-pay-d7', 'fin014-order-d7', NULL, 400000, 'TOMAN', 'fin014-ref-d7', NOW()),
  ('fin014-rcpt-d7b', 'fin014-ver-d7b', 'gateway', 'fin014-pay-d7b', 'fin014-order-d7b', NULL, 1500000, 'TOMAN', 'fin014-ref-d7b', NOW()),
  ('fin014-rcpt-c2c', 'fin014-ver-c2c', 'gateway', 'fin014-pay-c2c', 'fin014-order-c2c', NULL, 500000, 'TOMAN', 'fin014-ref-c2c', NOW()),
  ('fin014-rcpt-c8d', 'fin014-ver-c8d', 'simulated', 'fin014-pay-c8d', 'fin014-order-c8d', NULL, 300000, 'TOMAN', 'fin014-sim-ref-d', NOW()),
  ('fin014-rcpt-c8e', 'fin014-ver-c8e', 'gateway', 'fin014-pay-c8e', 'fin014-order-c8e', NULL, 300000, 'TOMAN', 'fin014-ref-c8e', NOW()),
  ('fin014-rcpt-cp5', 'fin014-ver-cp5', 'gateway', 'fin014-pay-cp5', 'fin014-coupon-order-5', NULL, 100000, 'TOMAN', 'fin014-ref-cp5', NOW());

-- Ledger entries (release credits + parcel legs) ----------------------
INSERT INTO ledger_entries (id, date, target_account, target_type, type, amount, reason, operator, related_order_or_dispute, created_at) VALUES
  ('fin014-led-a1', '2026-09-20 10:00:00+00', 'fin014-store-a', 'seller', 'credit', 1000000, 'Synthetic historical profit release', 'fin014-fixture', 'fin014-order-a1', '2026-09-20 10:00:00+00'),
  ('fin014-led-b1', '2026-11-02 10:00:00+00', 'fin014-store-b', 'seller', 'credit', 800000, 'Synthetic historical profit release', 'fin014-fixture', 'fin014-order-b1', '2026-11-02 10:00:00+00'),
  ('fin014-led-d4c', '2026-11-07 10:00:00+00', 'fin014-store-c', 'seller', 'credit', 200000, 'Synthetic historical profit release', 'fin014-fixture', 'fin014-order-d4c', '2026-11-07 10:00:00+00'),
  ('fin014-led-d7', '2026-11-05 10:00:00+00', 'fin014-store-c', 'seller', 'credit', 400000, 'Synthetic historical profit release', 'fin014-fixture', 'fin014-order-d7', '2026-11-05 10:00:00+00'),
  ('fin014-led-d7b', '2026-11-06 10:00:00+00', 'fin014-store-c', 'seller', 'credit', 300000, 'Synthetic historical profit release', 'fin014-fixture', 'fin014-order-d7b', '2026-11-06 10:00:00+00'),
  ('fin014-led-p1a', '2026-10-15 10:00:00+00', 'fin014-supplier-1', 'supplier', 'debit', 50000, 'Supplier return parcel charge fin014-evt-p1a', 'fin014-fixture', NULL, '2026-10-15 10:00:00+00'),
  ('fin014-led-p1b', '2026-10-15 10:00:00+00', 'fin014-supplier-1', 'supplier', 'debit', 50000, 'Supplier return parcel charge fin014-evt-p1b', 'fin014-fixture', NULL, '2026-10-15 10:00:00+00'),
  ('fin014-led-p3', '2026-10-15 10:00:00+00', 'fin014-supplier-1', 'supplier', 'debit', 40000, 'Supplier return parcel charge fin014-parcel-3', 'fin014-fixture', NULL, '2026-10-15 10:00:00+00'),
  ('fin014-led-p4', '2026-10-15 10:00:00+00', 'fin014-supplier-1', 'supplier', 'debit', 70000, 'Supplier return parcel charge fin014-evt-p4', 'fin014-fixture', NULL, '2026-10-15 10:00:00+00'),
  ('fin014-led-p4r', '2026-10-16 10:00:00+00', 'fin014-supplier-1', 'supplier', 'credit', 10000, 'Supplier return parcel reversal fin014-evt-p4r', 'fin014-fixture', NULL, '2026-10-16 10:00:00+00');

-- Class 1: payouts + settlement ---------------------------------------
INSERT INTO payout_requests (id, store_id, recipient_type, recipient_name, shaba, bank_name, amount, status, created_at) VALUES
  ('fin014-payout-1', 'fin014-store-a', 'seller', 'Fin014 Seller A', 'IR000000000000000000000001', 'Fin014 Bank', 1500000, 'pending', '2026-11-10 10:00:00+00'),
  ('fin014-payout-2', 'fin014-store-b', 'seller', 'Fin014 Seller B', 'IR000000000000000000000002', 'Fin014 Bank', 800000, 'pending', '2026-11-10 10:00:00+00'),
  ('fin014-payout-3', 'fin014-store-a', 'seller', 'Fin014 Seller A', 'IR000000000000000000000001', 'Fin014 Bank', 5000000, 'rejected', '2026-11-10 10:00:00+00');
UPDATE payout_requests SET rejection_reason='fin014 control: rejected, never paid' WHERE id='fin014-payout-3';
INSERT INTO monthly_settlement_cycles (period, settlement_day, status, total_stores, total_gross, total_net, skipped_stores, created_by)
  VALUES ('2026-10', 1, 'completed', 2, 2800000, 2800000, 0, 'fin014-fixture')
  ON CONFLICT (period) DO NOTHING; -- period is the PK: several fixture copies share one cycle row for the month
INSERT INTO monthly_settlement_items (id, cycle_period, store_id, gross_amount, fee_amount, net_amount, status) VALUES
  ('fin014-settle-1', '2026-10', 'fin014-store-a', 2000000, 0, 2000000, 'pending_payout'),
  ('fin014-settle-2', '2026-10', 'fin014-store-b', 800000, 0, 800000, 'pending_payout');

-- Class 3: parcels + financial events ---------------------------------
INSERT INTO supplier_return_requests (id, supplier_id, product_id, requested_quantity, collection_method, postal_charge, status, reason)
  VALUES ('fin014-srr-1', 'fin014-supplier-1', 'fin014-product-1', 1, 'postal', 0, 'approved', 'fin014 fixture');
INSERT INTO supplier_return_parcels (id, return_request_id, parcel_number, carrier_code, payment_method, origin, destination, length_mm, width_mm, height_mm, weight_grams, created_by, idempotency_key) VALUES
  ('fin014-parcel-1', 'fin014-srr-1', 1, 'tapin', 'sender_pays', 'Fin014 origin', 'Fin014 destination', 300, 200, 200, 500, 'fin014-fixture', 'fin014-parcel-key-1'),
  ('fin014-parcel-2', 'fin014-srr-1', 2, 'tapin', 'sender_pays', 'Fin014 origin', 'Fin014 destination', 300, 200, 200, 500, 'fin014-fixture', 'fin014-parcel-key-2'),
  ('fin014-parcel-3', 'fin014-srr-1', 3, 'tapin', 'sender_pays', 'Fin014 origin', 'Fin014 destination', 300, 200, 200, 500, 'fin014-fixture', 'fin014-parcel-key-3'),
  ('fin014-parcel-4', 'fin014-srr-1', 4, 'tapin', 'sender_pays', 'Fin014 origin', 'Fin014 destination', 300, 200, 200, 500, 'fin014-fixture', 'fin014-parcel-key-4');
INSERT INTO supplier_return_financial_events (id, return_request_id, parcel_id, supplier_id, operation, amount, payment_method, ledger_entry_id, idempotency_key, reason, actor_user_id, charge_component) VALUES
  ('fin014-evt-p1a', 'fin014-srr-1', 'fin014-parcel-1', 'fin014-supplier-1', 'debit', 50000, 'sender_pays', 'fin014-led-p1a', 'fin014-key-p1a', 'fin014 duplicate debit 1 of 2', 'fin014-fixture', 'original'),
  ('fin014-evt-p1b', 'fin014-srr-1', 'fin014-parcel-1', 'fin014-supplier-1', 'debit', 50000, 'sender_pays', 'fin014-led-p1b', 'fin014-key-p1b', 'fin014 duplicate debit 2 of 2', 'fin014-fixture', 'adjustment'),
  ('fin014-evt-p2', 'fin014-srr-1', 'fin014-parcel-2', 'fin014-supplier-1', 'debit', 60000, 'sender_pays', NULL, 'fin014-key-p2', 'fin014 debit with no ledger leg', 'fin014-fixture', 'original'),
  ('fin014-evt-p3a', 'fin014-srr-1', 'fin014-parcel-3', 'fin014-supplier-1', 'debit', 40000, 'sender_pays', 'fin014-led-p3', 'fin014-key-p3a', 'fin014 debit sharing a ledger leg 1 of 2', 'fin014-fixture', 'original'),
  ('fin014-evt-p3b', 'fin014-srr-1', 'fin014-parcel-3', 'fin014-supplier-1', 'debit', 60000, 'sender_pays', 'fin014-led-p3', 'fin014-key-p3b', 'fin014 debit sharing a ledger leg 2 of 2', 'fin014-fixture', 'adjustment'),
  ('fin014-evt-p4', 'fin014-srr-1', 'fin014-parcel-4', 'fin014-supplier-1', 'debit', 70000, 'sender_pays', 'fin014-led-p4', 'fin014-key-p4', 'fin014 control debit', 'fin014-fixture', 'original'),
  ('fin014-evt-p4r', 'fin014-srr-1', 'fin014-parcel-4', 'fin014-supplier-1', 'reversal', 10000, 'sender_pays', 'fin014-led-p4r', 'fin014-key-p4r', 'fin014 control partial correction', 'fin014-fixture', 'adjustment');

-- Class 6: coupons + reservations -------------------------------------
-- FIN-007 seeding shape (the deferred consistency triggers evaluate
-- at COMMIT, so part 1 must end consistent and part 2 creates the
-- drift): coupons start consistent (used == occupying reservations,
-- used <= max); reservations are inserted as admission+reserved (the
-- runtime fence allows no other INSERT shape) with matching order
-- discount evidence + transition events; used_count is then set to
-- the reservation count. Part 2 creates the defects: coupon-1's
-- max_uses is lowered below used_count, coupon-2's reservation is
-- deleted (DELETE fires no consistency trigger).
INSERT INTO seller_discount_codes (id, store_id, code, mode, value, starts_at, ends_at, is_active, max_uses, used_count) VALUES
  ('fin014-coupon-1', 'fin014-store-d', 'FIN014ONE', 'percent', 10, '2026-01-01 00:00:00+00', '2027-01-01 00:00:00+00', TRUE, 5, 0),
  ('fin014-coupon-2', 'fin014-store-d', 'FIN014TWO', 'percent', 10, '2026-01-01 00:00:00+00', '2027-01-01 00:00:00+00', TRUE, 10, 0),
  ('fin014-coupon-3', 'fin014-store-d', 'FIN014THREE', 'percent', 10, '2026-01-01 00:00:00+00', '2027-01-01 00:00:00+00', TRUE, 5, 0);
INSERT INTO order_coupon_reservations (order_id, coupon_id, store_id, code, seller_discount_amount, coupon_discount_amount, coupon_revision, state, evidence_kind, reason) VALUES
  ('fin014-coupon-order-1', 'fin014-coupon-1', 'fin014-store-d', 'FIN014ONE', 10000, 5000, 1, 'reserved', 'admission', 'fin014 fixture'),
  ('fin014-coupon-order-2', 'fin014-coupon-1', 'fin014-store-d', 'FIN014ONE', 10000, 5000, 1, 'reserved', 'admission', 'fin014 fixture'),
  ('fin014-coupon-order-3', 'fin014-coupon-2', 'fin014-store-d', 'FIN014TWO', 10000, 5000, 1, 'reserved', 'admission', 'fin014 fixture'),
  ('fin014-coupon-order-4', 'fin014-coupon-3', 'fin014-store-d', 'FIN014THREE', 10000, 5000, 1, 'reserved', 'admission', 'fin014 fixture'),
  ('fin014-coupon-order-5', 'fin014-coupon-3', 'fin014-store-d', 'FIN014THREE', 10000, 5000, 1, 'reserved', 'admission', 'fin014 fixture');
INSERT INTO order_coupon_events (order_id, event_kind, prior_state, new_state, reason) VALUES
  ('fin014-coupon-order-1', 'reserved', NULL, 'reserved', 'fin014 fixture'),
  ('fin014-coupon-order-2', 'review_hold', 'reserved', 'review_hold', 'fin014 fixture'),
  ('fin014-coupon-order-3', 'reserved', NULL, 'reserved', 'fin014 fixture'),
  ('fin014-coupon-order-4', 'reserved', NULL, 'reserved', 'fin014 fixture'),
  ('fin014-coupon-order-5', 'consumed', 'reserved', 'consumed', 'fin014 fixture');
UPDATE order_coupon_reservations SET state='review_hold' WHERE order_id='fin014-coupon-order-2';
UPDATE order_coupon_reservations SET state='consumed' WHERE order_id='fin014-coupon-order-5';
UPDATE seller_discount_codes SET used_count=2 WHERE id='fin014-coupon-1';
UPDATE seller_discount_codes SET used_count=1 WHERE id='fin014-coupon-2';
UPDATE seller_discount_codes SET used_count=2 WHERE id='fin014-coupon-3';

-- Class 5: storage positions / lots ------------------------------------
-- A receipt lot cannot be seeded by plain SQL: guard_storage_receipt_lot_insert
-- requires a full inbound-QC disposition chain, and a fee charge requires a
-- VERIFIED lot. The lots are therefore built by the Go helper
-- seedFIN014StorageLots (fin014_fixture_test.go), which replays the real
-- inbound-QC chain through createInboundPutawayTx + creditStorageReceiptLotTx:
-- one lot with an UNVERIFIED measurement (history_status
-- 'unresolved_measurement' — the class-5 row) and one lot with a verified
-- measurement (the negative control). Lot ids are deterministic UUIDv5
-- ('WSP-' + sha1('inbound_qc:<disposition>:<movement>')), so the test
-- asserts class 5 through a lot-id-prefixed report call, not the
-- fin014- prefix. The detector's other branches (charges with no lot,
-- duplicate/non-positive charges) cover real pre-fence history and are
-- unseedable here by the same guards — see the file header.

-- Class 4: disputes (PENDING in part 1) + evidence ----------------------
INSERT INTO disputes (id, order_id, order_number, type, customer_name, product_title, claim_amount, reason, store_id, status) VALUES
  ('fin014-dispute-d4a', 'fin014-order-d4a', 'FIN014-D4A', 'return_7day', 'Fin014 Synthetic', 'Fin014 Synthetic Product', 250000, 'fin014 fixture dispute', 'fin014-store-c', 'pending'),
  ('fin014-dispute-d4b', 'fin014-order-d4b', 'FIN014-D4B', 'return_7day', 'Fin014 Synthetic', 'Fin014 Synthetic Product', 100000, 'fin014 fixture dispute', 'fin014-store-c', 'pending'),
  ('fin014-dispute-d4c', 'fin014-order-d4c', 'FIN014-D4C', 'return_7day', 'Fin014 Synthetic', 'Fin014 Synthetic Product', 200000, 'fin014 fixture dispute', 'fin014-store-c', 'pending'),
  ('fin014-dispute-d7', 'fin014-order-d7', 'FIN014-D7', 'return_7day', 'Fin014 Synthetic', 'Fin014 Synthetic Product', 400000, 'fin014 fixture dispute', 'fin014-store-c', 'pending');
INSERT INTO dispute_order_evidence (dispute_id, order_id, prior_status, prior_disputed_at, prior_delivered_at, prior_confirmed_at, prior_window_ends_at, prior_profit_released, prior_profit_released_at, prior_referral_released, hold_at, evidence_kind, actor_user_id, review_reference) VALUES
  ('fin014-dispute-d4a', 'fin014-order-d4a', 'delivered', NULL, '2026-10-28 10:00:00+00', '2026-10-28 10:00:00+00', '2026-11-01 10:00:00+00', FALSE, NULL, FALSE, '2026-11-02 10:00:00+00', 'admission', 'fin014-fixture', ''),
  ('fin014-dispute-d4b', 'fin014-order-d4b', 'delivered', NULL, '2026-10-28 10:00:00+00', '2026-10-28 10:00:00+00', '2026-11-01 10:00:00+00', FALSE, NULL, FALSE, '2026-11-02 10:00:00+00', 'admission', 'fin014-fixture', ''),
  ('fin014-dispute-d4c', 'fin014-order-d4c', 'delivered', NULL, '2026-10-28 10:00:00+00', '2026-10-28 10:00:00+00', '2026-11-01 10:00:00+00', FALSE, NULL, FALSE, '2026-11-02 10:00:00+00', 'admission', 'fin014-fixture', ''),
  ('fin014-dispute-d7', 'fin014-order-d7', 'delivered', NULL, NULL, NULL, NULL, FALSE, NULL, FALSE, '2026-11-02 10:00:00+00', 'admission', 'fin014-fixture', '');
-- Audited review rows: d4a and d4c have one (certain / control);
-- d4b has none (ambiguous, review_provenance_missing).
INSERT INTO audit_logs (id, operator, action, category, details) VALUES
  ('fin014-audit-d4a', 'fin014-fixture', 'FIN005 reviewed legacy evidence', 'dispute', 'FIN014 review of fin014-dispute-d4a: release_to_seller confirmed'),
  ('fin014-audit-d4c', 'fin014-fixture', 'FIN005 reviewed legacy evidence', 'dispute', 'FIN014 review of fin014-dispute-d4c: release_to_seller confirmed');

-- Class 2: cancellation obligations (guard-validated inserts) ---------
-- c2u: no gateway receipt, so the guard parks it as unknown.
-- c2c: gateway receipt exists, so it is pending (control: exactly one).
INSERT INTO order_refund_obligations (id, order_id, source_payment_id, customer_id, amount_toman, currency, purpose, business_id, destination, policy_version, status, state_reason, actor_user_id) VALUES
  ('fin014-obl-c2u', 'fin014-order-c2u', 'fin014-pay-c2u', NULL, 500000, 'TOMAN', 'cancellation', 'fin014-order-c2u', 'original_payment', 'original_payment_v1', 'unknown', 'funding_unverified', 'fin014-fixture'),
  ('fin014-obl-c2c', 'fin014-order-c2c', 'fin014-pay-c2c', NULL, 500000, 'TOMAN', 'cancellation', 'fin014-order-c2c', 'original_payment', 'original_payment_v1', 'pending', 'awaiting_original_provider', 'fin014-fixture');

-- == FIN014 PART 2 ==
SELECT set_config('sellersho.fin005_writer','v1',true);
SELECT set_config('sellersho.fin007_writer','v1',true);

-- Resolve the disputes (decision rows satisfy the FIN-005 deferred
-- consistency checks at commit; the d7 obligation satisfies the
-- decision_obligations check for the refund verdict).
UPDATE disputes SET status='resolved', resolution_action='release_to_seller', resolution_date='2026-11-10 10:00:00+00', responsible_party='seller'
  WHERE id IN ('fin014-dispute-d4a', 'fin014-dispute-d4b', 'fin014-dispute-d4c');
UPDATE disputes SET status='resolved', resolution_action='refund_seller', resolution_date='2026-11-10 10:00:00+00', responsible_party='seller'
  WHERE id='fin014-dispute-d7';
INSERT INTO dispute_financial_decisions (dispute_id, order_id, action, responsible_party, claim_amount, actor_user_id, decided_at, evidence_reference) VALUES
  ('fin014-dispute-d4a', 'fin014-order-d4a', 'release_to_seller', 'seller', 250000, 'fin014-fixture', '2026-11-10 10:00:00+00', 'fin014-dispute-d4a'),
  ('fin014-dispute-d4b', 'fin014-order-d4b', 'release_to_seller', 'seller', 100000, 'fin014-fixture', '2026-11-10 10:00:00+00', 'fin014-dispute-d4b'),
  ('fin014-dispute-d4c', 'fin014-order-d4c', 'release_to_seller', 'seller', 200000, 'fin014-fixture', '2026-11-10 10:00:00+00', 'fin014-dispute-d4c'),
  ('fin014-dispute-d7', 'fin014-order-d7', 'refund_seller', 'seller', 400000, 'fin014-fixture', '2026-11-10 10:00:00+00', 'fin014-dispute-d7');
-- d7 dispute refund obligation + retained provider attempt + succeeded
-- outcome event (the event guard requires the attempt row).
INSERT INTO order_refund_obligations (id, order_id, source_payment_id, customer_id, amount_toman, currency, purpose, business_id, destination, policy_version, status, state_reason, actor_user_id) VALUES
  ('fin014-obl-d7', 'fin014-order-d7', 'fin014-pay-d7', NULL, 400000, 'TOMAN', 'dispute', 'fin014-dispute-d7', 'original_payment', 'original_payment_v1', 'pending', 'awaiting_original_provider', 'fin014-fixture');
INSERT INTO order_refund_attempts (id, obligation_id, source_payment_id, amount_toman, currency, destination, operation_key, original_reference) VALUES
  ('fin014-attempt-d7', 'fin014-obl-d7', 'fin014-pay-d7', 400000, 'TOMAN', 'original_payment', 'refund:fin014-obl-d7', 'fin014-ref-d7');
INSERT INTO order_refund_events (id, obligation_id, source_payment_id, amount_toman, currency, destination, status, operation_key, provider_reference, reason) VALUES
  ('fin014-evt-d7', 'fin014-obl-d7', 'fin014-pay-d7', 400000, 'TOMAN', 'original_payment', 'succeeded', 'refund:fin014-obl-d7', 'fin014-refund-d7', 'fin014 fixture: succeeded dispute refund');
-- Move the control + d7 orders out of dispute into released state.
UPDATE orders SET status='delivered_confirmed', confirmed_at='2026-11-03 10:00:00+00', profit_released=TRUE, profit_released_at='2026-11-07 10:00:00+00', return_window_ends_at='2026-11-01 10:00:00+00'
  WHERE id='fin014-order-d4c';
UPDATE orders SET status='delivered_confirmed', confirmed_at='2026-11-03 10:00:00+00', profit_released=TRUE, profit_released_at='2026-11-05 10:00:00+00', return_window_ends_at='2026-11-01 10:00:00+00'
  WHERE id='fin014-order-d7';
-- Class 6 drift (created here, in a separate transaction, because
-- the FIN-007 deferred consistency triggers would reject it in the
-- same transaction that inserts the reservations): coupon-1's
-- max_uses drops below its used_count. Coupon-2's drift (used_count
-- bumped above its occupying reservations) is seeded by the Go loader
-- after this part: reservations are immutable (the FIN-007 fence
-- forbids DELETE), and the deferred count-consistency trigger must be
-- disabled around the one UPDATE — which cannot run inside this
-- multi-statement transaction (pending trigger events block ALTER
-- TABLE). Same fixture-setup precedent as fin006Fixture's policy
-- trigger; the trigger still guards every runtime write.
UPDATE seller_discount_codes SET max_uses=1 WHERE id='fin014-coupon-1';
