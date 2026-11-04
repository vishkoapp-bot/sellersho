-- Seller-curated related products (Oct 25, 2026, complementary mission):
-- per-store links from one product to others the SAME store sells, plus a
-- per-store customizable related-section title.
--
-- Store scoping is the point: the link is keyed (store_id, source_product_id)
-- and every read is filtered by the viewing store, so store A's picks can
-- never appear on store B's PDP or on central. Do NOT copy the add-on
-- attachment model (product_addon_attachments, product-keyed only) — that
-- model leaks across stores (audit: related-pdp-report §3).
CREATE TABLE IF NOT EXISTS store_related_products (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id          VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    source_product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    target_product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    sort_order        INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (store_id, source_product_id, target_product_id),
    CHECK (source_product_id <> target_product_id)
);
CREATE INDEX IF NOT EXISTS idx_store_related_source
    ON store_related_products (store_id, source_product_id, sort_order);

-- Per-store related-section title (nullable = use the template default:
-- central «کالاهای مرتبط و مشابه», Modern/Electronic «محصولات مرتبط»,
-- Fashion «محصولات مشابه», Digikala «کالاهای مرتبط», Tech «محصولات مرتبط»).
-- A sibling stream adds its own separate addon_section_title field — this
-- column is ONLY for the related rail.
ALTER TABLE stores ADD COLUMN IF NOT EXISTS related_section_title VARCHAR(200);
