CREATE TABLE observability_alert_state (
 code text PRIMARY KEY CHECK(code IN ('api_errors','dependency_loss','job_stale','disk_pressure','backup_failure')),
 active boolean NOT NULL DEFAULT false,
 delivery_pending boolean NOT NULL DEFAULT false,
 notified_at timestamptz NOT NULL DEFAULT '-infinity',
 updated_at timestamptz NOT NULL DEFAULT now()
);
