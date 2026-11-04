-- Down migration for 202610250403: remove the store-selection tables,
-- the linked-product column and the per-store section title. Order
-- history is unaffected (order_item_addons snapshots carry no FK here).
DROP TABLE IF EXISTS store_product_addon_exclusions;
DROP TABLE IF EXISTS store_product_addons;
ALTER TABLE product_addons DROP COLUMN IF EXISTS linked_product_id;
ALTER TABLE stores DROP COLUMN IF EXISTS addon_section_title;
