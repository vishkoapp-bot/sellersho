-- Drup Platform - Revert Direct Dropshipping Migration (000011)

DROP TABLE IF EXISTS supplier_shipment_audit_logs;
DROP TABLE IF EXISTS supplier_shipments;
ALTER TABLE orders DROP COLUMN IF EXISTS fulfillment_method;
ALTER TABLE suppliers DROP COLUMN IF EXISTS supports_direct_shipping;
DROP INDEX IF EXISTS idx_products_fulfillment;
ALTER TABLE products DROP COLUMN IF EXISTS fulfillment_method;
