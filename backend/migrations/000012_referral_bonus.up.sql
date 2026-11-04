-- Migration 000012: Referral Bonus Infrastructure (100,000 Toman with lock until first confirmed delivery)

CREATE TABLE IF NOT EXISTS referral_bonuses (
    id VARCHAR(50) PRIMARY KEY,
    referrer_store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    referred_store_id VARCHAR(50) NOT NULL UNIQUE REFERENCES stores(id) ON DELETE CASCADE,
    amount BIGINT NOT NULL DEFAULT 100000,
    status VARCHAR(20) NOT NULL DEFAULT 'locked' CHECK (status IN ('locked', 'released')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_referral_bonuses_referrer ON referral_bonuses(referrer_store_id);
CREATE INDEX IF NOT EXISTS idx_referral_bonuses_status ON referral_bonuses(status);
CREATE INDEX IF NOT EXISTS idx_referral_bonuses_referred ON referral_bonuses(referred_store_id);
