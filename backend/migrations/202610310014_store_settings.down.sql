-- 202610310014 rollback (Cluster D): drop only the columns this
-- migration added. panel_config copies of logo/addonSectionTitle
-- are untouched by this rollback.
ALTER TABLE stores DROP COLUMN IF EXISTS logo;
ALTER TABLE disputes DROP COLUMN IF EXISTS financial_decision;
