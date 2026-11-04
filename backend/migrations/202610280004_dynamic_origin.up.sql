-- Dynamic origin (rework v2, 2026-10-28) — design-rework-origin-dynamic.md §1.6.
-- No new column on products: origin_supplier_warehouse_id keeps its v1
-- meaning, narrowed by documentation to "declared DEFAULT origin for
-- direct stock with no location record" (§1.5, option b). The live
-- origin is resolved from where the fulfilling stock sits NOW
-- (inventory_balances), not from this column.
COMMENT ON COLUMN products.origin_supplier_warehouse_id IS
  'Declared DEFAULT origin for supplier-direct stock with no location record (rework v2 §1.5, option b). NOT the product origin: the live origin is resolved dynamically from inventory_balances at quote/order time (§1.2). NULL = no declared default; the profile-city / generic-label fallback applies.';

-- Index for the supplier-stock branch of the new StockLocationsForProducts
-- snapshot (sellable rows in the per-supplier bucket
-- location_key='supplier:<id>'). The central branch reuses the existing
-- query shape of WarehouseStockForProducts (no new index: same predicate
-- the v1 snapshot already ran).
CREATE INDEX IF NOT EXISTS idx_inv_bal_supplier_sellable
  ON inventory_balances (product_id, status) WHERE location_type = 'supplier';
