-- FIN-002: preserve sealed history; new cycles own durable per-store progress.
-- Inconsistent historical effects require review, never destructive deduplication.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM monthly_settlement_items GROUP BY cycle_period,store_id HAVING count(*)>1) THEN
    RAISE EXCEPTION 'monthly settlement reconciliation required: duplicate cycle/store effects';
  END IF;
  IF EXISTS (
    SELECT 1 FROM monthly_settlement_items i LEFT JOIN payout_requests p ON p.id=i.payout_request_id
    WHERE (i.status='pending_payout' AND (p.id IS NULL OR p.store_id IS DISTINCT FROM i.store_id OR p.amount IS DISTINCT FROM i.net_amount))
       OR (i.status='skipped_no_bank_details' AND i.payout_request_id IS NOT NULL)
  ) THEN
    RAISE EXCEPTION 'monthly settlement reconciliation required: invalid payout links';
  END IF;
END $$;

ALTER TABLE monthly_settlement_cycles
  ADD COLUMN IF NOT EXISTS progress_initialized BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS last_attempted_at TIMESTAMPTZ;

DO $$
DECLARE constraint_name TEXT;
BEGIN
  FOR constraint_name IN
    SELECT c.conname FROM pg_constraint c
    WHERE c.conrelid='monthly_settlement_cycles'::regclass AND c.contype='c'
      AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='status')]::SMALLINT[]
  LOOP
    EXECUTE format('ALTER TABLE monthly_settlement_cycles DROP CONSTRAINT %I',constraint_name);
  END LOOP;
END $$;
ALTER TABLE monthly_settlement_cycles ADD CONSTRAINT monthly_settlement_cycles_status_check
  CHECK (status IN ('running','blocked','completed'));

-- No store FK here: initialization must not take key-share locks on every account.
-- A store is revalidated under its canonical account guard before money effects.
CREATE TABLE IF NOT EXISTS monthly_settlement_progress (
  cycle_period TEXT NOT NULL REFERENCES monthly_settlement_cycles(period),
  store_id TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','blocked','done')),
  reason TEXT NOT NULL DEFAULT '' CHECK (reason IN ('','reserved','missing_bank','no_balance','ineligible','busy','reconciliation_required','temporary_error')),
  attempts INT NOT NULL DEFAULT 0 CHECK (attempts>=0),
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_attempted_at TIMESTAMPTZ,
  PRIMARY KEY(cycle_period,store_id)
);
CREATE INDEX IF NOT EXISTS monthly_settlement_pending_work
  ON monthly_settlement_progress(cycle_period,next_attempt_at,store_id) WHERE state<>'done';
CREATE INDEX IF NOT EXISTS monthly_settlement_fair_cycles
  ON monthly_settlement_cycles(last_attempted_at,period) WHERE status<>'completed';
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint c WHERE c.conrelid='monthly_settlement_items'::regclass AND c.contype='u'
    AND c.conkey=ARRAY[
      (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='cycle_period'),
      (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='store_id')
    ]::SMALLINT[]
  ) THEN
    ALTER TABLE monthly_settlement_items ADD CONSTRAINT monthly_settlement_cycle_store_effect UNIQUE(cycle_period,store_id);
  END IF;
END $$;

-- Existing completed cycles are intentionally untouched and receive no new work.
