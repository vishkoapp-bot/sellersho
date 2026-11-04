-- 202610170004_product_is_active: customer-facing product visibility flag (H7).
--
-- The public catalog (GET /api/v1/products and /api/v1/products/{id}) had no
-- way to hide a product without deleting its row. E2E/test leftovers such as
-- prod-1792194550518 ("هدفون تست E2E") were therefore publicly listed with
-- broken images. This adds a visibility flag instead:
--   - public list/detail only return is_active = TRUE products,
--   - order creation rejects inactive products,
--   - internal endpoints (admin, supplier, warehouse) keep seeing all rows.
-- Existing rows default to TRUE: no currently listed product disappears.

ALTER TABLE products ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_products_is_active ON products (is_active);
