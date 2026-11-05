-- 202611050006: revert customer-return financial reversal events.
--
-- Dropping the table loses the per-item reversal audit trail of
-- customer returns refunded while it existed; the paired
-- ledger_entries debits remain, and supplier/store balances fall back
-- to their pre-GAP-6 terms (the read side guards on this table's
-- existence).
DROP INDEX IF EXISTS idx_crfe_return;
DROP INDEX IF EXISTS idx_crfe_store_component;
DROP INDEX IF EXISTS idx_crfe_supplier_component;

DROP TABLE IF EXISTS customer_return_financial_events;
