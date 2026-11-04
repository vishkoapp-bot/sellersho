-- 202610200002_customer_wallet_engine.up.sql
-- Customer wallet engine: spend at checkout (full / split) + Sheba withdrawals.
-- Additive only. All amounts integer Tomans. See wallet spec §2–§4.

-- 1. customer_wallets: hold projection for in-flight withdrawal requests.
--    available = balance - locked_balance (computed, never stored).
ALTER TABLE customer_wallets
    ADD COLUMN IF NOT EXISTS locked_balance BIGINT NOT NULL DEFAULT 0
        CHECK (locked_balance >= 0);

ALTER TABLE customer_wallets DROP CONSTRAINT IF EXISTS customer_wallets_balance_covers_locked;
ALTER TABLE customer_wallets ADD CONSTRAINT customer_wallets_balance_covers_locked
    CHECK (balance >= locked_balance);

-- 2. orders: how the payable splits between wallet and gateway/manual transfer.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS wallet_amount  BIGINT NOT NULL DEFAULT 0 CHECK (wallet_amount  >= 0),
    ADD COLUMN IF NOT EXISTS gateway_amount BIGINT NOT NULL DEFAULT 0 CHECK (gateway_amount >= 0);

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_wallet_gateway_within_payable;
ALTER TABLE orders ADD CONSTRAINT orders_wallet_gateway_within_payable
    CHECK (wallet_amount + gateway_amount <= payable_amount);

-- 3. customer_withdrawal_requests: Sheba payout requests for customer wallets.
--    Lifecycle (spec §3.4): pending -> approved -> paid; pending|approved -> rejected;
--    pending -> cancelled (customer). Holds live in customer_wallets.locked_balance;
--    NO journal exists until the paid transition (WalletWithdrawalPaidLines).
CREATE TABLE IF NOT EXISTS customer_withdrawal_requests (
    id                    VARCHAR(50) PRIMARY KEY,
    customer_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    customer_name         VARCHAR(100) NOT NULL,
    customer_mobile       VARCHAR(20)  NOT NULL,
    account_holder_name   VARCHAR(100) NOT NULL,
    sheba                 VARCHAR(30)  NOT NULL CHECK (sheba ~ '^IR[0-9]{24}$'),
    bank_name             VARCHAR(50)  NOT NULL,
    amount                BIGINT NOT NULL CHECK (amount > 0),
    status                VARCHAR(20)  NOT NULL DEFAULT 'pending'
                          CHECK (status IN ('pending','approved','rejected','paid','cancelled')),
    rejection_reason      TEXT,
    reviewed_by           VARCHAR(100),
    reviewed_at           TIMESTAMPTZ,
    transfer_reference    VARCHAR(100),
    transfer_date         TIMESTAMPTZ,
    transfer_confirmed_by VARCHAR(100),
    transfer_confirmed_at TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_customer_withdrawals_customer
    ON customer_withdrawal_requests (customer_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_customer_withdrawals_status
    ON customer_withdrawal_requests (status, created_at);

-- 4. platform_settings: wallet kill switches + minimum withdrawal (Tomans).
ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS wallet_spend_enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS wallet_withdraw_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS wallet_min_withdrawal   BIGINT  NOT NULL DEFAULT 0
        CHECK (wallet_min_withdrawal >= 0);
