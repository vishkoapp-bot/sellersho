-- 202610260001: Stuck-order auto-expiry kill-switch (M2-02, self-improvement 2026-10-26).
--
-- The 48h stuck-order expiry job (backend/cmd/server/stuck_order_expiry_job.go)
-- reads this flag on every pass. Default FALSE (guard-only mode): the job
-- lists and logs what it WOULD expire but cancels nothing, until an admin
-- explicitly enables it. Turning the job off again is a single UPDATE —
-- no redeploy needed (M2-02 rollback plan).
ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS stuck_order_expiry_enabled BOOLEAN NOT NULL DEFAULT FALSE;
