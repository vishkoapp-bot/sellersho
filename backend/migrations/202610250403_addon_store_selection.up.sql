-- 202610250403: Complementary products — store selection («انتخاب مکمل»).
--
-- Model (user decision Oct 25, 2026, clarified ~19:16, central rule ~19:20):
--   - A SUPPLIER defines complementary products for their product (a pool
--     per product, via product_addons + product_addon_attachments).
--     Defined ≠ displayed: a defined add-on shows NOWHERE by itself.
--   - It displays ONLY when the DROPSHIPPER selects it, at product-
--     selection time (select all / one-by-one / none). Only the selected
--     ones show on THAT dropshipper's storefront PDP for that product —
--     never on another store.
--   - The DROPSHIPPER can also pick ANY product from the store's own
--     activated products as a complementary product (a direct pick — no
--     add-on row is created; the pick itself is the selection).
--   - Fee = the complementary product's own resolved price for the
--     viewing store (no custom override). Existing custom title/price
--     add-ons (services like warranty) are untouched.
--   - CENTRAL: the supplier's defined list is SELECTED BY DEFAULT
--     (pre-ticked); the admin can untick per product (an exclusion row).
--     Central direct picks are opt-in rows like any store's.
--
-- Resolution (ResolveProductAddons, addon_repo.go) reads ONLY these
-- tables (+ the exclusion table on central) — the old unscoped
-- attachment query is gone. PDP and order totals share the resolution,
-- so display and charge can never diverge.
--
-- Fulfillment note: a complementary product is a FEE LINE on the parent
-- order line (ships with the order, no stock movement, no separate
-- shipment, no wholesale cost C). The fee flows 100% to the owning
-- supplier (Ws) — for a direct pick, the owner is the picked product's
-- supplier, which may differ from the host product's supplier
-- (cross-supplier case: S2 is paid for the add-on while the parent line
-- ships per the host product's fulfillment).

-- (a) A supplier/platform add-on may BE a catalog product: the linked
-- product's title/image/price are read live at resolve time. Nullable —
-- existing fee add-ons are untouched. ON DELETE SET NULL: deleting the
-- linked product turns the add-on back into a plain fee add-on only if
-- it still has its own title/price; resolution skips linked add-ons
-- whose product is gone (fail-closed in the resolver).
ALTER TABLE product_addons
    ADD COLUMN IF NOT EXISTS linked_product_id VARCHAR(50) REFERENCES products(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_product_addons_linked
    ON product_addons (linked_product_id);

-- (c) Per-store add-on selection: one row per (store, host product,
-- selected add-on OR directly-picked product). Exactly one of addon_id /
-- linked_product_id is set (CHECK); each is unique per (store, product).
CREATE TABLE IF NOT EXISTS store_product_addons (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id          VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id        VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    addon_id          VARCHAR(50) REFERENCES product_addons(id) ON DELETE CASCADE,
    linked_product_id VARCHAR(50) REFERENCES products(id) ON DELETE CASCADE,
    sort_order        INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((addon_id IS NOT NULL) <> (linked_product_id IS NOT NULL)),
    UNIQUE (store_id, product_id, addon_id),
    UNIQUE (store_id, product_id, linked_product_id)
);
CREATE INDEX IF NOT EXISTS idx_store_product_addons_lookup
    ON store_product_addons (store_id, product_id);

-- (b) Central opt-outs: on central the defined list resolves by default;
-- the admin unticks a defined add-on for a product by inserting a row
-- here (and re-ticks by deleting it). Dropshipper stores never read
-- this table — their selection is opt-in rows only.
CREATE TABLE IF NOT EXISTS store_product_addon_exclusions (
    store_id   VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    product_id VARCHAR(50) NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    addon_id   VARCHAR(50) NOT NULL REFERENCES product_addons(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (store_id, product_id, addon_id)
);

-- (e) Per-store complementary section title. NULL/empty = the default
-- «محصولات مکمل». Separate from the related stream's
-- related_section_title (different column, different feature).
ALTER TABLE stores ADD COLUMN IF NOT EXISTS addon_section_title VARCHAR(200);
