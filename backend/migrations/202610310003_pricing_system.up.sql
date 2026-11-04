-- Pricing system (2026-10-31, Andrew's decided policy).
-- Follows the 202610310002 style: additive columns on platform_settings
-- plus standalone tables only. No ALTER TABLE products or stores: no DDL
-- for those tables exists in this working copy, so altering them here
-- could break the migration chain.
--
-- Assumes the base tables already exist (platform_settings, products,
-- support_tickets), exactly as 202610310002 already assumes for
-- platform_settings. platform_settings is the singleton row id = 1.
--
-- Defaults / undecided items (do NOT silently resolve them):
--   platform_pct       default 0 until admin sets it. No platform %
--                      value is invented here, and its scope (global vs
--                      per-category/niche/product) is UNDECIDED: this
--                      column is global-only until Andrew decides.
--   central_profit_pct default 10. UNDECIDED 0 vs 10: the 10 default
--                      preserves the frontend central behaviour
--                      (base x 1.10) while the backend central
--                      previously used base (profit 0). FLAG, not a
--                      decision.
--   increase_cap_pct   default 10 (Andrew's example cap).
--   increase_locked    default TRUE = locked. Unlocking is manual only:
--                      admin sets increase_locked = FALSE via
--                      PUT /api/v1/admin/settings/pricing. There is NO
--                      calendar / allowed-days mechanism in this change.
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS platform_pct DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS central_profit_pct DOUBLE PRECISION NOT NULL DEFAULT 10;
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS increase_cap_pct DOUBLE PRECISION NOT NULL DEFAULT 10;
ALTER TABLE platform_settings ADD COLUMN IF NOT EXISTS increase_locked BOOLEAN NOT NULL DEFAULT TRUE;

-- Ticket routing: the existing support_tickets table has NO section /
-- category column (its inserts use only id, requester_user_id,
-- requester_role, subject). This column is the finance routing for
-- reference-increase exception tickets (section = 'finance').
ALTER TABLE support_tickets ADD COLUMN IF NOT EXISTS section TEXT NOT NULL DEFAULT '';

-- Supplier reference-price increase requests. The reference price itself
-- lives in products.supplier_base_cost (C); approving a row updates that
-- column (see PricingRepository.ReviewPriceIncrease). No stored final
-- price exists to update: display/order finals are derived at read time
-- by pricing.ResolvePricing, and order_items rows are historical
-- snapshots that must NOT be rewritten on approval.
CREATE TABLE IF NOT EXISTS supplier_price_increases (
    id                TEXT PRIMARY KEY,
    product_id        TEXT NOT NULL,
    supplier_id       TEXT NOT NULL,
    old_reference     BIGINT NOT NULL CHECK (old_reference > 0),
    new_reference     BIGINT NOT NULL CHECK (new_reference > old_reference),
    pct               DOUBLE PRECISION NOT NULL CHECK (pct > 0),
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    is_exception      BOOLEAN NOT NULL DEFAULT FALSE,
    exception_cap_pct DOUBLE PRECISION,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at       TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_supplier_price_increases_supplier ON supplier_price_increases (supplier_id);
CREATE INDEX IF NOT EXISTS idx_supplier_price_increases_product_status ON supplier_price_increases (product_id, status);
CREATE INDEX IF NOT EXISTS idx_supplier_price_increases_status ON supplier_price_increases (status);

-- Market ceiling per product (the "market price" cap). When no row
-- exists for a product, the reference resolver leaves the dropshipper
-- final uncapped and flags it (FLAG: ceiling fallback when unset is
-- UNDECIDED — uncapped is the implemented behaviour, not a decision).
CREATE TABLE IF NOT EXISTS product_market_ceilings (
    product_id TEXT PRIMARY KEY,
    ceiling    BIGINT NOT NULL CHECK (ceiling > 0),
    set_by     TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Per-product exception grants, product-scoped ONLY, never global.
-- A grant is created by admin from a finance-section exception ticket
-- and lets that one product's increase requests use exception_cap_pct
-- instead of the global increase_cap_pct (including while locked).
-- A later grant for the same product replaces the earlier one (upsert).
CREATE TABLE IF NOT EXISTS product_price_exceptions (
    product_id        TEXT PRIMARY KEY,
    exception_cap_pct DOUBLE PRECISION NOT NULL CHECK (exception_cap_pct > 0),
    ticket_id         TEXT,
    granted_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
