ALTER TABLE products DROP COLUMN IF EXISTS warranty_property_id;
DROP TABLE IF EXISTS warranty_property_requests;
DROP TABLE IF EXISTS warranty_properties;
DROP TABLE IF EXISTS supplier_return_requests;
DROP TABLE IF EXISTS warehouse_capacity_settings;
ALTER TABLE platform_settings DROP COLUMN IF EXISTS supplier_return_postal_charge;