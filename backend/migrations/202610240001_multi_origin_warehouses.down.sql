-- 202610240001 rollback: multi-origin shipping data structures only.
-- Ledger rows (inventory_balances) survive; only the attribution column
-- and the new tables are removed. Orders/legs created while the feature
-- was live keep their money totals (orders.shipping_cost is untouched);
-- their per-item origin detail is dropped with order_item_origins.
DROP TABLE IF EXISTS order_item_origins;
ALTER TABLE order_shipping_legs DROP COLUMN IF EXISTS warehouse_id;
ALTER TABLE inventory_balances DROP COLUMN IF EXISTS warehouse_id;
DROP TABLE IF EXISTS warehouses;
