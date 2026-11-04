-- Rollback for migration 202610160006: SKU/variant system + customer returns.

DROP TRIGGER IF EXISTS trg_variants_resync_product_stock ON product_variants;
DROP FUNCTION IF EXISTS fn_variants_resync_product_stock();

DROP TRIGGER IF EXISTS trg_products_assign_spn ON products;
DROP FUNCTION IF EXISTS fn_products_assign_spn();

DROP TABLE IF EXISTS return_items;
DROP TABLE IF EXISTS customer_returns;
DROP TABLE IF EXISTS customer_wallets;
DROP TABLE IF EXISTS shipment_items;
DROP TABLE IF EXISTS shipments;
DROP TABLE IF EXISTS product_variants;

ALTER TABLE products DROP COLUMN IF EXISTS spn;
