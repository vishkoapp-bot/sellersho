-- 202611190001: FIN-009 seller-profit reversal records (D-01).
--
-- A dispute/admin refund reverses the seller's released profit pro-rata:
-- the seller keeps only the share of RETAINED order value (owner policy
-- Q-01 pattern, FIN-011: "کم بشه با برگشت (برگشت کامل = صفر)"). The
-- reversal is an order-level compensating event, never a mutation of the
-- released order row or of the profit_released flag:
--
--   * dispute / admin refunds: written when the original_payment
--     obligation reaches 'succeeded' in OrderRepository.recordRefundOutcome
--     (source='dispute' | 'admin_refund', source_id = the obligation id).
--     Customer-return (wallet) refunds reverse in
--     recordReturnReversalsTx instead; cancellations write nothing (a
--     cancelled order can never release, so its profit is never earned).
--
-- The amount is a truncating pro-rata share of the snapshot
-- dropshipper_profit, derived from the refunded final_unit_price
-- proportion (profit is order-level only), computed against the
-- CUMULATIVE refunded value so split refunds sum exactly to the
-- whole-order target:
--   target = min(trunc(profit * cumulativeRefunded / orderTotal), profit)
--   amount = target - alreadyReversed   (floored at 0)
-- See referralReversalTarget in internal/repository/referral_reversal.go,
-- the single cumulative pro-rata rule both writers consult.
-- cumulativeRefunded counts EVERY refunded value of the order (succeeded
-- obligations plus refunded wallet returns); alreadyReversed counts every
-- seller_profit reversal already recorded for the order (wallet
-- customer_return_financial_events rows plus rows here).
--
-- Balances read this table, never the orders snapshot alone:
-- sellerAvailableSQL / GetStoreBalance net reversals on RELEASED orders
-- only (a reversal on an unreleased order nets against the FIN-008
-- treasury hold instead — see finance_treasury.go heldInOrders). The
-- release gate (disputeReleaseStateTx) refuses to release while any
-- dispute/admin obligation exists, so a pre-release dispute refund can
-- never make released profit appear.
--
-- Why NOT customer_return_financial_events: that table is per-return-ITEM
-- (return_id / return_item_id are NOT NULL FKs to customer_returns /
-- return_items, and fin005_return_entitlement_proven counts its rows per
-- return against the cost+margin expectation). Dispute/admin refunds have
-- no customer_returns row at all, so the seller leg lives here, next to
-- the referral reversal it mirrors. Each row is paired with a platform
-- ledger debit (ledger_entry_id) so the reversal is visible in the ledger
-- too. Rows are immutable (fin005_immutable trigger, same as the FIN-005
-- evidence tables): a correction is a new row, never an edit.
CREATE TABLE IF NOT EXISTS seller_profit_reversals (
  id VARCHAR(80) PRIMARY KEY,                                  -- 'SPR-' || uuid
  order_id TEXT NOT NULL,
  store_id TEXT NOT NULL,
  amount BIGINT NOT NULL CHECK (amount > 0),
  source VARCHAR(20) NOT NULL CHECK (source IN ('dispute','admin_refund')),
  source_id TEXT NOT NULL,
  ledger_entry_id VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,                -- 'spr:' || source || ':' || source_id
  reason TEXT NOT NULL,
  actor_user_id VARCHAR(80) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_spr_order ON seller_profit_reversals (order_id);
CREATE INDEX IF NOT EXISTS idx_spr_store ON seller_profit_reversals (store_id);

CREATE TRIGGER fin009_seller_profit_reversal_immutable BEFORE UPDATE OR DELETE ON seller_profit_reversals FOR EACH ROW EXECUTE FUNCTION fin005_immutable();
