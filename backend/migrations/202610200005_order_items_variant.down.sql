-- Down for 202610200005 (order_items variant snapshot).

DROP INDEX IF EXISTS idx_order_items_variant;
ALTER TABLE order_items DROP COLUMN IF EXISTS variant_spnc;
ALTER TABLE order_items DROP COLUMN IF EXISTS variant_name;
ALTER TABLE order_items DROP COLUMN IF EXISTS variant_id;
