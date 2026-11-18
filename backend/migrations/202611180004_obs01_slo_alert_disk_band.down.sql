-- 202611180004_obs01_slo_alert_disk_band (OBS-01 critic fix-2) rollback.
-- The slo_breach rows must go first: the restored CHECK does not admit
-- that code.
DELETE FROM observability_alert_state WHERE code = 'slo_breach';
ALTER TABLE observability_alert_state DROP COLUMN IF EXISTS severity_band;
ALTER TABLE observability_alert_state DROP CONSTRAINT observability_alert_state_code_check;
ALTER TABLE observability_alert_state ADD CONSTRAINT observability_alert_state_code_check CHECK (code IN ('api_errors','dependency_loss','job_stale','disk_pressure','backup_failure'));
