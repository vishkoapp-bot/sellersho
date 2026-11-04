-- Canonical inventory, supplier return operations, carrier quotes, and physical measurements.
-- Legacy products.stock and warehouse_bins.quantity remain for compatibility while
-- unreconciled historical data is migrated through explicit operational movements.

ALTER TABLE products
  ADD COLUMN IF NOT EXISTS length_mm INT CHECK (length_mm IS NULL OR length_mm > 0),
  ADD COLUMN IF NOT EXISTS width_mm INT CHECK (width_mm IS NULL OR width_mm > 0),
  ADD COLUMN IF NOT EXISTS height_mm INT CHECK (height_mm IS NULL OR height_mm > 0),
  ADD COLUMN IF NOT EXISTS weight_grams INT CHECK (weight_grams IS NULL OR weight_grams > 0),
  ADD COLUMN IF NOT EXISTS measurement_status VARCHAR(20) NOT NULL DEFAULT 'unverified'
    CHECK (measurement_status IN ('unverified', 'verified', 'disputed')),
  ADD COLUMN IF NOT EXISTS measurement_version INT NOT NULL DEFAULT 0;

ALTER TABLE warehouse_bins
  ADD COLUMN IF NOT EXISTS warehouse_name VARCHAR(100) DEFAULT 'انبار مرکزی تهران',
  ADD COLUMN IF NOT EXISTS capacity_volume_cm3 BIGINT CHECK (capacity_volume_cm3 IS NULL OR capacity_volume_cm3 > 0),
  ADD COLUMN IF NOT EXISTS max_weight_grams BIGINT CHECK (max_weight_grams IS NULL OR max_weight_grams > 0),
  ADD COLUMN IF NOT EXISTS occupied_volume_cm3 BIGINT NOT NULL DEFAULT 0 CHECK (occupied_volume_cm3 >= 0),
  ADD COLUMN IF NOT EXISTS occupied_weight_grams BIGINT NOT NULL DEFAULT 0 CHECK (occupied_weight_grams >= 0),
  ADD COLUMN IF NOT EXISTS capacity_status VARCHAR(20) NOT NULL DEFAULT 'unverified'
    CHECK (capacity_status IN ('unverified', 'verified', 'disputed'));

CREATE TABLE IF NOT EXISTS inventory_balances (
  id VARCHAR(80) PRIMARY KEY,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE RESTRICT,
  owner_type VARCHAR(20) NOT NULL CHECK (owner_type IN ('supplier', 'platform', 'customer_return', 'unreconciled')),
  location_type VARCHAR(20) NOT NULL CHECK (location_type IN ('supplier', 'warehouse', 'bin', 'in_transit', 'unknown')),
  location_key VARCHAR(150) NOT NULL,
  warehouse_name VARCHAR(100),
  bin_code VARCHAR(30) REFERENCES warehouse_bins(bin_code) ON DELETE RESTRICT,
  status VARCHAR(30) NOT NULL CHECK (status IN ('physical', 'sellable', 'sales_reserved', 'return_requested', 'quarantine', 'in_transit', 'damaged', 'unreconciled')),
  quantity INT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
  reconciled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (product_id, owner_type, location_type, location_key, status)
);

CREATE INDEX IF NOT EXISTS idx_inventory_balances_supplier_product
  ON inventory_balances(supplier_id, product_id, status);
CREATE INDEX IF NOT EXISTS idx_inventory_balances_location
  ON inventory_balances(warehouse_name, bin_code, status);

CREATE TABLE IF NOT EXISTS inventory_movements (
  id VARCHAR(80) PRIMARY KEY,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE RESTRICT,
  quantity INT NOT NULL CHECK (quantity > 0),
  from_status VARCHAR(30),
  to_status VARCHAR(30),
  from_location_key VARCHAR(150),
  to_location_key VARCHAR(150),
  reference_type VARCHAR(40) NOT NULL,
  reference_id VARCHAR(80) NOT NULL,
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  actor_user_id VARCHAR(80),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_reference
  ON inventory_movements(reference_type, reference_id, created_at DESC);

CREATE TABLE IF NOT EXISTS inventory_reservations (
  id VARCHAR(80) PRIMARY KEY,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE RESTRICT,
  location_key VARCHAR(150) NOT NULL,
  quantity INT NOT NULL CHECK (quantity > 0),
  reference_type VARCHAR(40) NOT NULL,
  reference_id VARCHAR(80) NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released', 'consumed')),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  released_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_inventory_reservations_reference
  ON inventory_reservations(reference_type, reference_id, status);

ALTER TABLE supplier_return_requests
  ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(150),
  ADD COLUMN IF NOT EXISTS reviewed_by VARCHAR(80),
  ADD COLUMN IF NOT EXISTS cancelled_by VARCHAR(80),
  ADD COLUMN IF NOT EXISTS handover_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS inspected_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS inspection_status VARCHAR(20) CHECK (inspection_status IS NULL OR inspection_status IN ('pending', 'passed', 'failed', 'partial')),
  ADD COLUMN IF NOT EXISTS accepted_quantity INT NOT NULL DEFAULT 0 CHECK (accepted_quantity >= 0),
  ADD COLUMN IF NOT EXISTS failed_quantity INT NOT NULL DEFAULT 0 CHECK (failed_quantity >= 0),
  ADD COLUMN IF NOT EXISTS inventory_reference_id VARCHAR(80),
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE supplier_return_requests DROP CONSTRAINT IF EXISTS supplier_return_requests_status_check;
ALTER TABLE supplier_return_requests ADD CONSTRAINT supplier_return_requests_status_check CHECK
  (status IN ('awaiting_return_test', 'approved', 'rejected', 'packed', 'pickup_pending', 'postal_pending', 'in_transit', 'received_pending_qc', 'completed', 'qc_failed', 'cancelled'));
CREATE UNIQUE INDEX IF NOT EXISTS uq_supplier_return_idempotency
  ON supplier_return_requests(idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS supplier_return_events (
  id VARCHAR(80) PRIMARY KEY,
  return_request_id VARCHAR(60) NOT NULL REFERENCES supplier_return_requests(id) ON DELETE CASCADE,
  from_status VARCHAR(40),
  to_status VARCHAR(40) NOT NULL,
  reason TEXT,
  actor_user_id VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS supplier_return_parcels (
  id VARCHAR(80) PRIMARY KEY,
  return_request_id VARCHAR(60) NOT NULL REFERENCES supplier_return_requests(id) ON DELETE CASCADE,
  parcel_number INT NOT NULL CHECK (parcel_number > 0),
  carrier_code VARCHAR(50) NOT NULL,
  payment_method VARCHAR(20) NOT NULL CHECK (payment_method IN ('sender_pays', 'receiver_pays')),
  origin VARCHAR(200) NOT NULL,
  destination VARCHAR(200) NOT NULL,
  length_mm INT NOT NULL CHECK (length_mm > 0),
  width_mm INT NOT NULL CHECK (width_mm > 0),
  height_mm INT NOT NULL CHECK (height_mm > 0),
  weight_grams INT NOT NULL CHECK (weight_grams > 0),
  estimated_charge BIGINT,
  estimate_source VARCHAR(20) CHECK (estimate_source IS NULL OR estimate_source IN ('tariff', 'api', 'manual')),
  estimate_reference VARCHAR(150),
  final_charge BIGINT,
  final_charge_source VARCHAR(20) CHECK (final_charge_source IS NULL OR final_charge_source IN ('tariff', 'api', 'manual')),
  final_charge_reference VARCHAR(150),
  status VARCHAR(25) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed', 'handed_over', 'in_transit', 'received', 'cancelled')),
  tracking_number VARCHAR(150),
  notes TEXT,
  created_by VARCHAR(80) NOT NULL,
  confirmed_by VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  handed_over_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (return_request_id, parcel_number)
);

CREATE TABLE IF NOT EXISTS supplier_return_parcel_items (
  parcel_id VARCHAR(80) NOT NULL REFERENCES supplier_return_parcels(id) ON DELETE CASCADE,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  quantity INT NOT NULL CHECK (quantity > 0),
  PRIMARY KEY (parcel_id, product_id)
);

CREATE TABLE IF NOT EXISTS supplier_return_financial_events (
  id VARCHAR(80) PRIMARY KEY,
  return_request_id VARCHAR(60) NOT NULL REFERENCES supplier_return_requests(id) ON DELETE CASCADE,
  parcel_id VARCHAR(80) REFERENCES supplier_return_parcels(id) ON DELETE RESTRICT,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
  operation VARCHAR(20) NOT NULL CHECK (operation IN ('debit', 'reversal', 'refund')),
  amount BIGINT NOT NULL CHECK (amount > 0),
  payment_method VARCHAR(20) NOT NULL,
  ledger_entry_id VARCHAR(80),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  reason TEXT NOT NULL,
  actor_user_id VARCHAR(80) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS carrier_configs (
  code VARCHAR(50) PRIMARY KEY,
  display_name VARCHAR(120) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  supports_sender_pays BOOLEAN NOT NULL DEFAULT FALSE,
  supports_receiver_pays BOOLEAN NOT NULL DEFAULT FALSE,
  supports_quote_api BOOLEAN NOT NULL DEFAULT FALSE,
  supports_booking_api BOOLEAN NOT NULL DEFAULT FALSE,
  tariff_version INT NOT NULL DEFAULT 0,
  updated_by VARCHAR(80),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS carrier_tariffs (
  id VARCHAR(80) PRIMARY KEY,
  carrier_code VARCHAR(50) NOT NULL REFERENCES carrier_configs(code) ON DELETE CASCADE,
  version INT NOT NULL,
  origin_region VARCHAR(100) NOT NULL,
  destination_region VARCHAR(100) NOT NULL,
  size_class VARCHAR(40) NOT NULL,
  base_weight_grams INT NOT NULL CHECK (base_weight_grams >= 0),
  base_charge BIGINT NOT NULL CHECK (base_charge >= 0),
  extra_kg_charge BIGINT NOT NULL DEFAULT 0 CHECK (extra_kg_charge >= 0),
  extra_rules JSONB NOT NULL DEFAULT '{}'::jsonb,
  valid_from TIMESTAMPTZ NOT NULL,
  valid_until TIMESTAMPTZ,
  created_by VARCHAR(80) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (carrier_code, version, origin_region, destination_region, size_class)
);

CREATE TABLE IF NOT EXISTS product_measurement_events (
  id VARCHAR(80) PRIMARY KEY,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  old_length_mm INT,
  old_width_mm INT,
  old_height_mm INT,
  old_weight_grams INT,
  new_length_mm INT NOT NULL CHECK (new_length_mm > 0),
  new_width_mm INT NOT NULL CHECK (new_width_mm > 0),
  new_height_mm INT NOT NULL CHECK (new_height_mm > 0),
  new_weight_grams INT NOT NULL CHECK (new_weight_grams > 0),
  reason TEXT NOT NULL,
  actor_user_id VARCHAR(80) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS putaway_capacity_reservations (
  id VARCHAR(80) PRIMARY KEY,
  task_id VARCHAR(50) NOT NULL REFERENCES putaway_tasks(id) ON DELETE CASCADE,
  bin_code VARCHAR(30) NOT NULL REFERENCES warehouse_bins(bin_code) ON DELETE RESTRICT,
  volume_cm3 BIGINT NOT NULL CHECK (volume_cm3 > 0),
  weight_grams BIGINT NOT NULL CHECK (weight_grams > 0),
  status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'consumed', 'released')),
  idempotency_key VARCHAR(150) NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  released_at TIMESTAMPTZ,
  UNIQUE (task_id)
);

INSERT INTO carrier_configs (code, display_name, supports_sender_pays, supports_receiver_pays, supports_quote_api)
VALUES
  ('tipax', 'تیپاکس', TRUE, FALSE, FALSE),
  ('post', 'شرکت ملی پست', TRUE, FALSE, FALSE)
ON CONFLICT (code) DO NOTHING;