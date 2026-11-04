-- 202610250007_refunds_table.up.sql
-- Money-fix W1 (2026-10-25), P0-6 / audit M-P0-4 (ledger F1): claim-first
-- PSP refunds. The admin refund handler claims the refund HERE (one row per
-- order, unique idempotency key) and flips the order to refund-pending
-- BEFORE calling the PSP, so a retry replays the recorded outcome instead
-- of issuing a second PSP refund. Status lifecycle:
--   pending      — claimed, PSP not yet called (retry re-attempts the PSP)
--   psp_refunded — PSP confirmed the refund (retry skips the PSP call)
--   completed    — refund journal posted + order/txn marked refunded
--   failed       — terminal failure recorded for manual review
-- amount is integer TOMAN (internal unit), the net-held amount at claim time.
CREATE TABLE IF NOT EXISTS refunds (
    id              VARCHAR(80) PRIMARY KEY,
    order_id        VARCHAR(50) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    idempotency_key VARCHAR(120) NOT NULL UNIQUE,
    psp_reference   VARCHAR(80),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    reason          TEXT NOT NULL DEFAULT '',
    status          VARCHAR(20) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'psp_refunded', 'completed', 'failed')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refunds_order_id ON refunds (order_id);
