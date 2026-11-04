-- 202611020004_ad_event_rejections.up.sql: LIVE-PARITY RECONSTRUCTION (D67).
--
-- The ad_event_rejections table exists in the live database (1 row)
-- with exactly the columns below (MASTER-AUDIT D67), but no
-- CREATE TABLE for it exists in any source migration or source Go
-- file. This migration adds it to the chain.
--
-- TODO(U72): the table's origin is unverified — it is not known
-- whether it was created by a lost migration or at runtime in the
-- merged tree. The filename here is reconstruction-assigned (the
-- next number after 202611020003, the last live record); it is NOT
-- claimed to be a live schema_migrations record name.
--
-- Column types are reconstruction defaults, not live-verified types:
-- the audit recorded column names only. campaign_id is TEXT with no
-- foreign key on purpose: the referenced campaign table is
-- unverified (seller_campaigns uses UUID ids, other campaign tables
-- differ), and a wrong FK would break inserts. Do not add an FK
-- until U72 is resolved. id uses BIGSERIAL, matching job_runs and
-- search_queries in this chain.
--
-- IF NOT EXISTS throughout, so re-applying this migration on a
-- database that already has the table is a no-op and cannot trip
-- the fail-closed runner.
CREATE TABLE IF NOT EXISTS ad_event_rejections (
  id BIGSERIAL PRIMARY KEY,
  campaign_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  token_prefix TEXT NOT NULL,
  reason TEXT NOT NULL,
  ip_address TEXT NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ad_event_rejections_campaign
  ON ad_event_rejections (campaign_id);
