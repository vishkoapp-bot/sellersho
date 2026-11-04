ALTER TABLE payout_requests ADD COLUMN IF NOT EXISTS available_balance BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS seller_service_purchases (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id VARCHAR(50) NOT NULL REFERENCES stores(id),
    service_type VARCHAR(30) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);