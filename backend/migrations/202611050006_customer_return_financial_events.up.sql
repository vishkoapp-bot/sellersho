-- 202611050006: per-item financial reversal events for CUSTOMER returns.
--
-- GAP 6 (Nov 5, 2026): refunding a customer return unwinds the sale, so
-- the supplier's product cost (order_items.wholesale_cost snapshot) and,
-- on dropshipper orders, the seller's margin (order_items.seller_margin
-- snapshot) reverse against the parties' balances. The events live here
-- and not in supplier_return_financial_events because that table's
-- return_request_id FKs to supplier_return_requests (supplier stock
-- returns) — a different flow (same reason the QC shipping split got
-- customer_return_shipping_charges).
--
-- Rows are written by OrderRepository.RefundCustomerReturn in the refund
-- transaction, one per refunded return item and component, each paired
-- with a platform ledger_entries debit (ledger_entry_id). Balances read
-- them back: SupplierBalance subtracts the product_cost sum (through an
-- orders join that excludes cancelled orders, so a later dispute-cancel
-- of the same order never double-reverses the cost), and
-- FinanceRepository.GetStoreBalance nets the seller_profit sum out of
-- the store's released profit. idempotency_key makes a replayed write a
-- no-op at the database level; the refund status guard is the primary
-- replay block.
CREATE TABLE IF NOT EXISTS customer_return_financial_events (
  id VARCHAR(80) PRIMARY KEY,                                  -- 'CRF-' || uuid
  return_id UUID NOT NULL REFERENCES customer_returns(id) ON DELETE CASCADE,
  return_item_id UUID NOT NULL REFERENCES return_items(id) ON DELETE CASCADE,
  order_id TEXT NOT NULL,
  supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE RESTRICT,
  store_id TEXT,
  component VARCHAR(20) NOT NULL CHECK (component IN ('product_cost','seller_profit')),
  operation VARCHAR(20) NOT NULL CHECK (operation IN ('debit','reversal','refund')),
  amount BIGINT NOT NULL CHECK (amount > 0),
  ledger_entry_id VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  reason TEXT NOT NULL,
  actor_user_id VARCHAR(80) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_crfe_supplier_component
  ON customer_return_financial_events (supplier_id, component);
CREATE INDEX IF NOT EXISTS idx_crfe_store_component
  ON customer_return_financial_events (store_id, component);
CREATE INDEX IF NOT EXISTS idx_crfe_return
  ON customer_return_financial_events (return_id);
