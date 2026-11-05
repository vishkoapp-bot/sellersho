-- 202611050002: per-store wishlists.
--
-- Owner doctrine: every dropshipper store is a COMPLETELY SEPARATE
-- SITE with its own wishlist; the central database only stores the
-- rows. customer_wishlists (202610310002) keyed a single global list
-- per user, so a product hearted inside one store surfaced — at
-- central prices — in every other context. The store dimension fixes
-- that: every row now belongs to exactly one store.
--
-- Existing rows were all placed through the old global surface, i.e.
-- the central store's wishlist, so the column default attributes
-- them to 'store-central' — no heart is lost or moved.
ALTER TABLE customer_wishlists
    ADD COLUMN IF NOT EXISTS store_id TEXT NOT NULL DEFAULT 'store-central';

ALTER TABLE customer_wishlists DROP CONSTRAINT IF EXISTS customer_wishlists_pkey;
ALTER TABLE customer_wishlists
    ADD CONSTRAINT customer_wishlists_pkey PRIMARY KEY (user_id, store_id, product_id);

CREATE INDEX IF NOT EXISTS idx_customer_wishlists_user_store
    ON customer_wishlists (user_id, store_id);
