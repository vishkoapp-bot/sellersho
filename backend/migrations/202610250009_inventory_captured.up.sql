-- 202610250009_inventory_captured.up.sql
-- Money-fix W1 (2026-10-25), P1 A-4 (critic §A-4): the manual-payment
-- bridge never captured inventory, while CancelOrder derived "captured"
-- from payment_status='paid' — every cancel of a manually-paid order
-- minted phantom stock. orders.inventory_captured records, per order,
-- whether stock was actually debited at payment time; CancelOrder restores
-- stock ONLY when it is true.
--
-- Backfill (true = stock was demonstrably captured):
--   * full-wallet orders (wallet_amount = payable_amount, wallet > 0):
--     the creation transaction runs the capture for born-paid orders;
--   * any order with a payment_transactions row: the gateway capture
--     (MarkPaymentPaid) ran the capture.
-- Manually-confirmed orders (no transaction row, wallet leg < payable)
-- stay FALSE — the manual bridge never captured — so a later cancel
-- restores 0 instead of inventing a unit. The (0,0) pre-migration case
-- (wallet 0 = payable 0) is excluded by the wallet_amount > 0 guard:
-- a zero-payable order captured nothing.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS inventory_captured BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE orders SET inventory_captured = TRUE
WHERE (wallet_amount = payable_amount AND wallet_amount > 0)
   OR EXISTS (SELECT 1 FROM payment_transactions pt WHERE pt.order_id = orders.id);
