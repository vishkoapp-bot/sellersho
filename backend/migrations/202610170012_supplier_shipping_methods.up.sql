-- Migration 202610170012: supplier shipping methods engine — admin-controlled
-- shipping-method permissions for suppliers.
--
-- Replaces the decorative suppliers.supports_direct_shipping flag with a
-- real two-method permission engine (suppliers audit C1/C2, 2026-10-17):
--   * Every supplier owns both methods ('direct', 'warehouse'), each with
--     an explicit status: 'disabled' | 'requested' | 'enabled'.
--   * Suppliers can only REQUEST a method (disabled -> requested); only the
--     super-admin can enable or disable it (requested -> enabled/disabled).
--   * The order routing engine (order_handlers.go) treats this table as the
--     authoritative gate and reads products.fulfillment_method only when
--     both methods are enabled for the product's supplier.
--
-- supplier_id is the supplier OWNER's users.id (UUID text) so admin tooling
-- can join this data directly with the users list (audit M5).

CREATE TABLE supplier_shipping_methods (
  supplier_id TEXT NOT NULL,
  method TEXT NOT NULL CHECK (method IN ('direct','warehouse')),
  status TEXT NOT NULL DEFAULT 'disabled' CHECK (status IN ('disabled','requested','enabled')),
  requested_at TIMESTAMPTZ,
  decided_at TIMESTAMPTZ,
  decided_by TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (supplier_id, method)
);
CREATE INDEX idx_ssm_supplier ON supplier_shipping_methods(supplier_id);
CREATE INDEX idx_ssm_status ON supplier_shipping_methods(status) WHERE status='requested';

-- Backfill 1: every supplier keeps the documented default behaviour — all
-- orders route through the central warehouse until an admin enables direct
-- shipping for them.
INSERT INTO supplier_shipping_methods (supplier_id, method, status, decided_at)
SELECT u.id, 'warehouse', 'enabled', NOW()
FROM users u
WHERE u.role = 'supplier'
ON CONFLICT DO NOTHING;

-- Backfill 2: suppliers that already held the (previously decorative)
-- supports_direct_shipping flag keep direct shipping enabled under the new
-- engine, so their effective behaviour does not change.
INSERT INTO supplier_shipping_methods (supplier_id, method, status, decided_at)
SELECT DISTINCT s.owner_user_id, 'direct', 'enabled', NOW()
FROM suppliers s
WHERE COALESCE(s.supports_direct_shipping, FALSE) = TRUE
  AND s.owner_user_id IS NOT NULL
ON CONFLICT DO NOTHING;
