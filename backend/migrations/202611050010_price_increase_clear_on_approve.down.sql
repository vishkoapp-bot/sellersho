-- 202611050010: revert the clear-intent flag on price-increase requests.
ALTER TABLE supplier_price_increases DROP COLUMN IF EXISTS clear_on_approve;
