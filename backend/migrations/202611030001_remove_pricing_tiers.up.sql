-- 202611030001: Remove tiered supplier pricing (user decision, 2026-11-03).
-- The supplier_pricing_tiers engine was removed from backend and frontend:
-- no API, no supplier panel tab, no pricing context, and ResolvePricing
-- never used the tiers anyway. The table held 0 rows at removal time.
-- Do NOT edit the original 000007 migration; this new migration drops
-- the table at deploy time only.
DROP TABLE IF EXISTS supplier_pricing_tiers;
