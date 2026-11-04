-- 202610280006 DOWN — reverses the numeric public product/variant numbers.
--
-- Drops products.spn_number + product_variants.spnc_number and both
-- sequences. The internal codes (products.spn "PC-...", product_variants.spnc
-- hex) are NOT touched — they live in other migrations and keep working.
--
-- ⚠️  IRREVERSIBLE DATA LOSS (by design): dropping the columns destroys the
-- numbers. The only record is the backfill mapping at
-- ~/workspace/product-urls-20261028/build/backend/backfill-mapping.json —
-- re-applying UP afterwards re-derives the SAME numbers only if no product
-- or variant was created/deleted in between (the backfill is a pure
-- function of created_at/id). After a rollback, regenerate the mapping
-- from prod before re-applying UP if any catalog rows changed.

ALTER TABLE products DROP CONSTRAINT IF EXISTS products_spn_number_unique;
ALTER TABLE products DROP COLUMN IF EXISTS spn_number;
ALTER TABLE product_variants DROP CONSTRAINT IF EXISTS product_variants_spnc_number_unique;
ALTER TABLE product_variants DROP COLUMN IF EXISTS spnc_number;
DROP SEQUENCE IF EXISTS products_spn_number_seq;
DROP SEQUENCE IF EXISTS product_variants_spnc_number_seq;
