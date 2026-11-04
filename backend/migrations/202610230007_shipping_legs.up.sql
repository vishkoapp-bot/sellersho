-- 202610230007: Per-supplier shipping legs.
--
-- Until now an order's shipping was ONE number: the platform formula
-- (base + perKg × ceil(totalWeight/1000)) over the whole basket. The
-- legs model prices each physical parcel separately: one leg per
-- distinct supplier whose items ship supplier-direct, plus one
-- «انبار مرکزی» (central warehouse) leg for warehouse-routed items.
-- orders.shipping_cost stays = Σ legs, so the ledger identity and every
-- existing money surface are untouched; the per-leg rows below are the
-- auditable breakdown behind that single number.
--
-- order_shipping_legs.supplier_id is INTERNAL ONLY (operations /
-- settlement): it is never serialized into a customer-facing payload —
-- customers see only origin_label (the supplier's warehouse CITY) and
-- the fare. The repository struct marks the field json:"-" as a
-- structural backstop.
--
-- source is 'platform_formula' for every v1 leg: Tapin has no known
-- pre-registration price endpoint (its real send_price exists only
-- AFTER POST /order/post/register/), so no leg may pretend to be a
-- carrier quote. The column leaves room for a future 'tapin' source
-- per leg without a schema change.
--
-- estimated = TRUE when at least one line in the leg had no known
-- product weight and the platform default
-- (platform_settings.default_item_weight_grams) was substituted — the
-- UI shows such legs as «برآوردی» instead of presenting an estimate
-- as exact.
--
-- supplier_shipments.send_price: the REAL Tapin send_price captured at
-- registration time (supplier_tapin_handlers.go), stored VERBATIM in
-- the unit Tapin returns (Rials). Internal accounting only; never
-- customer-facing.

CREATE TABLE IF NOT EXISTS order_shipping_legs (
  id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  order_id VARCHAR(255) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  leg_index INT NOT NULL,
  origin_label TEXT NOT NULL,
  supplier_id VARCHAR(255) REFERENCES suppliers(id) ON DELETE SET NULL,
  fare BIGINT NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT 'platform_formula',
  estimated BOOLEAN NOT NULL DEFAULT FALSE,
  weight_grams INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (order_id, leg_index)
);

CREATE INDEX IF NOT EXISTS idx_order_shipping_legs_order
  ON order_shipping_legs (order_id, leg_index);

ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS default_item_weight_grams INTEGER NOT NULL DEFAULT 500;

ALTER TABLE supplier_shipments
  ADD COLUMN IF NOT EXISTS send_price BIGINT;
