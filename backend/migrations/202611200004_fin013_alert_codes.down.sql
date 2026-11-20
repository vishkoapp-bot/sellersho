-- Down migration for 202611200004 (FIN-013 alert codes).
-- Restores the six-code CHECK. Only run on a disposable database, and
-- only after deleting any finance_invariant / finance_unknown_stale
-- rows from observability_alert_state (the CHECK would reject them on
-- the way back in — the upsert in DeliverAlerts would fail instead).
-- Only run on a disposable database.
ALTER TABLE observability_alert_state DROP CONSTRAINT observability_alert_state_code_check;
ALTER TABLE observability_alert_state ADD CONSTRAINT observability_alert_state_code_check CHECK (code IN ('api_errors','dependency_loss','job_stale','disk_pressure','backup_failure','slo_breach'));
