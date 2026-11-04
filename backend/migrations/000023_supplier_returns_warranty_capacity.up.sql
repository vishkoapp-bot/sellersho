-- Supplier inventory returns, global warranty properties, and manager-controlled storage settings.

ALTER TABLE platform_settings
  ADD COLUMN IF NOT EXISTS supplier_return_postal_charge BIGINT;

CREATE TABLE IF NOT EXISTS supplier_return_requests (
  id VARCHAR(60) PRIMARY KEY,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
  warehouse_bin_code VARCHAR(30) REFERENCES warehouse_bins(bin_code) ON DELETE SET NULL,
  requested_quantity INT NOT NULL CHECK (requested_quantity > 0),
  collection_method VARCHAR(30) NOT NULL CHECK (collection_method IN ('supplier_collect', 'postal')),
  postal_charge BIGINT NOT NULL DEFAULT 0 CHECK (postal_charge >= 0),
  status VARCHAR(40) NOT NULL DEFAULT 'awaiting_return_test' CHECK (status IN ('awaiting_return_test', 'approved', 'rejected', 'pickup_pending', 'postal_pending', 'completed', 'cancelled')),
  reason TEXT NOT NULL,
  rejection_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  reviewed_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  UNIQUE (supplier_id, product_id, id)
);

CREATE INDEX IF NOT EXISTS idx_supplier_returns_scope ON supplier_return_requests(supplier_id, product_id, status);

CREATE TABLE IF NOT EXISTS warranty_properties (
  id VARCHAR(60) PRIMARY KEY,
  name VARCHAR(200) NOT NULL,
  description TEXT NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
  created_by VARCHAR(50),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (name)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_warranty_properties_name ON warranty_properties (lower(trim(name)));

ALTER TABLE pending_approvals DROP CONSTRAINT IF EXISTS pending_approvals_entity_type_check;
ALTER TABLE pending_approvals ADD CONSTRAINT pending_approvals_entity_type_check CHECK (entity_type IN ('store_name', 'store_slug', 'custom_domain', 'proposed_product', 'payout', 'warranty_property'));

CREATE TABLE IF NOT EXISTS warranty_property_requests (
  id VARCHAR(60) PRIMARY KEY,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
  product_id VARCHAR(50) REFERENCES products(id) ON DELETE SET NULL,
  proposed_name VARCHAR(200) NOT NULL,
  description TEXT NOT NULL CHECK (length(trim(description)) >= 30),
  image_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
  status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
  rejection_reason TEXT,
  approved_property_id VARCHAR(60) REFERENCES warranty_properties(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  reviewed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_pending_warranty_name ON warranty_property_requests(lower(trim(proposed_name))) WHERE status = 'pending';

ALTER TABLE products
  ADD COLUMN IF NOT EXISTS warranty_property_id VARCHAR(60) REFERENCES warranty_properties(id);

CREATE TABLE IF NOT EXISTS warehouse_capacity_settings (
  warehouse_name VARCHAR(100) PRIMARY KEY,
  capacity_units INT NOT NULL CHECK (capacity_units > 0),
  updated_by VARCHAR(50),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO warehouse_capacity_settings (warehouse_name, capacity_units)
VALUES ('انبار مرکزی تهران', 10000)
ON CONFLICT (warehouse_name) DO NOTHING;