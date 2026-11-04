-- Supplier warehouses + per-product origin + inbound destination (2026-10-28).
-- Design: ~/workspace/warehouse-model-20261028/design.md §2, §3, §4, §5, §7.
-- All new columns are nullable / defaulted: no backfill; every live row
-- (117 products, 2 live inbounds, 8 order_item_origins) keeps working
-- exactly as before. Capacity columns are NOT here: the capacity unit
-- (item-count vs volumetric m³) is pending the Tapin-dimensions addendum
-- (user decision Oct 28 ~11:58 Tehran) — they arrive as 202610280003.

-- (1) Supplier-owned warehouses (§2.2): one supplier, many warehouses.
CREATE TABLE IF NOT EXISTS supplier_warehouses (
  id TEXT PRIMARY KEY,                       -- server-minted: 'swh-' + UUID
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
  name TEXT NOT NULL,                        -- e.g. «کارخانه کرج» / «انبار مشهد»
  province_name TEXT NOT NULL,
  city_name TEXT NOT NULL,                   -- origin city; shown on the leg/badge
  address TEXT,                              -- optional; for courier/freight later
  postal_code VARCHAR(10),                   -- optional; exactly 10 digits when given
  phone VARCHAR(20),                         -- optional
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (supplier_id, name)
);
CREATE INDEX IF NOT EXISTS idx_supplier_warehouses_supplier
  ON supplier_warehouses (supplier_id, is_active, created_at);

-- (2) Per-product origin (§3.1): optional reference to one of the
-- product's own supplier's warehouses. Meaningful only for
-- fulfillment_method='supplier_direct'; warehouse-routed products keep
-- resolving origin from attributed stock at allocation time.
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS origin_supplier_warehouse_id TEXT
    REFERENCES supplier_warehouses(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_products_origin_supplier_warehouse
  ON products (origin_supplier_warehouse_id);

-- (3) Persisted origin on legs + item origins (§4.2): the supplier
-- warehouse a leg / allocation part shipped from (NULL = legacy).
ALTER TABLE order_shipping_legs
  ADD COLUMN IF NOT EXISTS supplier_warehouse_id TEXT
    REFERENCES supplier_warehouses(id) ON DELETE SET NULL;
ALTER TABLE order_item_origins
  ADD COLUMN IF NOT EXISTS supplier_warehouse_id TEXT
    REFERENCES supplier_warehouses(id) ON DELETE SET NULL;

-- (4) Inbound destination + dispatch (§5.2): the central warehouse the
-- shipment is headed to + the supplier-declared dispatch time.
ALTER TABLE inbound_shipments
  ADD COLUMN IF NOT EXISTS warehouse_id TEXT REFERENCES warehouses(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS dispatch_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS dispatched_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_inbound_shipments_warehouse
  ON inbound_shipments (warehouse_id, status, dispatch_at);
CREATE INDEX IF NOT EXISTS idx_inbound_shipments_supplier_status
  ON inbound_shipments (supplier_id, status, created_at DESC);

-- (5) New lifecycle states (§5.3): 'scheduled' (created, awaiting
-- dispatch) and 'cancelled'. Pattern from 000008: drop + re-add the
-- CHECK with the full list. Old rows keep their statuses.
ALTER TABLE inbound_shipments DROP CONSTRAINT IF EXISTS inbound_shipments_status_check;
ALTER TABLE inbound_shipments ADD CONSTRAINT inbound_shipments_status_check
  CHECK (status IN ('scheduled', 'in_transit', 'pending', 'receiving', 'received',
                    'putaway_completed', 'qc_rejected', 'completed', 'cancelled'));
