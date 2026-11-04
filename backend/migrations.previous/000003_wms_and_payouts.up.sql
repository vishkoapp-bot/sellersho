-- Drup Platform - WMS, Manifests and Payout Requests Migration (000003)

-- 1. Inbound Shipments (Supplier Delivery to Warehouse)
CREATE TABLE IF NOT EXISTS inbound_shipments (
    id VARCHAR(50) PRIMARY KEY,
    supplier_id VARCHAR(50) REFERENCES suppliers(id),
    supplier_name VARCHAR(100) NOT NULL,
    warehouse_name VARCHAR(100) NOT NULL DEFAULT 'انبار مرکزی تهران',
    items_count INT NOT NULL,
    cartons_count INT NOT NULL,
    carrier VARCHAR(100),
    truck_plate VARCHAR(50),
    status VARCHAR(30) NOT NULL DEFAULT 'in_transit' CHECK (status IN ('in_transit', 'received', 'putaway_completed', 'qc_rejected')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    received_at TIMESTAMPTZ
);

-- 2. Warehouse Bin Locations (Putaway Shelving)
CREATE TABLE IF NOT EXISTS warehouse_bins (
    bin_code VARCHAR(30) PRIMARY KEY, -- e.g. 'B-12-03'
    hall_name VARCHAR(50) NOT NULL,
    aisle VARCHAR(20) NOT NULL,
    shelf VARCHAR(20) NOT NULL,
    capacity_percent INT NOT NULL DEFAULT 0,
    product_id VARCHAR(50) REFERENCES products(id),
    quantity INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Shipping Manifests (Dispatch Batches to Post / Tipax)
CREATE TABLE IF NOT EXISTS shipping_manifests (
    id VARCHAR(50) PRIMARY KEY, -- e.g. 'MAN-501'
    carrier VARCHAR(100) NOT NULL,
    driver_name VARCHAR(100) NOT NULL,
    packages_count INT NOT NULL DEFAULT 0,
    status VARCHAR(30) NOT NULL DEFAULT 'pending_pickup' CHECK (status IN ('pending_pickup', 'handed_over')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    dispatched_at TIMESTAMPTZ
);

-- 4. Payout Requests (Shaba Paya Settlement)
CREATE TABLE IF NOT EXISTS payout_requests (
    id VARCHAR(50) PRIMARY KEY,
    store_id VARCHAR(50) REFERENCES stores(id),
    recipient_type VARCHAR(20) NOT NULL CHECK (recipient_type IN ('seller', 'supplier')),
    recipient_name VARCHAR(100) NOT NULL,
    shaba VARCHAR(30) NOT NULL,
    bank_name VARCHAR(50) NOT NULL,
    amount BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    rejection_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);
