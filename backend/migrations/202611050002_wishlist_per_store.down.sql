-- 202611050002: revert per-store wishlists.
--
-- The global schema has no place for rows hearted under non-central
-- stores; those rows are dropped with the dimension they belong to.
-- Central rows survive and become the single global list again.
DROP INDEX IF EXISTS idx_customer_wishlists_user_store;

ALTER TABLE customer_wishlists DROP CONSTRAINT IF EXISTS customer_wishlists_pkey;

DELETE FROM customer_wishlists WHERE store_id <> 'store-central';

ALTER TABLE customer_wishlists DROP COLUMN IF EXISTS store_id;

ALTER TABLE customer_wishlists
    ADD CONSTRAINT customer_wishlists_pkey PRIMARY KEY (user_id, product_id);
