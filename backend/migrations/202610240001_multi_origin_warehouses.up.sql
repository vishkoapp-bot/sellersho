-- 202610240001: Multi-origin shipping — central warehouses + per-warehouse
-- stock attribution + per-item origin persistence.
--
-- Until now every warehouse-routed item collapsed into ONE «انبار مرکزی»
-- leg, and stock was one global number per product/variant: the system
-- could not say "this item ships from Mashhad because THAT warehouse
-- holds it". This migration adds the missing data, without touching the
-- ledger's authority:
--
--   warehouses: platform central warehouses, each with a Tapin geography
--     key (province_id/city_id for future carrier quoting) AND a display
--     city name (the same-city tie-break at quote time matches on the
--     normalized city NAME, never on the Tapin id — see
--     shipping_allocation.go — so quote and order creation, which only
--     ever sees names, can never disagree).
--   inventory_balances.warehouse_id: attributes a balance row to one
--     warehouse. NULL = legacy unattributed stock (all pre-feature rows):
--     it keeps today's single «انبار مرکزی» leg and FIFO debit — the
--     feature is strictly additive, never a rewrite of history.
--   order_shipping_legs.warehouse_id: the leg's warehouse identity
--     (mirrors supplier_id for direct legs; both stay internal-only and
--     never reach a customer payload).
--   order_item_origins: the persisted allocation — one row per (order
--     item × leg) split part. A line split across two warehouses gets
--     two rows; a direct line gets one. Payment capture debits exactly
--     these origins; cancel-restore credits them back. The ledger
--     (inventory_balances + inventory_movements) stays the ONLY stock
--     authority — this table records WHERE the ledger movement belongs,
--     it is never itself a stock counter.
--
-- Display rule untouched: products.stock is still SUM(sellable) over ALL
-- balance rows (attributed + legacy), via the existing sync + trigger.

CREATE TABLE IF NOT EXISTS warehouses (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  tapin_province_id INTEGER NOT NULL,
  tapin_city_id INTEGER NOT NULL,
  province_name TEXT NOT NULL,
  city_name TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_warehouses_active_priority
  ON warehouses (is_active, priority, id);

ALTER TABLE inventory_balances
  ADD COLUMN IF NOT EXISTS warehouse_id TEXT REFERENCES warehouses(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_inventory_balances_warehouse
  ON inventory_balances (product_id, warehouse_id, status);

ALTER TABLE order_shipping_legs
  ADD COLUMN IF NOT EXISTS warehouse_id TEXT REFERENCES warehouses(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS order_item_origins (
  id TEXT PRIMARY KEY,
  order_id VARCHAR(255) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  order_item_id UUID NOT NULL REFERENCES order_items(id) ON DELETE CASCADE,
  leg_index INT NOT NULL,
  origin_type TEXT NOT NULL CHECK (origin_type IN ('supplier', 'warehouse', 'legacy')),
  supplier_id VARCHAR(255) REFERENCES suppliers(id) ON DELETE SET NULL,
  warehouse_id TEXT REFERENCES warehouses(id) ON DELETE SET NULL,
  origin_label TEXT NOT NULL,
  quantity INT NOT NULL CHECK (quantity > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (order_item_id, leg_index)
);

CREATE INDEX IF NOT EXISTS idx_order_item_origins_order
  ON order_item_origins (order_id, order_item_id, leg_index);
