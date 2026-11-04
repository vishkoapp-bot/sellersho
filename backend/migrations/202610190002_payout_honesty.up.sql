-- FIX 20261019 (#2 payout honesty): payouts are manual bank transfers.
-- No automatic Paya/bank integration exists, so the ledger must NOT claim
-- money moved on approval. New flow:
--   pending -> approved (marked FOR manual transfer, NO journal)
--   approved -> transfer_confirmed (admin did the bank transfer, journal posted)
--   pending -> rejected

ALTER TABLE payout_requests
    ADD COLUMN IF NOT EXISTS payout_method VARCHAR(30) NOT NULL DEFAULT 'manual_bank_transfer';

ALTER TABLE payout_requests
    ADD COLUMN IF NOT EXISTS transfer_reference VARCHAR(100);

ALTER TABLE payout_requests
    ADD COLUMN IF NOT EXISTS transfer_date TIMESTAMPTZ;

ALTER TABLE payout_requests
    ADD COLUMN IF NOT EXISTS transfer_confirmed_by VARCHAR(100);

ALTER TABLE payout_requests
    ADD COLUMN IF NOT EXISTS transfer_confirmed_at TIMESTAMPTZ;

-- Allow the new honest status alongside legacy ones.
ALTER TABLE payout_requests DROP CONSTRAINT IF EXISTS payout_requests_status_check;
ALTER TABLE payout_requests ADD CONSTRAINT payout_requests_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'processing', 'completed', 'paid', 'transfer_confirmed'));
