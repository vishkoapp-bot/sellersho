-- M3 LIVE PROPOSAL (deploy-time only — DO NOT run from staging work, and
-- NEVER run without a full database backup first).
--
-- What it does:
--   Normalises pre-existing store-scope discount campaigns so the Fix69
--   resolver can find them. Fix69's code normalises target_id to '' only
--   on create/update; rows already stored with a non-empty target_id never
--   match, because GetActiveDiscountCampaign queries
--   scope='store' AND target_id='' (backend/internal/repository/discount_repo.go).
--
-- Why it is safe:
--   * Guarded: only rows with scope='store' AND target_id <> '' are touched.
--     Product- and niche-scope campaigns are untouched, as are store-scope
--     rows already at ''.
--   * Idempotent: re-running it after a successful run affects 0 rows.
--   * It sets target_id to exactly the value the resolver queries for, so
--     it cannot create a new match shape — it only repairs rows that were
--     previously unmatchable.
--   * No other column is changed (no name, mode, value, window or is_active
--     change), and no rows are inserted or deleted.
--
-- Before running on live, re-count first (staging had 0 such rows, so the
-- live count must be measured on live, not copied from staging):
--   SELECT COUNT(*) FROM seller_discount_campaigns
--     WHERE scope='store' AND target_id <> '';
-- After running, verify:
--   SELECT COUNT(*) FROM seller_discount_campaigns
--     WHERE scope='store' AND target_id <> '';  -- must be 0
--   SELECT COUNT(*) FROM seller_discount_campaigns
--     WHERE scope='store' AND target_id = '';   -- resolver-matchable count
--
-- Staging result (for reference only): 0 rows affected — the staging
-- seller_discount_campaigns table was empty (0 rows total).

UPDATE seller_discount_campaigns
SET target_id = ''
WHERE scope = 'store'
  AND target_id <> '';
