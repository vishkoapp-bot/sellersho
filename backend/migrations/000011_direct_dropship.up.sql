-- Drup Platform - Direct Dropshipping & Dual Fulfillment System (000011)

-- 1. Add fulfillment_method to products
ALTER TABLE products 
ADD COLUMN IF NOT EXISTS fulfillment_method VARCHAR(20) DEFAULT 'warehouse' CHECK (fulfillment_method IN ('warehouse', 'supplier_direct'));

CREATE INDEX IF NOT EXISTS idx_products_fulfillment ON products(fulfillment_method);

COMMENT ON COLUMN products.fulfillment_method IS 'warehouse: از انبار پلتفرم ارسال می‌شود | supplier_direct: مستقیماً از Supplier ارسال می‌شود';

-- 2. Add supports_direct_shipping to suppliers
ALTER TABLE suppliers 
ADD COLUMN IF NOT EXISTS supports_direct_shipping BOOLEAN DEFAULT FALSE;

COMMENT ON COLUMN suppliers.supports_direct_shipping IS 'آیا این Supplier می‌تواند مستقیماً برای مشتریان ارسال کند؟';

-- 3. Add fulfillment_method to orders
ALTER TABLE orders 
ADD COLUMN IF NOT EXISTS fulfillment_method VARCHAR(20) DEFAULT 'warehouse' CHECK (fulfillment_method IN ('warehouse', 'supplier_direct', 'mixed'));

COMMENT ON COLUMN orders.fulfillment_method IS 'نحوه تأمین این سفارش: warehouse | supplier_direct | mixed';

-- 4. Create supplier_shipments table
CREATE TABLE IF NOT EXISTS supplier_shipments (
    id VARCHAR(255) PRIMARY KEY,
    order_id VARCHAR(255) NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    supplier_id VARCHAR(255) NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    
    -- Customer info for direct fulfillment
    shipping_address JSONB NOT NULL,
    customer_phone VARCHAR(20) NOT NULL,
    customer_name VARCHAR(255) NOT NULL,
    
    -- Carrier & dispatch data
    carrier VARCHAR(100),
    tracking_number VARCHAR(255),
    shipped_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    
    -- Status lifecycle: pending -> shipped -> in_transit -> delivered / failed
    status VARCHAR(50) DEFAULT 'pending',
    supplier_notes TEXT,
    
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_supplier_shipments_order ON supplier_shipments(order_id);
CREATE INDEX IF NOT EXISTS idx_supplier_shipments_supplier ON supplier_shipments(supplier_id);
CREATE INDEX IF NOT EXISTS idx_supplier_shipments_status ON supplier_shipments(status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_shipments_tracking ON supplier_shipments(tracking_number) WHERE tracking_number IS NOT NULL;

-- 5. Audit Logging for privacy and compliance
CREATE TABLE IF NOT EXISTS supplier_shipment_audit_logs (
    id VARCHAR(255) PRIMARY KEY,
    shipment_id VARCHAR(255) REFERENCES supplier_shipments(id) ON DELETE CASCADE,
    actor_user_id VARCHAR(255),
    action VARCHAR(100) NOT NULL,
    timestamp TIMESTAMPTZ DEFAULT NOW()
);

-- 6. Seed dedicated supplier user: supplier_ali (password: supplier123)
INSERT INTO users (id, mobile, full_name, role, username, password_hash, is_active, created_at, updated_at)
VALUES 
    ('b0000000-0000-0000-0000-000000000099', '09129990099', 'علی تأمین‌کننده (دایرکت دراپ‌شیپ)', 'supplier', 'supplier_ali', '$2a$10$5beGcbI84ITaLLG73XRBROptg/4.o7N1S795NU7Ljs/0dT0Gi546C', TRUE, NOW(), NOW())
ON CONFLICT (mobile) DO UPDATE SET
    username = EXCLUDED.username,
    password_hash = EXCLUDED.password_hash,
    role = EXCLUDED.role,
    is_active = TRUE,
    updated_at = NOW();

-- 7. Ensure supplier profile linked to supplier_ali with supports_direct_shipping = TRUE
INSERT INTO suppliers (id, owner_user_id, name, logo_url, rating, total_products, delivery_score, warehouse_city, description, badge, supports_direct_shipping)
VALUES
    ('sup-ali', 'b0000000-0000-0000-0000-000000000099', 'تأمین الکترونیک علی', 'https://web-cdn.snapp.ir/fintech/merchant-growth/img/technolife.png', 4.90, 25, 98, 'تهران', 'تأمین‌کننده مستقیم و بدون واسطه', 'ارسال مستقیم', TRUE)
ON CONFLICT (id) DO UPDATE SET
    owner_user_id = EXCLUDED.owner_user_id,
    supports_direct_shipping = TRUE;

-- Also update sup-technolife to support direct shipping if configured
UPDATE suppliers SET supports_direct_shipping = TRUE WHERE id = 'sup-technolife';
