-- Down for 202610280004: drop the snapshot index and the documenting comment.
DROP INDEX IF EXISTS idx_inv_bal_supplier_sellable;
COMMENT ON COLUMN products.origin_supplier_warehouse_id IS NULL;
