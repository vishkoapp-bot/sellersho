-- Drup Platform - Initial Schema Migration (PostgreSQL 16)
-- Part 1: Users, Suppliers, Categories, Niches, Stores, Products

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. Users & Authentication
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    mobile VARCHAR(15) UNIQUE NOT NULL,
    full_name VARCHAR(100) NOT NULL,
    role VARCHAR(30) NOT NULL DEFAULT 'customer' CHECK (role IN ('customer', 'seller', 'supplier', 'warehouse_staff', 'admin', 'finance_specialist', 'support_specialist', 'logistics_specialist')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Suppliers
CREATE TABLE IF NOT EXISTS suppliers (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    logo_url TEXT,
    rating NUMERIC(3,2) DEFAULT 5.00,
    total_products INT DEFAULT 0,
    delivery_score INT DEFAULT 100,
    warehouse_city VARCHAR(50) NOT NULL DEFAULT 'تهران',
    description TEXT,
    badge VARCHAR(50),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Categories & Niches
CREATE TABLE IF NOT EXISTS categories (
    id VARCHAR(50) PRIMARY KEY,
    title VARCHAR(100) NOT NULL,
    slug VARCHAR(100) UNIQUE NOT NULL,
    icon_name VARCHAR(50) NOT NULL,
    gradient VARCHAR(100),
    image_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS niches (
    id VARCHAR(50) PRIMARY KEY,
    title VARCHAR(100) NOT NULL,
    slug VARCHAR(100) UNIQUE NOT NULL,
    category_slug VARCHAR(100) REFERENCES categories(slug) ON DELETE CASCADE,
    badge VARCHAR(50),
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 4. Stores (Dropshippers & Central)
CREATE TABLE IF NOT EXISTS stores (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) UNIQUE NOT NULL,
    owner_type VARCHAR(20) NOT NULL DEFAULT 'dropshipper' CHECK (owner_type IN ('central', 'dropshipper')),
    owner_name VARCHAR(100) NOT NULL,
    owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    tagline TEXT,
    description TEXT,
    profit_margin_percent NUMERIC(5,2) NOT NULL DEFAULT 12.00,
    custom_store_margin_override NUMERIC(5,2),
    is_margin_locked_by_admin BOOLEAN NOT NULL DEFAULT FALSE,
    active_plan_id VARCHAR(20) NOT NULL DEFAULT 'trial' CHECK (active_plan_id IN ('trial', 'standard', 'pro', 'vip')),
    plan_expires_at TIMESTAMPTZ,
    referral_code VARCHAR(30) UNIQUE,
    referrer_seller_id VARCHAR(50) REFERENCES stores(id) ON DELETE SET NULL,
    signed_contract_version INT NOT NULL DEFAULT 1,
    custom_domain VARCHAR(150),
    total_sales BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 5. Products (Reference Catalog)
CREATE TABLE IF NOT EXISTS products (
    id VARCHAR(50) PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    category_slug VARCHAR(100) REFERENCES categories(slug),
    supplier_id VARCHAR(50) REFERENCES suppliers(id),
    original_price BIGINT NOT NULL,           -- Anchor crossed-out price in Tomans
    supplier_base_cost BIGINT NOT NULL,       -- Wholesale cost (C) in Tomans
    platform_margin BIGINT NOT NULL,          -- Platform default fee (F) in Tomans
    is_original BOOLEAN NOT NULL DEFAULT TRUE, -- False = High-Copy
    stock INT NOT NULL DEFAULT 0,
    image_url TEXT,
    specs JSONB DEFAULT '{}'::jsonb,
    description TEXT,
    warehouse_name VARCHAR(100) DEFAULT 'انبار مرکزی تهران',
    preparation_days INT DEFAULT 1,
    supplier_warranty VARCHAR(100),
    is_featured BOOLEAN DEFAULT FALSE,
    is_special_deal BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS product_niches (
    product_id VARCHAR(50) REFERENCES products(id) ON DELETE CASCADE,
    niche_id VARCHAR(50) REFERENCES niches(id) ON DELETE CASCADE,
    PRIMARY KEY (product_id, niche_id)
);

CREATE TABLE IF NOT EXISTS store_product_overrides (
    store_id VARCHAR(50) REFERENCES stores(id) ON DELETE CASCADE,
    product_id VARCHAR(50) REFERENCES products(id) ON DELETE CASCADE,
    override_value NUMERIC(12,2) NOT NULL,
    mode VARCHAR(10) NOT NULL DEFAULT 'fixed' CHECK (mode IN ('percent', 'fixed')),
    PRIMARY KEY (store_id, product_id)
);

CREATE TABLE IF NOT EXISTS store_niche_overrides (
    store_id VARCHAR(50) REFERENCES stores(id) ON DELETE CASCADE,
    niche_id VARCHAR(50) REFERENCES niches(id) ON DELETE CASCADE,
    override_value NUMERIC(12,2) NOT NULL,
    mode VARCHAR(10) NOT NULL DEFAULT 'percent' CHECK (mode IN ('percent', 'fixed')),
    PRIMARY KEY (store_id, niche_id)
);

-- 6. Orders & Items
CREATE TABLE IF NOT EXISTS orders (
    id VARCHAR(50) PRIMARY KEY,
    order_number VARCHAR(30) UNIQUE NOT NULL,
    store_id VARCHAR(50) REFERENCES stores(id),
    store_name VARCHAR(100) NOT NULL,
    total_amount BIGINT NOT NULL,
    discount_amount BIGINT NOT NULL DEFAULT 0,
    payable_amount BIGINT NOT NULL,
    payment_method VARCHAR(20) NOT NULL DEFAULT 'cash' CHECK (payment_method IN ('cash', 'installments')),
    customer_full_name VARCHAR(100) NOT NULL,
    customer_phone VARCHAR(20) NOT NULL,
    customer_province VARCHAR(50) NOT NULL,
    customer_city VARCHAR(50) NOT NULL,
    customer_address TEXT NOT NULL,
    customer_postal_code VARCHAR(20) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'processing' CHECK (status IN (
        'pending', 'processing', 'shipped', 'delivered_unconfirmed',
        'delivered_confirmed', 'delivered', 'delivery_disputed', 'cancelled'
    )),
    tracking_code VARCHAR(50) UNIQUE NOT NULL,
    shipping_cost BIGINT NOT NULL DEFAULT 0,
    dropshipper_profit BIGINT NOT NULL DEFAULT 0,
    platform_profit BIGINT NOT NULL DEFAULT 0,
    referral_commission BIGINT DEFAULT 0,
    referral_commission_released BOOLEAN DEFAULT FALSE,
    referrer_seller_id VARCHAR(50) REFERENCES stores(id),
    profit_released BOOLEAN DEFAULT FALSE,
    profit_released_at TIMESTAMPTZ,
    shipped_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    disputed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
    product_id VARCHAR(50) REFERENCES products(id),
    quantity INT NOT NULL DEFAULT 1,
    wholesale_cost BIGINT NOT NULL,
    platform_fee BIGINT NOT NULL,
    seller_margin BIGINT NOT NULL,
    final_unit_price BIGINT NOT NULL
);

-- 7. Disputes & Arbitration
CREATE TABLE IF NOT EXISTS disputes (
    id VARCHAR(50) PRIMARY KEY,
    order_id VARCHAR(50) REFERENCES orders(id),
    order_number VARCHAR(30) NOT NULL,
    type VARCHAR(30) NOT NULL CHECK (type IN ('return_7day', 'delivery_undelivered')),
    customer_name VARCHAR(100) NOT NULL,
    product_title VARCHAR(255) NOT NULL,
    claim_amount BIGINT NOT NULL,
    reason TEXT NOT NULL,
    store_id VARCHAR(50) REFERENCES stores(id),
    supplier_name VARCHAR(100),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'resolved')),
    responsible_party VARCHAR(100),
    resolution_action VARCHAR(30) CHECK (resolution_action IN ('release_to_seller', 'refund_carrier', 'refund_warehouse', 'refund_supplier', 'refund_seller')),
    resolution_date TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 8. Financial Ledger (Double-Entry, Immutable)
CREATE TABLE IF NOT EXISTS ledger_entries (
    id VARCHAR(50) PRIMARY KEY,
    date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    target_account VARCHAR(150) NOT NULL,
    target_type VARCHAR(20) NOT NULL CHECK (target_type IN ('seller', 'supplier', 'carrier', 'platform')),
    type VARCHAR(10) NOT NULL CHECK (type IN ('debit', 'credit')),
    amount BIGINT NOT NULL,
    reason TEXT NOT NULL,
    operator VARCHAR(100) NOT NULL,
    related_order_or_dispute VARCHAR(50),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 9. Centralized Approvals Inbox
CREATE TABLE IF NOT EXISTS pending_approvals (
    id VARCHAR(50) PRIMARY KEY,
    entity_type VARCHAR(30) NOT NULL CHECK (entity_type IN ('store_name', 'store_slug', 'custom_domain', 'proposed_product', 'payout')),
    target_id VARCHAR(50) NOT NULL,
    title VARCHAR(200) NOT NULL,
    requested_data JSONB NOT NULL,
    requested_by VARCHAR(100) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    rejection_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at TIMESTAMPTZ
);

-- 10. Platform Settings
CREATE TABLE IF NOT EXISTS platform_settings (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    base_fee_f BIGINT NOT NULL DEFAULT 100000,
    min_profit_margin NUMERIC(5,2) NOT NULL DEFAULT 5.00,
    max_profit_margin NUMERIC(5,2) NOT NULL DEFAULT 35.00,
    default_margin NUMERIC(5,2) NOT NULL DEFAULT 20.00,
    attribution_model VARCHAR(20) NOT NULL DEFAULT 'last_touch',
    cookie_days INT NOT NULL DEFAULT 30,
    delivery_hold_period_days INT NOT NULL DEFAULT 20,
    delivery_followup_trigger_days INT NOT NULL DEFAULT 10,
    referral_commission_percent NUMERIC(5,2) NOT NULL DEFAULT 2.00,
    referral_is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    referral_subject_to_delivery_lock BOOLEAN NOT NULL DEFAULT TRUE,
    seller_contract_version INT NOT NULL DEFAULT 2,
    seller_contract_text TEXT NOT NULL DEFAULT '',
    supplier_contract_version INT NOT NULL DEFAULT 2,
    supplier_contract_text TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
