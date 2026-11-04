-- 202610230005: Complementary products («محصولات مکمل») — opt-in add-ons
-- attached to products, created by the PLATFORM (admin) or by a SUPPLIER
-- (the flagship instance is «گارانتی پلتفرم», the platform warranty).
--
-- An add-on is NOT a standalone catalog product: it has no catalog route,
-- no stock and no shipment of its own (kind='physical' is informational in
-- v1 — the add-on rides on its parent item's shipment as a fee/service
-- line). The customer opts in per cart line; the fee is PER UNIT and is
-- snapshotted onto order_item_addons at order time, exactly like
-- order_items.final_unit_price, so later price edits never rewrite order
-- history.
--
-- Money isolation (ledger identity extends to P = C + D + F + S + Wp + ΣWs):
--   - platform add-on fees (Wp)  -> 100% platform_revenue at release;
--   - supplier add-on fees (Ws)  -> 100% that supplier's supplier_payable,
--     as ledger lines SEPARATE from their wholesale share C (the add-on
--     revenue must never pollute the product cost reports that read C);
--   - NO platform commission is taken on supplier add-ons in v1, and the
--     referral base stays F-only.
-- orders.platform_addon_amount / supplier_addon_amount freeze the two
-- totals at creation so the release path (which never recomputes prices)
-- can read them directly.
--
-- Supplier add-ons are approval-gated: creation and material edits are
-- staged in pending_approvals (entity_type 'product_addon') and the live
-- row lands/changes only on admin approval — the same pattern as
-- warranty_property and the Fix-8 catalog proposals. Supplier rows are
-- born approval_status='pending', is_active=FALSE.
--
-- The seeded «گارانتی پلتفرم» ships INACTIVE (is_active=FALSE): the
-- mechanism goes live only when an admin defines the coverage terms and
-- activates it from the catalog tab. Until then no customer surface shows
-- it and no fee can be charged.

CREATE TABLE IF NOT EXISTS product_addons (
    id                VARCHAR(50) PRIMARY KEY,
    owner_type        VARCHAR(10) NOT NULL CHECK (owner_type IN ('platform', 'supplier')),
    owner_supplier_id VARCHAR(50) REFERENCES suppliers(id),
    title             VARCHAR(200) NOT NULL,
    description       TEXT,
    kind              VARCHAR(10) NOT NULL DEFAULT 'service' CHECK (kind IN ('service', 'physical')),
    price_mode        VARCHAR(10) NOT NULL CHECK (price_mode IN ('fixed', 'percent')),
    price_value       DOUBLE PRECISION NOT NULL,
    cap_toman         BIGINT NOT NULL DEFAULT 0,
    approval_status   VARCHAR(12) NOT NULL DEFAULT 'approved' CHECK (approval_status IN ('pending', 'approved', 'rejected')),
    is_active         BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order        INT NOT NULL DEFAULT 0,
    created_by        VARCHAR(100) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((owner_type = 'platform' AND owner_supplier_id IS NULL)
        OR (owner_type = 'supplier' AND owner_supplier_id IS NOT NULL)),
    CHECK (price_value >= 0),
    CHECK (cap_toman >= 0)
);

CREATE INDEX IF NOT EXISTS idx_product_addons_owner
    ON product_addons (owner_type, owner_supplier_id);
CREATE INDEX IF NOT EXISTS idx_product_addons_resolvable
    ON product_addons (is_active, approval_status);

-- Attachment scopes: a platform add-on may attach to one product, a whole
-- category, or globally. A SUPPLIER add-on may only attach (scope
-- 'product') to products that supplier owns — that rule is enforced
-- fail-closed in the repository (it needs the products.supplier_id join),
-- not only here.
CREATE TABLE IF NOT EXISTS product_addon_attachments (
    addon_id      VARCHAR(50) NOT NULL REFERENCES product_addons(id) ON DELETE CASCADE,
    scope_type    VARCHAR(10) NOT NULL CHECK (scope_type IN ('product', 'category', 'global')),
    product_id    VARCHAR(50) REFERENCES products(id) ON DELETE CASCADE,
    category_slug VARCHAR(100),
    CHECK ((scope_type = 'product'  AND product_id IS NOT NULL AND category_slug IS NULL)
        OR (scope_type = 'category' AND category_slug IS NOT NULL AND product_id IS NULL)
        OR (scope_type = 'global'   AND product_id IS NULL AND category_slug IS NULL))
);

-- One row per (add-on, scope, target): the NULL-safe uniqueness lives in
-- an expression index (Postgres PRIMARY KEYs cannot carry expressions).
CREATE UNIQUE INDEX IF NOT EXISTS uq_product_addon_attachments_target
    ON product_addon_attachments (addon_id, scope_type, COALESCE(product_id, ''), COALESCE(category_slug, ''));

CREATE INDEX IF NOT EXISTS idx_product_addon_attachments_product
    ON product_addon_attachments (product_id) WHERE scope_type = 'product';
CREATE INDEX IF NOT EXISTS idx_product_addon_attachments_category
    ON product_addon_attachments (category_slug) WHERE scope_type = 'category';

-- Per-order-item add-on snapshots. addon_id deliberately carries NO hard
-- FK to product_addons: deleting an add-on must never break order history
-- (the snapshot columns below are the durable record).
CREATE TABLE IF NOT EXISTS order_item_addons (
    id                         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_item_id              UUID NOT NULL REFERENCES order_items(id) ON DELETE CASCADE,
    addon_id                   VARCHAR(50),
    title_snapshot             VARCHAR(200) NOT NULL,
    owner_type_snapshot        VARCHAR(10) NOT NULL,
    owner_supplier_id_snapshot VARCHAR(50),
    fee_per_unit               BIGINT NOT NULL,
    quantity                   INT NOT NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_order_item_addons_item
    ON order_item_addons (order_item_id);
CREATE INDEX IF NOT EXISTS idx_order_item_addons_supplier
    ON order_item_addons (owner_supplier_id_snapshot) WHERE owner_type_snapshot = 'supplier';

ALTER TABLE orders ADD COLUMN IF NOT EXISTS platform_addon_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS supplier_addon_amount BIGINT NOT NULL DEFAULT 0;

-- Supplier add-on proposals flow through the shared approval inbox.
ALTER TABLE pending_approvals DROP CONSTRAINT IF EXISTS pending_approvals_entity_type_check;
ALTER TABLE pending_approvals ADD CONSTRAINT pending_approvals_entity_type_check
    CHECK (entity_type IN ('store_name', 'store_slug', 'custom_domain', 'proposed_product', 'payout', 'warranty_property', 'product_gallery_update', 'product_variant_create', 'product_variant_create_bulk', 'product_addon'));

-- Seed: the platform warranty as a platform-owned add-on. INACTIVE on
-- purpose (see header). Percent pricing mirrors the approved design
-- (5% of the item's final price, no cap until an admin sets one).
INSERT INTO product_addons (
    id, owner_type, owner_supplier_id, title, description, kind,
    price_mode, price_value, cap_toman, approval_status, is_active,
    sort_order, created_by
) VALUES (
    'addon-platform-warranty', 'platform', NULL,
    'گارانتی پلتفرم',
    'پوشش تکمیلی پلتفرم — جدا از گارانتی تأمین‌کننده.',
    'service', 'percent', 5, 0, 'approved', FALSE, 0, 'system'
) ON CONFLICT (id) DO NOTHING;

INSERT INTO product_addon_attachments (addon_id, scope_type, product_id, category_slug)
VALUES ('addon-platform-warranty', 'global', NULL, NULL)
ON CONFLICT DO NOTHING;
