-- 202610220001: per-variant cart quantity limits (min/max units of ONE
-- variant allowed in a single user's cart/order), set by the supplier.
--
-- Why: nothing stopped a single buyer from putting a variant's entire
-- stock into one cart and wiping it out. The supplier now decides, per
-- variant, the minimum (default 1 = today's behavior) and maximum
-- (NULL = no limit beyond stock) purchasable quantity per cart/order.
--
--   min_cart_qty INT NOT NULL DEFAULT 1, CHECK >= 1
--   max_cart_qty INT NULL, CHECK >= 1 when set
-- The max >= min relation is enforced at the app layer (supplier
-- endpoint validation) so the error can be a clear Persian message
-- instead of an opaque SQLSTATE 23514.
--
-- Existing rows get the defaults: min 1, max NULL — behavior is
-- byte-identical for every product whose supplier never touches the
-- new setting. Variant-less products are covered too: their single
-- default variant row carries the limits, and the public product DTO
-- surfaces them as the product's own minCartQty/maxCartQty.

ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS min_cart_qty INT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS max_cart_qty INT NULL;

ALTER TABLE product_variants
  DROP CONSTRAINT IF EXISTS product_variants_min_cart_qty_check,
  ADD CONSTRAINT product_variants_min_cart_qty_check CHECK (min_cart_qty >= 1),
  DROP CONSTRAINT IF EXISTS product_variants_max_cart_qty_check,
  ADD CONSTRAINT product_variants_max_cart_qty_check CHECK (max_cart_qty IS NULL OR max_cart_qty >= 1);
