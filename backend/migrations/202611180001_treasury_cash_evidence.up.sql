-- 202611180001_treasury_cash_evidence.up.sql
-- FIN-008 (G-15): evidenced opening cash for the treasury cash position.
--
-- An opening-cash record is verifiable bank/provider cash ONLY when it
-- carries a non-empty evidence reference (provider reference / bank
-- statement id). There is no guessed opening balance anywhere in the
-- tree: the CHECK below rejects NULL/empty evidence, and no settings
-- key, config value, env var or import endpoint writes this table.
-- liquidCash counts opening cash only from rows in this table (plus
-- verified gateway receipts); everything else is unresolvedProviderCash.
CREATE TABLE IF NOT EXISTS treasury_opening_cash (
    id TEXT PRIMARY KEY,
    amount_toman BIGINT NOT NULL CHECK (amount_toman BETWEEN 1 AND 9007199254740991),
    currency TEXT NOT NULL CHECK (currency = 'TOMAN'),
    evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('bank_statement', 'provider_settlement', 'opening_balance_audit')),
    evidence_reference TEXT NOT NULL CHECK (length(btrim(evidence_reference)) > 0),
    verified_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX IF NOT EXISTS treasury_opening_cash_verified ON treasury_opening_cash(verified_at);
