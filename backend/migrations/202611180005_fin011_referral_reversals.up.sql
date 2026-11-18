-- 202611180005: FIN-011 referral-commission reversal records (Q-01 retained-value).
--
-- A referral commission is a share of RETAINED order value: returns reduce it
-- pro-rata, a full return zeroes it (owner decision Nov 18 ~23:35 Tehran,
-- "کم بشه با برگشت (برگشت کامل = صفر)"). The reversal is an order-level
-- compensating event, never a mutation of the released order row or of the
-- referral_commission_released flag:
--
--   * customer returns: written by OrderRepository.recordReturnReversalsTx
--     in the same transaction as the wallet refund (source='customer_return',
--     source_id = the customer_returns id);
--   * dispute / admin refunds: written when the original_payment obligation
--     reaches 'succeeded' in OrderRepository.recordRefundOutcome
--     (source='dispute' | 'admin_refund', source_id = the obligation id).
--     Cancellation refunds write nothing: a cancelled order can never
--     release, so its commission is never earned.
--
-- The amount is a truncating pro-rata share of the snapshot commission,
-- derived from the returned final_unit_price proportion (platform profit
-- is order-level only), computed against the CUMULATIVE refunded value so
-- split returns sum exactly to the whole-order target:
--   target = min(trunc(commission * cumulativeRefunded / orderTotal), commission)
--   amount = target - alreadyReversed   (floored at 0)
-- See referralReversalTarget in internal/repository/referral_reversal.go,
-- the single implementation both writers and the release gate consult.
--
-- Balances read this table, never the orders snapshot alone:
-- sellerAvailableSQL / GetStoreBalance net reversals on RELEASED orders
-- only (a reversal on an unreleased order nets against the FIN-008
-- treasury hold instead — see finance_treasury.go). The release gate
-- (disputeReleaseStateTx) refuses to release while SUM(reversals) != the
-- expected total for the order's succeeded refunds.
--
-- Why NOT customer_return_financial_events: that table is per-return-ITEM
-- (return_item_id FK, and fin005_return_entitlement_proven counts its rows
-- per return against the cost+margin expectation). Referral commission is
-- order-level, and dispute refunds have no customer_returns row at all
-- (return_id FK), so the referral reversal lives here and fin005 stays
-- blind to it by design; the release-gate completeness check above is the
-- control that replaces it. Each row is paired with a platform ledger
-- debit (ledger_entry_id) so the reversal is visible in the ledger too.
-- Rows are immutable (fin005_immutable trigger, same as the FIN-005
-- evidence tables): a correction is a new row, never an edit.
CREATE TABLE IF NOT EXISTS referral_commission_reversals (
  id VARCHAR(80) PRIMARY KEY,                                  -- 'RCR-' || uuid
  order_id TEXT NOT NULL,
  referrer_store_id TEXT NOT NULL,
  amount BIGINT NOT NULL CHECK (amount > 0),
  source VARCHAR(20) NOT NULL CHECK (source IN ('customer_return','dispute','admin_refund')),
  source_id TEXT NOT NULL,
  ledger_entry_id VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,                -- 'rcr:' || source || ':' || source_id
  reason TEXT NOT NULL,
  actor_user_id VARCHAR(80) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_rcr_order ON referral_commission_reversals (order_id);
CREATE INDEX IF NOT EXISTS idx_rcr_referrer ON referral_commission_reversals (referrer_store_id);

CREATE TRIGGER fin011_reversal_immutable BEFORE UPDATE OR DELETE ON referral_commission_reversals FOR EACH ROW EXECUTE FUNCTION fin005_immutable();
