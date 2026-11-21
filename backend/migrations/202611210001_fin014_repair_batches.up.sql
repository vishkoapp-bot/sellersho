-- 202611210001_fin014_repair_batches (FIN-014 §3.3, W4 — additive).
--
-- Repair-batch bookkeeping for the staged financial cutover: report
-- runs (one row per dry-run report, with the oracle diff and the
-- pinned FIN-011 flag), repair batches (one proposal per report, with
-- the Q8 approval record the apply command requires), and repair
-- operations (one content-derived operation per defect, with its
-- expected-before predicates and cash-basis gating).
--
-- Class EXPAND, verdict compatible: three brand-new tables the old
-- binary never touches. No history rewrite — nothing here updates or
-- deletes ledger entries, receipts, obligations, or order snapshots;
-- repairs are compensating events written by the domain writers (W6),
-- recorded here as operations. No new writer is added by this file.
--
-- Immutability: report runs are append-only evidence (no UPDATE or
-- DELETE ever). A batch becomes immutable once applied. An operation's
-- proposal content (class, record, effect, expected-before,
-- explanation, cash basis, confidence) is frozen at proposal time;
-- only its lifecycle columns (status, hold_reason, applied_at) may
-- move, and never after it is applied.

CREATE TABLE fin014_report_runs (
  id TEXT PRIMARY KEY,
  prefix TEXT NOT NULL DEFAULT '',
  cycle_period TEXT NOT NULL DEFAULT '',
  schema_version INT NOT NULL,
  total_rows INT NOT NULL CHECK (total_rows >= 0),
  certain_count INT NOT NULL CHECK (certain_count >= 0),
  ambiguous_count INT NOT NULL CHECK (ambiguous_count >= 0),
  unresolved_count INT NOT NULL CHECK (unresolved_count >= 0),
  oracle_checks JSONB NOT NULL DEFAULT '{}'::jsonb,
  fin011_pinned BOOLEAN NOT NULL DEFAULT TRUE,
  report_json JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX fin014_report_runs_created_at ON fin014_report_runs(created_at);

CREATE TABLE fin014_repair_batches (
  id TEXT PRIMARY KEY,
  report_run_id TEXT NOT NULL REFERENCES fin014_report_runs(id) ON DELETE RESTRICT,
  status TEXT NOT NULL DEFAULT 'proposed'
    CHECK (status IN ('proposed','approved','applying','applied','failed','aborted')),
  created_by TEXT NOT NULL DEFAULT '',
  approved_by TEXT,
  approved_at TIMESTAMPTZ,
  applied_at TIMESTAMPTZ,
  last_applied_operation_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  CHECK (status <> 'approved' OR (approved_by IS NOT NULL AND approved_at IS NOT NULL))
);
CREATE INDEX fin014_repair_batches_run_status ON fin014_repair_batches(report_run_id, status);

CREATE TABLE fin014_repair_operations (
  id TEXT PRIMARY KEY,
  batch_id TEXT NOT NULL REFERENCES fin014_repair_batches(id) ON DELETE RESTRICT,
  class TEXT NOT NULL,
  record_id TEXT NOT NULL,
  record_kind TEXT NOT NULL,
  store_id TEXT NOT NULL DEFAULT '',
  order_id TEXT NOT NULL DEFAULT '',
  effect JSONB NOT NULL,
  expected_before JSONB NOT NULL,
  explanation TEXT NOT NULL,
  cash_basis TEXT NOT NULL CHECK (cash_basis IN ('verified_gateway','simulated','unverified','unknown')),
  confidence TEXT NOT NULL CHECK (confidence IN ('certain','ambiguous','unresolved')),
  status TEXT NOT NULL DEFAULT 'proposed'
    CHECK (status IN ('proposed','held','applied','skipped','failed')),
  hold_reason TEXT,
  applied_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX fin014_repair_operations_batch_status ON fin014_repair_operations(batch_id, status);
CREATE INDEX fin014_repair_operations_class_record ON fin014_repair_operations(class, record_id);

CREATE FUNCTION fin014_immutable_report_run() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'fin014 report runs are append-only evidence (FIN-014 W4)';
END $$;
CREATE TRIGGER fin014_report_run_immutable BEFORE UPDATE OR DELETE ON fin014_report_runs
  FOR EACH ROW EXECUTE FUNCTION fin014_immutable_report_run();

CREATE FUNCTION fin014_immutable_batch_once_applied() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.applied_at IS NOT NULL OR OLD.status = 'applied' THEN
    RAISE EXCEPTION 'fin014 repair batch is immutable once applied (FIN-014 W4)';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER fin014_repair_batch_immutable BEFORE UPDATE OR DELETE ON fin014_repair_batches
  FOR EACH ROW EXECUTE FUNCTION fin014_immutable_batch_once_applied();

CREATE FUNCTION fin014_immutable_operation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  batch_applied TIMESTAMPTZ;
BEGIN
  IF OLD.status = 'applied' THEN
    RAISE EXCEPTION 'fin014 repair operation is immutable once applied (FIN-014 W4)';
  END IF;
  SELECT applied_at INTO batch_applied FROM fin014_repair_batches WHERE id = OLD.batch_id;
  IF batch_applied IS NOT NULL THEN
    RAISE EXCEPTION 'fin014 repair operation is immutable once its batch is applied (FIN-014 W4)';
  END IF;
  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  -- A failed operation may be re-proposed with fresh predicates (W6
  -- resumability): only status, hold_reason, applied_at,
  -- expected_before and batch_id may change, and only from 'failed'.
  -- The effect, class, record and explanation stay frozen, so the
  -- operation ID (content-derived) still identifies the same repair
  -- and a re-proposal cannot duplicate an applied one.
  IF OLD.status = 'failed' THEN
    IF (OLD.id, OLD.class, OLD.record_id, OLD.record_kind, OLD.store_id, OLD.order_id,
        OLD.effect, OLD.explanation, OLD.cash_basis, OLD.confidence, OLD.created_at)
      IS DISTINCT FROM
       (NEW.id, NEW.class, NEW.record_id, NEW.record_kind, NEW.store_id, NEW.order_id,
        NEW.effect, NEW.explanation, NEW.cash_basis, NEW.confidence, NEW.created_at) THEN
      RAISE EXCEPTION 'fin014 failed operation may only refresh its predicates and lifecycle (FIN-014 W4)';
    END IF;
    RETURN NEW;
  END IF;
  IF (OLD.id, OLD.batch_id, OLD.class, OLD.record_id, OLD.record_kind, OLD.store_id, OLD.order_id,
      OLD.effect, OLD.expected_before, OLD.explanation, OLD.cash_basis, OLD.confidence, OLD.created_at)
    IS DISTINCT FROM
     (NEW.id, NEW.batch_id, NEW.class, NEW.record_id, NEW.record_kind, NEW.store_id, NEW.order_id,
      NEW.effect, NEW.expected_before, NEW.explanation, NEW.cash_basis, NEW.confidence, NEW.created_at) THEN
    RAISE EXCEPTION 'fin014 repair operation proposal content is frozen at proposal time (FIN-014 W4)';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER fin014_repair_operation_immutable BEFORE UPDATE OR DELETE ON fin014_repair_operations
  FOR EACH ROW EXECUTE FUNCTION fin014_immutable_operation();
