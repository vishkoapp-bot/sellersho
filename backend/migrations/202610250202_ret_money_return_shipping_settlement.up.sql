-- 202610250202: returns-shipping settlement (money stream, user policy Oct 25 2026).
--
-- Policy (RETURNS SHIPPING POLICY — FULL RULES, Oct 25 2026 ~17:50):
--   - a return is QC-received at a central warehouse; the returned product
--     gets a tracking code (return_code) so it can be followed everywhere;
--   - the return shipping amount is computed with the SAME engine as the
--     outbound fare (computeShippingCost / computeShippingLegs, Toman;
--     NULL weight -> 500g platform default, exactly like outbound);
--   - per item, by QC verdict: 'defective' -> the item's share is charged
--     to the SUPPLIER (their balance may go negative, explicit policy)
--     and credited to the customer wallet (the customer prepaid the
--     courier and is made whole); 'ok' (taste / wrong choice) -> the
--     item's share is debited from the CUSTOMER wallet (product refund
--     credited first, debit clamped so the wallet never goes below zero
--     — customer_wallets.balance CHECK (balance >= 0) forbids it).
--   - a parcel that arrives post-paid (COD) is REFUSED: the warehouse
--     keeper cannot pay the postman, so the parcel goes back to the
--     customer automatically — no receipt, no money, no restock.
--
-- Columns owned by the core stream (warehouse_id, prepaid, tracking_number,
-- shipped_at) are added here with IF NOT EXISTS only so this tree is
-- self-contained; the parent deduplicates at merge.
-- Money units: ALL amounts in this migration are integer Toman, matching
-- computeShippingCost and the wallet/ledger. Shipping settings in
-- platform_settings are Toman-scale (base 101690 / per_kg 21841 on prod).

CREATE SEQUENCE IF NOT EXISTS customer_return_code_seq;

ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS warehouse_id TEXT REFERENCES warehouses(id) ON DELETE SET NULL;
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS prepaid BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS tracking_number TEXT;
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS shipped_at TIMESTAMPTZ;

ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS return_code TEXT UNIQUE;
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS cod_refused BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS return_shipping_amount BIGINT;
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS return_shipping_bearer VARCHAR(20)
    CHECK (return_shipping_bearer IN ('supplier', 'customer', 'mixed'));
ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS return_shipping_settled_at TIMESTAMPTZ;

-- Per-item settlement of the return shipping amount. One row per received
-- return item that carried a non-zero share. bearer='supplier' rows feed
-- SupplierBalance.ReturnCharges (see supplierBalanceSQL), so the supplier's
-- available balance drops — and can go negative — exactly as the policy
-- requires. bearer='customer' rows are the trace of the wallet debit.
-- idempotency_key is return-scoped per item: an item can only be received
-- once (received_at guard), so a retry can never insert a second row.
CREATE TABLE IF NOT EXISTS customer_return_shipping_charges (
  id TEXT PRIMARY KEY,
  return_id UUID NOT NULL REFERENCES customer_returns(id) ON DELETE CASCADE,
  return_item_id UUID NOT NULL REFERENCES return_items(id) ON DELETE CASCADE,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
  bearer VARCHAR(20) NOT NULL CHECK (bearer IN ('supplier', 'customer')),
  amount BIGINT NOT NULL CHECK (amount > 0),
  ledger_entry_id VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  reason TEXT NOT NULL,
  actor_user_id VARCHAR(80) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_crsc_return ON customer_return_shipping_charges(return_id);
CREATE INDEX IF NOT EXISTS idx_crsc_supplier ON customer_return_shipping_charges(supplier_id);
