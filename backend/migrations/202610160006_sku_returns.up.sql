-- Migration 202610160006: SKU/variant system + customer returns.
--
-- 1. products.spn — product-level serial number ("PC-" + 7 digits).
--    Backfilled for existing products; a BEFORE INSERT trigger auto-assigns it
--    for every future product (covers proposal-approval and direct-create paths).
-- 2. product_variants — per-product variants with unique 8-char SPNC codes.
--    Existing products get one default variant carrying their current stock.
--    A trigger keeps products.stock = SUM(variant stocks) (single inventory truth).
-- 3. shipments / shipment_items — supplier -> warehouse inbound shipments (SKU flow).
-- 4. customer_returns / return_items — customer returns (7-day window, admin
--    approve/reject, warehouse QC receive, wallet refund via balanced ledger).
-- 5. customer_wallets — customer wallet balances credited on return refunds.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ---------------------------------------------------------------- 1. SPN ----
ALTER TABLE products ADD COLUMN IF NOT EXISTS spn VARCHAR(20) UNIQUE;

-- Backfill unique "PC-" + 7 digits for products that lack an SPN.
DO $$
DECLARE
  p RECORD;
  new_spn TEXT;
BEGIN
  FOR p IN SELECT id FROM products WHERE spn IS NULL ORDER BY id LOOP
    LOOP
      new_spn := 'PC-' || lpad((floor(random() * 10000000))::int::text, 7, '0');
      EXIT WHEN NOT EXISTS (SELECT 1 FROM products WHERE spn = new_spn);
    END LOOP;
    UPDATE products SET spn = new_spn WHERE id = p.id;
  END LOOP;
END $$;

-- Auto-assign SPN on every future product insert (fail-safe for all create paths).
CREATE OR REPLACE FUNCTION fn_products_assign_spn() RETURNS trigger AS $$
BEGIN
  IF NEW.spn IS NULL OR NEW.spn = '' THEN
    LOOP
      NEW.spn := 'PC-' || lpad((floor(random() * 10000000))::int::text, 7, '0');
      EXIT WHEN NOT EXISTS (SELECT 1 FROM products WHERE spn = NEW.spn);
    END LOOP;
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_products_assign_spn ON products;
CREATE TRIGGER trg_products_assign_spn
  BEFORE INSERT ON products
  FOR EACH ROW EXECUTE FUNCTION fn_products_assign_spn();

-- ------------------------------------------------------ 2. Variants ----
CREATE TABLE IF NOT EXISTS product_variants (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  spnc VARCHAR(12) NOT NULL UNIQUE,
  variant_name TEXT NOT NULL DEFAULT 'پیش‌فرض',
  attributes JSONB NOT NULL DEFAULT '{}',
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  stock INT NOT NULL DEFAULT 0 CHECK (stock >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS product_variants_product_idx ON product_variants(product_id);

-- One default variant per existing product, carrying the product's current stock
-- (so products.stock stays consistent once the resync trigger below is live).
DO $$
DECLARE
  p RECORD;
  new_spnc TEXT;
BEGIN
  FOR p IN SELECT id, COALESCE(stock, 0) AS stock FROM products ORDER BY id LOOP
    IF NOT EXISTS (SELECT 1 FROM product_variants WHERE product_id = p.id) THEN
      LOOP
        new_spnc := upper(substring(md5(random()::text || clock_timestamp()::text || p.id), 1, 8));
        EXIT WHEN NOT EXISTS (SELECT 1 FROM product_variants WHERE spnc = new_spnc);
      END LOOP;
      INSERT INTO product_variants (product_id, spnc, variant_name, attributes, is_default, stock)
      VALUES (p.id, new_spnc, 'پیش‌فرض', '{}', TRUE, p.stock);
    END IF;
  END LOOP;
END $$;

-- Single inventory truth: products.stock always equals the sum of its variants' stock.
CREATE OR REPLACE FUNCTION fn_variants_resync_product_stock() RETURNS trigger AS $$
DECLARE
  pid VARCHAR(50);
BEGIN
  pid := COALESCE(NEW.product_id, OLD.product_id);
  UPDATE products
  SET stock = COALESCE((SELECT SUM(stock) FROM product_variants WHERE product_id = pid), 0),
      updated_at = NOW()
  WHERE id = pid;
  RETURN COALESCE(NEW, OLD);
END $$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_variants_resync_product_stock ON product_variants;
CREATE TRIGGER trg_variants_resync_product_stock
  AFTER INSERT OR UPDATE OF stock, product_id OR DELETE ON product_variants
  FOR EACH ROW EXECUTE FUNCTION fn_variants_resync_product_stock();

-- ----------------------------------------------------- 3. Shipments ----
CREATE TABLE IF NOT EXISTS shipments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shipment_code VARCHAR(16) NOT NULL UNIQUE,
  supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id),
  status VARCHAR(20) NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft', 'sent', 'received', 'partially_received')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  received_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS shipments_supplier_idx ON shipments(supplier_id, created_at DESC);

CREATE TABLE IF NOT EXISTS shipment_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shipment_id UUID NOT NULL REFERENCES shipments(id) ON DELETE CASCADE,
  variant_id UUID NOT NULL REFERENCES product_variants(id),
  expected_qty INT NOT NULL CHECK (expected_qty > 0),
  received_qty INT NOT NULL DEFAULT 0 CHECK (received_qty >= 0),
  qc_status VARCHAR(20) NOT NULL DEFAULT 'pending'
    CHECK (qc_status IN ('pending', 'passed', 'failed'))
);
CREATE INDEX IF NOT EXISTS shipment_items_shipment_idx ON shipment_items(shipment_id);

-- ------------------------------------------------ 4. Customer returns ----
CREATE TABLE IF NOT EXISTS customer_returns (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id VARCHAR(50) NOT NULL,
  customer_id UUID NOT NULL REFERENCES users(id),
  status VARCHAR(20) NOT NULL DEFAULT 'requested'
    CHECK (status IN ('requested', 'approved', 'rejected', 'received', 'refunded', 'completed')),
  reason TEXT NOT NULL DEFAULT '',
  admin_note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS customer_returns_order_idx ON customer_returns(order_id);
CREATE INDEX IF NOT EXISTS customer_returns_customer_idx ON customer_returns(customer_id, created_at DESC);
CREATE INDEX IF NOT EXISTS customer_returns_status_idx ON customer_returns(status, created_at DESC);

CREATE TABLE IF NOT EXISTS return_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  return_id UUID NOT NULL REFERENCES customer_returns(id) ON DELETE CASCADE,
  order_item_id VARCHAR(50) NOT NULL,
  variant_id UUID REFERENCES product_variants(id),
  qty INT NOT NULL CHECK (qty > 0),
  qc_status VARCHAR(20) NOT NULL DEFAULT 'pending'
    CHECK (qc_status IN ('pending', 'ok', 'defective')),
  received_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS return_items_return_idx ON return_items(return_id);

-- ------------------------------------------------- 5. Customer wallets ----
CREATE TABLE IF NOT EXISTS customer_wallets (
  customer_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
