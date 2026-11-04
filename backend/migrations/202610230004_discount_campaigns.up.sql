-- 202610230004: Time-boxed seller discount campaigns (Seller Growth
-- Phase 4, user decision B).
--
-- A campaign sets an ABSOLUTE target margin (percent of base cost B, or a
-- fixed Toman amount — the same two modes as the existing seller pricing
-- overrides) for a scope (one product / one niche / the whole store) over
-- a time window. Inside the window the campaign REPLACES the normal
-- margin tier in pricing.ResolvePricing. The discount comes ONLY out of
-- the seller's own margin: the order snapshot's wholesale_cost (C) and
-- platform_fee (F) are never touched by a campaign.
--
-- Simultaneous campaigns: the more specific scope wins
-- (product > niche > store), then the most recently created.
--
-- The campaign floor is zero (the seller may give up their whole margin)
-- and the resulting margin can never go negative; the admin margin lock
-- (stores.is_margin_locked_by_admin) blocks campaign creation AND
-- application, so a campaign can never bypass an admin lock.
--
-- target_id is the product id for scope='product', the niche id for
-- scope='niche', and '' for scope='store'.

CREATE TABLE IF NOT EXISTS seller_discount_campaigns (
  id TEXT PRIMARY KEY,
  store_id TEXT NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('product', 'niche', 'store')),
  target_id TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL CHECK (mode IN ('percent', 'fixed')),
  value DOUBLE PRECISION NOT NULL,
  starts_at TIMESTAMPTZ NOT NULL,
  ends_at TIMESTAMPTZ NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (ends_at > starts_at),
  CHECK (value >= 0)
);

CREATE INDEX IF NOT EXISTS idx_seller_discount_campaigns_store_window
  ON seller_discount_campaigns (store_id, is_active, starts_at, ends_at);
