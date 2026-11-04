-- P0 fix wave 2026-10-18 (Fix 1): idempotency store for the live Shaparak
-- PSP callback (POST /api/v1/payments/shaparak/callback in "real" gateway
-- mode). PSPs retry IPN callbacks aggressively, so the handler claims each
-- psp_reference exactly once here and replays the stored outcome on
-- duplicates instead of re-capturing (double-capture protection).
--
-- NOT APPLIED TO PROD — flag for the deploy checklist.

CREATE TABLE IF NOT EXISTS payment_callback_events (
  id VARCHAR(80) PRIMARY KEY,
  -- The PSP's unique transaction reference (e.g. RRN / PSP order id).
  -- REQUIRED on the capture path: the idempotency key.
  psp_reference VARCHAR(120) NOT NULL UNIQUE,
  payment_transaction_id VARCHAR(80),
  order_id VARCHAR(80),
  status VARCHAR(20) NOT NULL DEFAULT 'received'
    CHECK (status IN ('received', 'captured', 'failed', 'rejected')),
  raw_payload TEXT,
  -- The exact JSON response returned for this callback; duplicates replay it.
  result_payload TEXT,
  error TEXT,
  retry_count INT NOT NULL DEFAULT 0,
  received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_payment_callback_events_psp_ref
  ON payment_callback_events(psp_reference);
CREATE INDEX IF NOT EXISTS idx_payment_callback_events_status
  ON payment_callback_events(status);
