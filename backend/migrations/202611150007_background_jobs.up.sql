-- Additive JOB-01 cutover. Existing histories and all business rows are retained.
CREATE TABLE background_job_state (
 job TEXT PRIMARY KEY, owner TEXT, lease_until TIMESTAMPTZ,
 started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ, last_success_at TIMESTAMPTZ,
 last_error TEXT NOT NULL DEFAULT '', disabled BOOLEAN NOT NULL DEFAULT FALSE,
 runs BIGINT NOT NULL DEFAULT 0, affected BIGINT NOT NULL DEFAULT 0,
 cutover_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE background_job_outcomes (
 run_id TEXT PRIMARY KEY, job TEXT NOT NULL REFERENCES background_job_state(job),
 started_at TIMESTAMPTZ NOT NULL, finished_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 duration_ms BIGINT NOT NULL, affected BIGINT NOT NULL, confirmed BIGINT NOT NULL,
 disabled BOOLEAN NOT NULL, error_code TEXT NOT NULL
);
CREATE INDEX background_job_outcomes_history_idx ON background_job_outcomes(job,finished_at DESC);
CREATE TABLE background_job_candidate_retry (
 job TEXT NOT NULL, candidate TEXT NOT NULL, next_retry_at TIMESTAMPTZ NOT NULL,
 reason TEXT NOT NULL, PRIMARY KEY(job,candidate)
);
CREATE INDEX background_job_retry_due_idx ON background_job_candidate_retry(job,next_retry_at);
CREATE INDEX orders_job_delivery_confirm_idx ON orders(shipped_at,id)
 WHERE status IN ('shipped','delivered_unconfirmed') AND profit_released=FALSE;
CREATE INDEX orders_job_delivery_release_idx ON orders(return_window_ends_at,id)
 WHERE status='delivered_confirmed' AND profit_released=FALSE;
CREATE INDEX ad_request_limits_cleanup_idx ON ad_request_limits(bucket);
CREATE INDEX supplier_sla_assessment_idx ON supplier_sla_metrics(assessed_at,supplier_id);
CREATE INDEX orders_job_payment_deadline_idx ON orders(payment_expires_at,id COLLATE "C")
 WHERE payment_status='pending' AND status IN ('pending','processing') AND NOT inventory_captured;
