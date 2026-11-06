-- 202611060001 down: restore the three-scope CHECK. Variant-scoped
-- rows cannot satisfy the restored constraint, so they are removed
-- first (data loss is confined to variant-scoped campaigns, which the
-- previous schema could not represent anyway).

DELETE FROM seller_discount_campaigns WHERE scope = 'variant';
ALTER TABLE seller_discount_campaigns
  DROP CONSTRAINT seller_discount_campaigns_scope_check;
ALTER TABLE seller_discount_campaigns
  ADD CONSTRAINT seller_discount_campaigns_scope_check
  CHECK (scope IN ('product', 'niche', 'store'));
