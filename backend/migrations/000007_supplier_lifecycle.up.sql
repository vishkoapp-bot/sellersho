-- Drup Platform - Supplier Lifecycle Schema Migration (000007)

CREATE TABLE IF NOT EXISTS supplier_pricing_tiers (
    id VARCHAR(50) PRIMARY KEY,
    supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE CASCADE,
    product_id VARCHAR(50) REFERENCES products(id) ON DELETE CASCADE,
    min_quantity INT NOT NULL,
    max_quantity INT,
    price BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS supplier_campaigns (
    id VARCHAR(50) PRIMARY KEY,
    supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE CASCADE,
    campaign_name VARCHAR(200) NOT NULL,
    product_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    budget BIGINT NOT NULL,
    spent_budget BIGINT NOT NULL DEFAULT 0,
    start_date TIMESTAMPTZ,
    end_date TIMESTAMPTZ,
    target_categories JSONB DEFAULT '[]'::jsonb,
    cpc_bid BIGINT NOT NULL DEFAULT 500,
    impressions INT NOT NULL DEFAULT 0,
    clicks INT NOT NULL DEFAULT 0,
    status VARCHAR(30) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS qc_discrepancies (
    id VARCHAR(50) PRIMARY KEY,
    inbound_id VARCHAR(50),
    supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE CASCADE,
    product_id VARCHAR(50),
    expected_quantity INT NOT NULL,
    received_quantity INT NOT NULL,
    defect_count INT NOT NULL DEFAULT 0,
    reason TEXT NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'open',
    supplier_response TEXT,
    resolution_offer TEXT,
    attachments JSONB DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

ALTER TABLE inbound_shipments
    ADD COLUMN IF NOT EXISTS expected_arrival TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS notes TEXT,
    ADD COLUMN IF NOT EXISTS items JSONB DEFAULT '[]'::jsonb;

ALTER TABLE payout_requests
    ADD COLUMN IF NOT EXISTS supplier_id VARCHAR(50) REFERENCES suppliers(id) ON DELETE SET NULL;

INSERT INTO qc_discrepancies (id, inbound_id, supplier_id, product_id, expected_quantity, received_quantity, defect_count, reason, status)
VALUES ('DISP-701', 'INB-9941', 'sup-technolife', 'prod-1', 50, 48, 2, 'کسری ۲ عدد و بسته‌بندی مخدوش', 'open')
ON CONFLICT (id) DO NOTHING;
