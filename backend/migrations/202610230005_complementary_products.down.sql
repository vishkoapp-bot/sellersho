-- Down for 202610230005 (complementary products): remove the add-on
-- schema. Order history snapshots in order_item_addons are discarded with
-- the feature rollback — that is the point of rolling back; the frozen
-- orders.platform_addon_amount / supplier_addon_amount columns are dropped
-- as well (their values are reproducible from the snapshots while the
-- feature is live, and meaningless without it).

ALTER TABLE pending_approvals DROP CONSTRAINT IF EXISTS pending_approvals_entity_type_check;
ALTER TABLE pending_approvals ADD CONSTRAINT pending_approvals_entity_type_check
    CHECK (entity_type IN ('store_name', 'store_slug', 'custom_domain', 'proposed_product', 'payout', 'warranty_property', 'product_gallery_update', 'product_variant_create', 'product_variant_create_bulk'));

ALTER TABLE orders DROP COLUMN IF EXISTS supplier_addon_amount;
ALTER TABLE orders DROP COLUMN IF EXISTS platform_addon_amount;

DROP TABLE IF EXISTS order_item_addons;
DROP TABLE IF EXISTS product_addon_attachments;
DROP TABLE IF EXISTS product_addons;
