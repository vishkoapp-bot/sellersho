-- Migration 202610250301: supplier visibility of customer returns + request-back.
--
-- Policy (user decision 2026-10-25 ~17:50, RETURNS SHIPPING POLICY — FULL RULES):
-- returned products show in the SUPPLIER panel, and the supplier can request
-- the returned goods back from the returns warehouse.
--
-- FINDING (verified 2026-10-25, stream 3): HandleGetReturns /
-- GetReturnRequests today serves ONLY the supplier's own return requests to
-- the warehouse (supplier_return_requests: the supplier returning sellable
-- stock). Customer returns (customer_returns / return_items) were invisible
-- to suppliers anywhere. This migration builds the missing side.
--
-- 1. supplier_return_back_requests — one row per supplier request to take
--    physical returned units back from the returns warehouse. Statuses:
--      requested -> (warehouse review) -> approved | rejected
--      approved -> shipped_back (warehouse dispatches, records carrier/tracking)
--      shipped_back -> completed (supplier confirms receipt)
--      requested -> cancelled (supplier withdraws before review)
--    inventory_source records how the units were reserved at request time:
--      'canonical' — QC-ok units, reserved out of the restocked sellable
--        balance at location_key 'inbound:<customer_return_id>';
--      'legacy'    — QC-ok units of a legacy product (no balance rows),
--        reserved against products.stock directly;
--      'none'      — QC-defective units, never restocked, nothing to reserve.
-- 2. supplier_return_back_events — audit trail mirroring
--    supplier_return_events (idempotency-keyed, one event per transition).
-- 3. customer_returns columns shared with the money stream (which adds
--    return_shipping_amount / return_shipping_bearer) and the warehouse
--    stream (return_code): added IF NOT EXISTS so this migration is a no-op
--    for any column another stream's migration already created, and vice
--    versa. No CHECK constraints are added here — the owning stream defines
--    policy constraints on its own columns.

CREATE TABLE IF NOT EXISTS supplier_return_back_requests (
  id VARCHAR(60) PRIMARY KEY,
  customer_return_id UUID NOT NULL REFERENCES customer_returns(id) ON DELETE RESTRICT,
  return_item_id UUID NOT NULL REFERENCES return_items(id) ON DELETE RESTRICT,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  quantity INT NOT NULL CHECK (quantity > 0),
  status VARCHAR(30) NOT NULL DEFAULT 'requested'
    CHECK (status IN ('requested', 'approved', 'rejected', 'shipped_back', 'completed', 'cancelled')),
  reason TEXT NOT NULL DEFAULT '',
  review_reason TEXT NOT NULL DEFAULT '',
  reviewed_by VARCHAR(80),
  reviewed_at TIMESTAMPTZ,
  shipped_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  carrier_code VARCHAR(40),
  tracking_number VARCHAR(100),
  inventory_source VARCHAR(20) NOT NULL DEFAULT 'none'
    CHECK (inventory_source IN ('none', 'canonical', 'legacy')),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_supplier_return_back_supplier ON supplier_return_back_requests(supplier_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_supplier_return_back_item ON supplier_return_back_requests(return_item_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_supplier_return_back_status ON supplier_return_back_requests(status, created_at DESC);
-- At most one live request per returned item (requested/approved/shipped_back).
-- Rejected/cancelled/completed rows do not block a fresh request.
CREATE UNIQUE INDEX IF NOT EXISTS uq_supplier_return_back_active_item
  ON supplier_return_back_requests(return_item_id)
  WHERE status IN ('requested', 'approved', 'shipped_back');

CREATE TABLE IF NOT EXISTS supplier_return_back_events (
  id VARCHAR(80) PRIMARY KEY,
  back_request_id VARCHAR(60) NOT NULL REFERENCES supplier_return_back_requests(id) ON DELETE CASCADE,
  from_status VARCHAR(30),
  to_status VARCHAR(30) NOT NULL,
  reason TEXT,
  actor_user_id VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_supplier_return_back_events_req ON supplier_return_back_events(back_request_id, created_at);

-- Shared customer_returns columns (money stream: return_shipping_*;
-- warehouse stream: return_code). IF NOT EXISTS keeps the merge
-- idempotent whichever stream's migration runs first.
ALTER TABLE customer_returns ADD COLUMN IF NOT EXISTS return_code VARCHAR(32);
ALTER TABLE customer_returns ADD COLUMN IF NOT EXISTS return_shipping_amount BIGINT;
ALTER TABLE customer_returns ADD COLUMN IF NOT EXISTS return_shipping_bearer VARCHAR(20);
