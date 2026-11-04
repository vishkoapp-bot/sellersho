-- Money-trap fix 2026-10-19: dead-letter queue for failed PSP callbacks.
--
-- When the live Shaparak callback fails AFTER the customer was charged
-- (verification error, inventory issue, server crash), the callback is
-- queued here for retry with exponential backoff instead of being lost.
-- After 5 failures the row is flagged for manual admin review.

CREATE TABLE IF NOT EXISTS payment_callback_retries (
  id VARCHAR(80) PRIMARY KEY,
  psp_reference VARCHAR(120) NOT NULL,
  payment_token VARCHAR(120),
  order_id VARCHAR(80),
  order_number VARCHAR(80),
  -- Raw callback payload for replay
  raw_payload TEXT NOT NULL,
  -- Failure tracking
  attempt_count INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 5,
  last_error TEXT,
  -- Scheduling: next_retry_at drives the worker's polling
  next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  status VARCHAR(20) NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'retrying', 'succeeded', 'needs_manual_review', 'abandoned')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_callback_retries_next_retry
  ON payment_callback_retries(next_retry_at) WHERE status IN ('pending', 'retrying');
CREATE INDEX IF NOT EXISTS idx_callback_retries_status
  ON payment_callback_retries(status);
CREATE INDEX IF NOT EXISTS idx_callback_retries_psp_ref
  ON payment_callback_retries(psp_reference);
