-- Down migration for 202611200003 (EDGE-01 allow-check index).
-- Drops only the EDGE-01 pending_approvals lookup index. The allow
-- handlers keep working without it (they degrade to the pre-EDGE-01
-- sequential scan of pending_approvals per custom-domain check).
-- Only run on a disposable database.
DROP INDEX IF EXISTS idx_edge01_pending_approvals_target_lookup;
