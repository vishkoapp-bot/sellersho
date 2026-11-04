-- 202611040012: finance_adjustments audit table (Fix69 W-ROUTES R5).
--
-- POST /finance/adjustments (live: AdminDashboardView-MERGED.js
-- createAdjustment, body {targetKind, targetId, direction, amount,
-- reason} -> {docId}) writes a double-entry ledger document whose
-- legs live in ledger_entries (reason prefixed "سند تعدیل", grouped
-- by related_order_or_dispute = docId). This table is the audit
-- row for that document: one row per docId. The ledger remains
-- the system of record the live tab reads; this table never feeds
-- balances. targetKind "seller" refers to a stores id (the live
-- tab's seller list is the stores list); "supplier" to suppliers.
CREATE TABLE IF NOT EXISTS finance_adjustments (
    id          VARCHAR(50) PRIMARY KEY,
    target_kind VARCHAR(20) NOT NULL CHECK (target_kind IN ('supplier', 'seller')),
    target_id   VARCHAR(64) NOT NULL,
    direction   VARCHAR(10) NOT NULL CHECK (direction IN ('debit', 'credit')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    reason      TEXT NOT NULL,
    operator    VARCHAR(100) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finance_adjustments_target
    ON finance_adjustments (target_kind, target_id, created_at DESC);
