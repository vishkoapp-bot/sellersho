-- 202610170006_warehouse_staff_city: warehouse_city on users for staff created by super-admin.
--
-- Warehouse staff are provisioned exclusively by the super-admin via
-- POST /api/v1/admin/staff. Their assigned warehouse city is stored on the
-- users row (previously only suppliers had warehouse_city). The warehouse
-- login guard reads it from GET /api/v1/warehouse/me instead of using
-- hardcoded fake station data.

ALTER TABLE users ADD COLUMN IF NOT EXISTS warehouse_city VARCHAR(50) NOT NULL DEFAULT 'تهران';
