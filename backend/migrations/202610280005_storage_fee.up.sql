-- Storage fee (rework v2, 2026-10-28) — design-rework-origin-dynamic.md §5.5.
-- Scope: CENTRAL warehouses of the platform only. All amounts are integer
-- Tomans. A position is one deposited batch of one product, one owner, one
-- central warehouse; one charge row per position per chargeable calendar day
-- (Asia/Tehran); UNIQUE(position_id, fee_date) is the idempotency anchor.
CREATE TABLE IF NOT EXISTS warehouse_storage_positions (
  id TEXT PRIMARY KEY,                       -- 'WSP-' + UUID
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
  warehouse_id TEXT NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
  inbound_id TEXT REFERENCES inbound_shipments(id) ON DELETE SET NULL,  -- NULL = admin set / other stocking
  goods_class VARCHAR(20) NOT NULL DEFAULT 'normal' CHECK (goods_class IN ('normal','returned')),
  qty INT NOT NULL CHECK (qty >= 0),
  stocked_at TIMESTAMPTZ NOT NULL,          -- start of the storage clock (receipt/stocking time)
  original_stocked_at TIMESTAMPTZ NOT NULL, -- survives inter-warehouse transfer: the free period never resets (§5.7)
  last_fee_date DATE,                        -- last charged calendar day (checkpoint)
  total_fee_charged_toman BIGINT NOT NULL DEFAULT 0,
  closed_at TIMESTAMPTZ,                    -- NULL = open; set when qty reaches 0
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wsp_open ON warehouse_storage_positions (warehouse_id, closed_at) WHERE closed_at IS NULL;

CREATE TABLE IF NOT EXISTS warehouse_storage_fee_charges (
  id TEXT PRIMARY KEY,                       -- 'WSF-' + UUID
  position_id TEXT NOT NULL REFERENCES warehouse_storage_positions(id) ON DELETE RESTRICT,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  warehouse_id TEXT NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
  fee_date DATE NOT NULL,                    -- chargeable calendar day (Asia/Tehran)
  qty INT NOT NULL,
  fee_unit_day_toman BIGINT NOT NULL,        -- Tomans per unit for that day
  amount_toman BIGINT NOT NULL CHECK (amount_toman > 0),
  goods_class VARCHAR(20) NOT NULL,         -- effective class that day (normal/stagnant/returned)
  estimated BOOLEAN NOT NULL DEFAULT FALSE,  -- weight/dimensions were estimated
  journal_debit_id TEXT,
  journal_credit_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (position_id, fee_date)             -- idempotency: one charge per position per day
);
CREATE INDEX IF NOT EXISTS idx_wsfc_supplier ON warehouse_storage_fee_charges (supplier_id, fee_date DESC);
