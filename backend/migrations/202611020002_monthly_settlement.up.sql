-- Monthly settlement engine (overnight P0 item 7, 2026-11-02).
-- Admin settings live on the platform_settings singleton as NULLABLE
-- columns: NULL means UNSET, and the monthly job is inert until BOTH
-- are set by an admin. No day and no fee percent is defaulted here:
-- "5th of month" is an unconfirmed example, not a ruling, and the
-- early-withdrawal fee percent is unset until Andrew rules it.
-- All statements are IF NOT EXISTS / guarded so a re-apply is a no-op.
-- The deploy copies ONLY new migrations, so this pair must travel
-- with the item-7 code (see staging-evidence/overnight-p0-builds-20261102/07-settlement/REPORT.md).

ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS settlement_day INT
    CHECK (settlement_day IS NULL OR (settlement_day >= 1 AND settlement_day <= 31));
ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS early_withdrawal_fee_pct NUMERIC(5,2)
    CHECK (early_withdrawal_fee_pct IS NULL OR (early_withdrawal_fee_pct >= 0 AND early_withdrawal_fee_pct < 100));

-- One row per settled month. period is the calendar month being
-- settled ('YYYY-MM'); it is the primary key, so a second run for
-- the same month can never create a second cycle (idempotency is
-- enforced by the schema, not only by the code). settlement_day is
-- a snapshot of the setting in force at run time, for audit.
CREATE TABLE IF NOT EXISTS monthly_settlement_cycles (
  period TEXT PRIMARY KEY CHECK (period ~ '^[0-9]{4}-[0-9]{2}$'),
  settlement_day INT NOT NULL CHECK (settlement_day >= 1 AND settlement_day <= 31),
  status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('completed')),
  total_stores INT NOT NULL DEFAULT 0 CHECK (total_stores >= 0),
  total_gross BIGINT NOT NULL DEFAULT 0 CHECK (total_gross >= 0),
  total_net BIGINT NOT NULL DEFAULT 0 CHECK (total_net >= 0),
  skipped_stores INT NOT NULL DEFAULT 0 CHECK (skipped_stores >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_by TEXT NOT NULL DEFAULT ''
);

-- One row per dropshipper store in a cycle. fee_amount is always 0
-- for monthly items (the monthly payout is fee-free; only the early
-- withdrawal path charges a fee). A store with a withdrawable
-- balance but no Sheba on record gets status
-- 'skipped_no_bank_details' and NO payout_request: the engine never
-- invents bank details, and the balance stays withdrawable for a
-- request-based payout or the next cycle.
CREATE TABLE IF NOT EXISTS monthly_settlement_items (
  id TEXT PRIMARY KEY,
  cycle_period TEXT NOT NULL REFERENCES monthly_settlement_cycles(period),
  store_id VARCHAR(50) NOT NULL REFERENCES stores(id),
  gross_amount BIGINT NOT NULL CHECK (gross_amount > 0),
  fee_amount BIGINT NOT NULL DEFAULT 0 CHECK (fee_amount >= 0),
  net_amount BIGINT NOT NULL CHECK (net_amount > 0),
  payout_request_id VARCHAR(50) REFERENCES payout_requests(id),
  status TEXT NOT NULL CHECK (status IN ('pending_payout', 'skipped_no_bank_details')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (cycle_period, store_id)
);

-- Early-withdrawal requests: seller asks, finance approves or
-- rejects. While 'pending', gross_amount is subtracted from the
-- seller's available balance (see sellerAvailableSQL /
-- GetStoreBalance), so the same profit cannot be swept twice.
-- On approval the engine creates a payout_requests row for the NET
-- amount and marks it 'approved' in the same transaction, plus the
-- matching ledger debit; the fee itself is recorded here (fee_pct
-- snapshot + fee_amount) and is subtracted from availability once
-- approved. On rejection nothing moves and the hold releases.
CREATE TABLE IF NOT EXISTS seller_early_withdrawals (
  id TEXT PRIMARY KEY,
  store_id VARCHAR(50) NOT NULL REFERENCES stores(id),
  gross_amount BIGINT NOT NULL CHECK (gross_amount > 0),
  fee_pct NUMERIC(5,2) NOT NULL CHECK (fee_pct >= 0 AND fee_pct < 100),
  fee_amount BIGINT NOT NULL CHECK (fee_amount >= 0),
  net_amount BIGINT NOT NULL CHECK (net_amount > 0),
  shaba TEXT NOT NULL CHECK (shaba <> ''),
  bank_name TEXT NOT NULL CHECK (bank_name <> ''),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
  rejection_reason TEXT,
  payout_request_id VARCHAR(50) REFERENCES payout_requests(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_monthly_items_store ON monthly_settlement_items(store_id);
CREATE INDEX IF NOT EXISTS idx_early_withdrawals_store ON seller_early_withdrawals(store_id, status);
