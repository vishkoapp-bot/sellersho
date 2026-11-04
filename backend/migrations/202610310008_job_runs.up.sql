-- 202610310008: job_runs — persisted history for the admin jobs tab.
--
-- Phase 2 (2026-11-01): the delivery-lock-releaser job (two-stage:
-- auto-confirm, then profit release after the return window) records
-- every cycle here, in addition to the worker's in-memory history
-- (the dist notes history "is read from service memory and cleared
-- on server restart" — persisting it keeps the tab truthful across
-- restarts without changing its API shape). The worker and the jobs
-- handlers also CREATE TABLE IF NOT EXISTS at runtime, so this
-- migration is belt-and-braces, not a hard dependency.
CREATE TABLE IF NOT EXISTS job_runs (
  id BIGSERIAL PRIMARY KEY,
  job_id TEXT NOT NULL,
  started_at TIMESTAMPTZ NOT NULL,
  released BIGINT NOT NULL DEFAULT 0,
  confirmed BIGINT NOT NULL DEFAULT 0,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_job_runs_job_started ON job_runs (job_id, started_at DESC);
