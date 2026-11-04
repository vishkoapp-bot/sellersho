-- 202611030002: Dropshipper discount codes (user decision, 2026-11-03).
-- A dropshipper may discount a product (existing seller_discount_campaigns
-- table, now activated) or create a discount code. In both cases the
-- discount comes ONLY out of the dropshipper's own profit margin:
-- wholesale cost and platform fee are never touched, and the discounted
-- margin is stored on the order snapshot so settlement pays the reduced
-- profit. Code mode 'percent' is a percent of the final price, capped
-- at the remaining margin; mode 'fixed' is a fixed Toman amount, also
-- capped at the remaining margin.
CREATE TABLE IF NOT EXISTS seller_discount_codes (
  id TEXT PRIMARY KEY,
  store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
  code TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('percent', 'fixed')),
  value DOUBLE PRECISION NOT NULL CHECK (value >= 0),
  starts_at TIMESTAMPTZ NOT NULL,
  ends_at TIMESTAMPTZ NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  max_uses INT CHECK (max_uses IS NULL OR max_uses > 0),
  used_count INT NOT NULL DEFAULT 0 CHECK (used_count >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (ends_at > starts_at),
  UNIQUE (store_id, code)
);
CREATE INDEX IF NOT EXISTS idx_seller_discount_codes_store_active
  ON seller_discount_codes (store_id, is_active, starts_at, ends_at);

-- Order snapshot: the seller discount actually applied, and the code used.
-- discount_amount on orders keeps its existing anchor-price meaning;
-- seller_discount_amount is the margin-only discount, so the two are
-- never conflated.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS seller_discount_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS discount_code TEXT;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS discount_amount BIGINT NOT NULL DEFAULT 0;
