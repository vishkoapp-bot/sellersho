-- 202611060001: variant-scoped seller discount campaigns (option A +
-- seller request, 2026-11-06). A campaign may now target ONE VARIANT
-- (scope='variant', target_id = variant_id): the most specific scope
-- wins (variant > product > niche > store). No data changes; only the
-- scope CHECK widens. Product-level pricing ignores variant-scoped
-- campaigns; the variant line prices them on PDP/cart/checkout, and
-- the discount-quote/order math already resolves per variant.

ALTER TABLE seller_discount_campaigns
  DROP CONSTRAINT seller_discount_campaigns_scope_check;
ALTER TABLE seller_discount_campaigns
  ADD CONSTRAINT seller_discount_campaigns_scope_check
  CHECK (scope IN ('product', 'variant', 'niche', 'store'));
