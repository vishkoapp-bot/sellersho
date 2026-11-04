-- Rollback for migration 202610160009.
DROP TABLE IF EXISTS subscription_plans;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS trial_catalog_quota;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS central_campaign_budget;
