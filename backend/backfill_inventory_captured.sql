-- ============================================================================
-- backfill_inventory_captured.sql  --  PROPOSAL ONLY, DO NOT RUN BLINDLY
-- ============================================================================
-- Repairs the historical damage from the inventory_captured bug:
-- MarkPaymentPaid (internal/repository/order_repo.go) debited
-- products.stock / product_variants.stock when a gateway payment landed
-- but never set orders.inventory_captured = TRUE. CancelOrder restores
-- stock ONLY when that flag is true (migration 202610250009), so:
--
--   * orders paid and later cancelled (pre-shipment) never got their
--     stock back -- the units are permanently unsellable;
--   * orders paid and still live carry flag = FALSE, so a FUTURE cancel
--     would again skip the restore.
--
-- The code fix (same change set) makes MarkPaymentPaid set the flag in
-- the payment transaction. This file repairs rows created BEFORE the fix.
--
-- EVIDENCE STANDARD (mirrors migration 202610250009's own reasoning):
-- a payment_transactions row with status = 'paid' is the demonstrable
-- proof that MarkPaymentPaid ran, because the stock debit and the
-- pt.status -> 'paid' flip commit in ONE transaction. Orders without a
-- paid pt row are NEVER touched by the stock-restoring step: no capture
-- is proven for them (manual-bridge era orders captured nothing, per
-- the 202610250009 migration notes).
--
-- SAFETY PROPERTIES:
--   * Every UPDATE is filtered on inventory_captured = FALSE, so the
--     whole script is idempotent: a second run matches 0 rows.
--   * Orders already restored are unreachable: a successful CancelOrder
--     restore only ever happened under flag = TRUE, and nothing ever
--     sets the flag back to FALSE. Pre-migration cancels are likewise
--     safe: migration 202610250009 backfilled flag = TRUE for every
--     then-existing order with a pt row, and pre-migration CancelOrder
--     derived "captured" from payment_status = 'paid' and restored.
--   * Dispute-refunded orders (status = 'cancelled' AND
--     payment_status = 'refunded', written by ApplyDisputeRefundEffect)
--     are EXCLUDED from the stock restore: those goods were shipped
--     (delivery_undelivered disputes), so returning them to sellable
--     stock is a business decision, not a mechanical repair. Step 0
--     lists them for manual review.
--   * Wallet-born-paid orders (wallet_amount = payable_amount AND
--     wallet_amount > 0) are EXCLUDED from the stock restore:
--     migration 202610250009 already set their flag, and the current
--     code cannot create new ones (wallet checkout is rejected), so
--     any flag = FALSE stragglers are reported by Step 0 for manual
--     review instead of being auto-restored on unverified capture
--     semantics.
--
-- RISKS / BEFORE YOU RUN:
--   1. Take a backup/snapshot of orders, order_items, products and
--      product_variants first. Step 2 moves real sellable stock.
--   2. For products covered by the canonical inventory ledger
--      (inventory_balances), products.stock is a PROJECTION maintained
--      by syncProductStockTx (internal/repository/inventory_unify.go);
--      a later sync can overwrite Step 2's product-level restore for
--      such products. Check which affected products are canonical-
--      covered (Step 0.4) and reconcile those through the warehouse
--      ledger instead, if any exist.
--   3. Run each step in its OWN transaction during a quiet window,
--      compare the affected-row counts against the Step 0 counts, and
--      only then COMMIT. The BEGIN/COMMIT lines are commented out on
--      purpose: enable them deliberately, one step at a time.
-- ============================================================================


-- ----------------------------------------------------------------------------
-- STEP 0 -- PRE-FLIGHT COUNTS (read-only; run first, keep the numbers)
-- ----------------------------------------------------------------------------

-- 0.1 Live paid orders whose flag was lost (Step 1 repairs these).
SELECT COUNT(*) AS step1_flag_only_repairs
FROM orders o
WHERE o.inventory_captured = FALSE
  AND o.payment_status = 'paid'
  AND o.status <> 'cancelled'
  AND EXISTS (SELECT 1 FROM payment_transactions pt
              WHERE pt.order_id = o.id AND pt.status = 'paid');

-- 0.2 Paid-then-cancelled orders whose stock was never restored
-- (Step 2 repairs these). payment_status = 'cancelled' is the exact
-- value CancelOrder writes; 'refunded' rows are the dispute path and
-- are deliberately NOT in this set.
SELECT COUNT(*) AS step2_stock_restore_repairs
FROM orders o
WHERE o.inventory_captured = FALSE
  AND o.status = 'cancelled'
  AND o.payment_status = 'cancelled'
  AND EXISTS (SELECT 1 FROM payment_transactions pt
              WHERE pt.order_id = o.id AND pt.status = 'paid');

-- 0.3 Excluded, manual review: dispute-refunded cancelled orders whose
-- stock was debited at payment and never restored by any path.
SELECT o.id, o.order_number, o.created_at
FROM orders o
WHERE o.inventory_captured = FALSE
  AND o.status = 'cancelled'
  AND o.payment_status = 'refunded'
  AND EXISTS (SELECT 1 FROM payment_transactions pt
              WHERE pt.order_id = o.id AND pt.status = 'paid');

-- 0.4 Excluded, manual review: wallet-born-paid stragglers and the
-- canonical-ledger coverage of Step 2's products.
SELECT COUNT(*) AS wallet_born_paid_flag_false
FROM orders
WHERE inventory_captured = FALSE
  AND wallet_amount = payable_amount
  AND wallet_amount > 0;

SELECT DISTINCT oi.product_id
FROM orders o
JOIN order_items oi ON oi.order_id = o.id
WHERE o.inventory_captured = FALSE
  AND o.status = 'cancelled'
  AND o.payment_status = 'cancelled'
  AND EXISTS (SELECT 1 FROM payment_transactions pt
              WHERE pt.order_id = o.id AND pt.status = 'paid')
  AND EXISTS (SELECT 1 FROM inventory_balances ib
              WHERE ib.product_id = oi.product_id);


-- ----------------------------------------------------------------------------
-- STEP 1 -- FLAG-ONLY REPAIR for live paid orders.
-- Their stock was already debited at payment; only the flag was lost.
-- No stock movement. After this, a future CancelOrder restores them
-- correctly instead of leaking their stock a second way.
-- ----------------------------------------------------------------------------
-- BEGIN;
UPDATE orders o
SET inventory_captured = TRUE,
    updated_at = NOW()
WHERE o.inventory_captured = FALSE
  AND o.payment_status = 'paid'
  AND o.status <> 'cancelled'
  AND EXISTS (SELECT 1 FROM payment_transactions pt
              WHERE pt.order_id = o.id AND pt.status = 'paid');
-- Expect the affected-row count to equal step1_flag_only_repairs (0.1),
-- then COMMIT; otherwise ROLLBACK and investigate.


-- ----------------------------------------------------------------------------
-- STEP 2 -- STOCK RESTORE for paid-then-cancelled orders.
-- These orders were debited at payment and CancelOrder skipped the
-- restore because the flag was false. Restore exactly what
-- CancelOrder would have restored (same statements, aggregated per
-- product/variant so duplicate lines in one order sum correctly),
-- then set the flag to record that capture happened and the repair
-- restore has been applied.
-- ----------------------------------------------------------------------------
-- BEGIN;

-- 2a. Product stock.
UPDATE products p
SET stock = p.stock + r.qty,
    updated_at = NOW()
FROM (
    SELECT oi.product_id, SUM(oi.quantity)::int AS qty
    FROM orders o
    JOIN order_items oi ON oi.order_id = o.id
    WHERE o.inventory_captured = FALSE
      AND o.status = 'cancelled'
      AND o.payment_status = 'cancelled'
      AND EXISTS (SELECT 1 FROM payment_transactions pt
                  WHERE pt.order_id = o.id AND pt.status = 'paid')
    GROUP BY oi.product_id
) r
WHERE p.id = r.product_id;

-- 2b. Variant stock (only lines that carry a variant).
UPDATE product_variants v
SET stock = v.stock + r.qty,
    updated_at = NOW()
FROM (
    SELECT oi.variant_id, SUM(oi.quantity)::int AS qty
    FROM orders o
    JOIN order_items oi ON oi.order_id = o.id
    WHERE o.inventory_captured = FALSE
      AND o.status = 'cancelled'
      AND o.payment_status = 'cancelled'
      AND oi.variant_id IS NOT NULL
      AND EXISTS (SELECT 1 FROM payment_transactions pt
                  WHERE pt.order_id = o.id AND pt.status = 'paid')
    GROUP BY oi.variant_id
) r
WHERE v.id = r.variant_id;

-- 2c. Mark the repaired orders. CancelOrder's status guard
-- (already-cancelled orders cannot be cancelled again) prevents any
-- later double restore, and the flag now truthfully records that
-- this order's stock was captured (and, via 2a/2b, returned).
UPDATE orders o
SET inventory_captured = TRUE,
    updated_at = NOW()
WHERE o.inventory_captured = FALSE
  AND o.status = 'cancelled'
  AND o.payment_status = 'cancelled'
  AND EXISTS (SELECT 1 FROM payment_transactions pt
              WHERE pt.order_id = o.id AND pt.status = 'paid');
-- Expect this count to equal step2_stock_restore_repairs (0.2), then
-- COMMIT; otherwise ROLLBACK and investigate.

-- ----------------------------------------------------------------------------
-- STEP 3 -- POST-REPAIR VERIFICATION (read-only).
-- Both counts must be 0. (The Step 0.3 / 0.4 manual-review sets are
-- intentionally still flag = FALSE; they were never in scope here.)
-- ----------------------------------------------------------------------------
SELECT
  (SELECT COUNT(*) FROM orders o
    WHERE o.inventory_captured = FALSE AND o.payment_status = 'paid'
      AND o.status <> 'cancelled'
      AND EXISTS (SELECT 1 FROM payment_transactions pt
                  WHERE pt.order_id = o.id AND pt.status = 'paid')
  ) AS remaining_live_paid_flag_false,
  (SELECT COUNT(*) FROM orders o
    WHERE o.inventory_captured = FALSE AND o.status = 'cancelled'
      AND o.payment_status = 'cancelled'
      AND EXISTS (SELECT 1 FROM payment_transactions pt
                  WHERE pt.order_id = o.id AND pt.status = 'paid')
  ) AS remaining_paid_cancelled_flag_false;
