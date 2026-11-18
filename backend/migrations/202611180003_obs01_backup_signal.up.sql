-- 202611180003_obs01_backup_signal (OBS-01, D-02).
--
-- The verified backup-success signal. The nightly backup publisher
-- (scripts/publish-backup-success.sh, called from backup-auto.sh)
-- upserts one row per backup kind ONLY after verifying the backup
-- artifacts on disk (non-empty + gzip -t clean). The observability
-- poller (internal/observability/dependencies.go pollBackupSignal)
-- reads MAX(last_success_at) and publishes
-- sellersho_backup_last_success_timestamp_seconds; the backup_failure
-- alert fires when that timestamp goes stale (> 26 h).
--
-- Before this migration the gauge existed only in the poller's read
-- path and nothing ever wrote it, so backup_failure could never fire
-- and the Prometheus absent() rule fired permanently instead.
CREATE TABLE IF NOT EXISTS backup_signals (
    kind text PRIMARY KEY,
    last_success_at timestamptz NOT NULL,
    verified_artifacts text NOT NULL DEFAULT '',
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE backup_signals IS 'Verified backup-success timestamps published by the backup publisher; read by the observability poller (OBS-01 D-02).';
