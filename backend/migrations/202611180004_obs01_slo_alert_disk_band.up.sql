-- 202611180004_obs01_slo_alert_disk_band (OBS-01 critic fix-2, C-01 + C-11).
--
-- 1. C-01: 'slo_breach' joins the alert-code CHECK so the in-app poller
--    can deliver the latency-SLO breach to the admin inbox. The Prometheus
--    rules SellershoLatencySLOBreachRead/Mutation
--    (ops/observability/alerts.yml) have NO delivery path of their own
--    (no Alertmanager, no Grafana contact point anywhere in the stack);
--    the in-app poller is the only operator channel, so the SLO breach
--    needs a matching in-app condition or it can never reach an operator.
--    The thresholds are the agreed CAP-01 SLO (read p95 300 ms / p99
--    1000 ms; mutation p95 750 ms / p99 2000 ms), same as the rules.
-- 2. C-11: severity_band records the worst disk-pressure band seen during
--    the current disk_pressure incident so a still-active, worsening
--    incident can re-notify instead of going silent until recovery.
--    Bands: 0 healthy (>= 15% free), 1 < 15%, 2 < 10%, 3 < 5%, 4 < 2%.
--    The band resets to 0 when the incident recovers; it only ever grows
--    while the incident stays active.
ALTER TABLE observability_alert_state DROP CONSTRAINT observability_alert_state_code_check;
ALTER TABLE observability_alert_state ADD CONSTRAINT observability_alert_state_code_check CHECK (code IN ('api_errors','dependency_loss','job_stale','disk_pressure','backup_failure','slo_breach'));
ALTER TABLE observability_alert_state ADD COLUMN IF NOT EXISTS severity_band int NOT NULL DEFAULT 0;
COMMENT ON COLUMN observability_alert_state.severity_band IS 'Worst disk-pressure band (0 healthy, 1 <15%, 2 <10%, 3 <5%, 4 <2% free) seen during the current disk_pressure incident; reset to 0 on recovery. Only meaningful for code=disk_pressure (OBS-01 C-11).';
