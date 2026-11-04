-- Drup Platform - Warehouse Staff, Admin, and Finance Lifecycle Schema (000008)

-- 1. Relax role constraint to support both 'finance' and 'finance_specialist', 'warehouse_staff', 'admin'
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check 
    CHECK (role IN ('customer', 'seller', 'supplier', 'admin', 'warehouse_staff', 'logistics_specialist', 'finance_specialist', 'finance', 'support_specialist'));

-- 2. Seed Dedicated Staff Accounts for E2E Tests
INSERT INTO users (id, mobile, full_name, role, username, password_hash, is_active, created_at, updated_at)
VALUES 
    ('a0000000-0000-0000-0000-000000000077', '09000000071', 'علی کارگر (انباردار)', 'warehouse_staff', 'warehouse_ali', '$2a$10$boGlIBfo9lgFx7cwl0xKs.wnyHJe3St98d2feLRKC1bmYbFrFIfWK', TRUE, NOW(), NOW()),
    ('a0000000-0000-0000-0000-000000000011', '09000000011', 'رضا مدیری (مدیر سیستم)', 'admin', 'admin_reza', '$2a$10$p6zuh4wx1l.S1.z3aScOaexWy1nQaaXIA8vbdH5HUA5Ilp1.Gx/YO', TRUE, NOW(), NOW()),
    ('a0000000-0000-0000-0000-000000000021', '09000000021', 'سارا حسابدار (مدیر مالی)', 'finance', 'finance_sara', '$2a$10$bLIHIq/7H8LQ0qs6csDO9un3Yyw/6eA3CJGO6vQooXXDrRmh0MNZi', TRUE, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000001', '09000000001', 'مدیر کل سیستم', 'admin', 'admin', '$2a$10$p6zuh4wx1l.S1.z3aScOaexWy1nQaaXIA8vbdH5HUA5Ilp1.Gx/YO', TRUE, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000002', '09000000002', 'کارشناس مالی و پایا', 'finance_specialist', 'finance', '$2a$10$bLIHIq/7H8LQ0qs6csDO9un3Yyw/6eA3CJGO6vQooXXDrRmh0MNZi', TRUE, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000007', '09000000007', 'پرسنل انبار مرکزی', 'warehouse_staff', 'warehouse', '$2a$10$boGlIBfo9lgFx7cwl0xKs.wnyHJe3St98d2feLRKC1bmYbFrFIfWK', TRUE, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000008', '09121112233', 'فروشگاه بی‌بی', 'seller', 'seller_bibi', '$2a$10$p6zuh4wx1l.S1.z3aScOaexWy1nQaaXIA8vbdH5HUA5Ilp1.Gx/YO', TRUE, NOW(), NOW()),
    ('00000000-0000-0000-0000-000000000009', '09123456789', 'مشتری سامانه', 'customer', 'customer_test', '$2a$10$p6zuh4wx1l.S1.z3aScOaexWy1nQaaXIA8vbdH5HUA5Ilp1.Gx/YO', TRUE, NOW(), NOW())
ON CONFLICT (mobile) DO UPDATE SET
    username = EXCLUDED.username,
    password_hash = EXCLUDED.password_hash,
    role = EXCLUDED.role,
    is_active = TRUE,
    updated_at = NOW();

-- 3. Enhance Inbound Shipments for Warehouse Inbound Receiving
ALTER TABLE inbound_shipments DROP CONSTRAINT IF EXISTS inbound_shipments_status_check;
ALTER TABLE inbound_shipments ADD CONSTRAINT inbound_shipments_status_check 
    CHECK (status IN ('in_transit', 'pending', 'receiving', 'received', 'putaway_completed', 'qc_rejected', 'completed'));

ALTER TABLE inbound_shipments 
    ADD COLUMN IF NOT EXISTS scanned_items JSONB DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS started_receiving_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS inbound_receiving_logs (
    id VARCHAR(50) PRIMARY KEY,
    inbound_id VARCHAR(50) REFERENCES inbound_shipments(id) ON DELETE CASCADE,
    staff_user_id VARCHAR(50),
    action VARCHAR(50) NOT NULL,
    scanned_item JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS qc_results (
    id VARCHAR(50) PRIMARY KEY,
    product_id VARCHAR(50) REFERENCES products(id) ON DELETE CASCADE,
    inbound_id VARCHAR(50),
    inspector_user_id VARCHAR(50),
    status VARCHAR(20) NOT NULL,
    defect_reason TEXT,
    quantity_passed INT DEFAULT 0,
    quantity_failed INT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW()
);


-- Ensure bin_locations or warehouse_bins has A-12-05
INSERT INTO warehouse_bins (bin_code, hall_name, aisle, shelf, capacity_percent, product_id, quantity, updated_at)
VALUES ('A-12-05', 'سالن مرکزی A', '12', '05', 40, 'prod-1', 40, NOW())
ON CONFLICT (bin_code) DO NOTHING;

CREATE TABLE IF NOT EXISTS putaway_tasks (
    id VARCHAR(50) PRIMARY KEY,
    product_id VARCHAR(50) REFERENCES products(id) ON DELETE CASCADE,
    inbound_id VARCHAR(50),
    bin_location VARCHAR(50),
    quantity INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    assigned_to VARCHAR(50),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO putaway_tasks (id, product_id, inbound_id, bin_location, quantity, status)
VALUES ('PUT-INIT-001', 'prod-1', 'INB-TEST-PENDING-01', 'A-12-05', 50, 'pending')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS pick_tasks (
    id VARCHAR(50) PRIMARY KEY,
    order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
    picker_user_id VARCHAR(50),
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    picked_items JSONB NOT NULL DEFAULT '[]'::jsonb,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check 
    CHECK (status IN ('pending', 'processing', 'picking', 'picked', 'packed', 'shipped', 'delivered_unconfirmed', 'delivered_confirmed', 'delivered', 'delivery_disputed', 'cancelled'));

ALTER TABLE orders 
    ADD COLUMN IF NOT EXISTS carrier VARCHAR(100),
    ADD COLUMN IF NOT EXISTS tracking_number VARCHAR(100),
    ADD COLUMN IF NOT EXISTS shipping_label TEXT,
    ADD COLUMN IF NOT EXISTS packed_at TIMESTAMPTZ;

INSERT INTO inbound_shipments (id, supplier_id, supplier_name, warehouse_name, items_count, cartons_count, carrier, truck_plate, status, created_at, items)
VALUES ('INB-TEST-PENDING-01', 'sup-technolife', 'تکنولایف', 'انبار مرکزی تهران', 50, 5, 'پست لجستیک', '۱۲-ایران-۳۴۵', 'pending', NOW(), '[{"productId": "prod-1", "quantity": 50, "barcode": "6260123456789"}]'::jsonb)
ON CONFLICT (id) DO UPDATE SET status = 'pending';

INSERT INTO orders (id, order_number, store_id, store_name, total_amount, payable_amount, customer_full_name, customer_phone, customer_province, customer_city, customer_address, customer_postal_code, tracking_code, status, payment_status, created_at)
VALUES ('ord-ready-pick-001', 'ORD-PICK-2026-001', 'store-bibi', 'بی‌بی استایل', 1500000, 1500000, 'سهراب سپهری', '09123456789', 'تهران', 'تهران', 'تهران، خیابان ولیعصر', '1987654321', 'TRK-PICK-001', 'processing', 'paid', NOW())
ON CONFLICT (id) DO UPDATE SET status = 'processing';

INSERT INTO order_items (order_id, product_id, quantity, wholesale_cost, platform_fee, seller_margin, final_unit_price)
VALUES ('ord-ready-pick-001', 'prod-1', 1, 1200000, 100000, 200000, 1500000)
ON CONFLICT DO NOTHING;

ALTER TABLE platform_settings
    ADD COLUMN IF NOT EXISTS supplier_fee_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE platform_settings
SET base_fee_f = 100000,
    supplier_fee_overrides = '{"sup-technolife": 120000}'::jsonb
WHERE id = 1;

ALTER TABLE stores 
    ADD COLUMN IF NOT EXISTS status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'pending', 'rejected', 'suspended')),
    ADD COLUMN IF NOT EXISTS rejection_reason TEXT;

INSERT INTO stores (id, name, slug, owner_type, owner_name, profit_margin_percent, active_plan_id, total_sales, status, created_at)
VALUES 
    ('store-pending-001', 'فروشگاه در انتظار تایید آریا', 'aria-pending-shop', 'dropshipper', 'آریا راد', 15.00, 'trial', 0, 'pending', NOW()),
    ('store-pending-002', 'فروشگاه ناقص مدارک', 'incomplete-docs-shop', 'dropshipper', 'کامران نادری', 10.00, 'trial', 0, 'pending', NOW())
ON CONFLICT (id) DO NOTHING;

ALTER TABLE payout_requests DROP CONSTRAINT IF EXISTS payout_requests_status_check;
ALTER TABLE payout_requests ADD CONSTRAINT payout_requests_status_check 
    CHECK (status IN ('pending', 'approved', 'rejected', 'processing', 'completed'));

INSERT INTO payout_requests (id, store_id, supplier_id, recipient_type, recipient_name, shaba, bank_name, amount, status, created_at)
VALUES 
    ('PAYOUT-PENDING-001', 'store-bibi', NULL, 'seller', 'بهاره رضوانی', 'IR120170000000123456789012', 'بانک ملی ایران', 2500000, 'pending', NOW()),
    ('PAYOUT-PENDING-002', 'store-sepehr', NULL, 'seller', 'سپهر زمانی', 'IR990180000000987654321098', 'بانک تجارت', 1800000, 'pending', NOW())
ON CONFLICT (id) DO NOTHING;
