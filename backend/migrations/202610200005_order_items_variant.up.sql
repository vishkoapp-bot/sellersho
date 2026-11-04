-- 202610200005: order line variant snapshot (store-gaps spec §۱.۳).
-- variant_id is a plain FK (no ON DELETE action): v1 has no variant-delete
-- path, and variant_name/variant_spnc keep the historical snapshot even so.
-- Orders without a variant keep all three NULL — byte-identical behavior.

ALTER TABLE order_items ADD COLUMN IF NOT EXISTS variant_id   UUID NULL REFERENCES product_variants(id);
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS variant_name TEXT NULL;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS variant_spnc VARCHAR(12) NULL;
CREATE INDEX IF NOT EXISTS idx_order_items_variant ON order_items (variant_id) WHERE variant_id IS NOT NULL;
