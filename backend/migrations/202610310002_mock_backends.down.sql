-- 202610310002 DOWN (rewritten 2026-10-31, staging-blocker fix).
--
-- The original down dropped product_reviews and subscription_plans
-- outright and dropped five platform_settings columns. That destroys
-- PRE-EXISTING objects on databases that ran the older chain, because
-- 0002's up was a no-op for them (CREATE TABLE / ADD COLUMN IF NOT
-- EXISTS). Evidence from the pre-0002 migration chain:
--   * product_reviews PRE-EXISTS in the old order-linked shape
--     (202610200003_product_brand_and_reviews) -> MUST NOT be dropped.
--     Its migration to the new shape is 0004, not this down.
--   * subscription_plans PRE-EXISTS (202610160009_marketing_admin, with
--     its own seed rows and a sort_order column) -> MUST NOT be dropped.
--     0002's seed INSERTs (ON CONFLICT DO NOTHING) are left in place:
--     pre-existing seed rows are indistinguishable from 0002's, and
--     deleting them would destroy pre-existing data.
--   * platform_settings.product_fee_overrides PRE-EXISTS
--     (202610160007_finance_admin_settings), warehouse_tariffs
--     PRE-EXISTS (202610160008_warehouse_tariffs), trial_catalog_quota
--     and central_campaign_budget PRE-EXIST (202610160009) -> MUST NOT
--     be dropped.
-- Dropped below are ONLY objects 0002 itself created, i.e. objects with
-- no definition anywhere in the pre-0002 chain:
--   * customer_wishlists, customer_addresses (new tables),
--   * platform_settings.packaging_tariffs (new column).
-- Assumption documented: no definition of those three exists outside
-- the migration chain checked. If a target database gained them from
-- another source, this down would drop that data too.

DROP TABLE IF EXISTS customer_wishlists;
DROP TABLE IF EXISTS customer_addresses;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS packaging_tariffs;
