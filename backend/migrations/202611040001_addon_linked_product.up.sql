-- 202611040001: live addons widget (Ql) fields: a product-mode add-on links
-- to one of the supplier's own products (title/price come from the product),
-- and a rejection reason is shown when a manager rejects an add-on.
ALTER TABLE product_addons ADD COLUMN IF NOT EXISTS linked_product_id VARCHAR(50) REFERENCES products(id) ON DELETE SET NULL;
ALTER TABLE product_addons ADD COLUMN IF NOT EXISTS rejection_reason TEXT;
