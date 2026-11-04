-- 202610170009_drop_legacy_variant_resync: retire the legacy trigger that
-- recomputed products.stock = SUM(product_variants.stock).
--
-- products.stock is now projected from the canonical inventory_balances
-- ledger by trg_inventory_balances_sync_stock (migration 202610170001).
-- The legacy trigger clobbered the canonical projection on every
-- product_variants write (warehouse receives, QC restocks), so it must go.
--
-- Purely subtractive: touches no data, only removes the trigger+function.

DROP TRIGGER IF EXISTS trg_variants_resync_product_stock ON product_variants;
DROP FUNCTION IF EXISTS fn_variants_resync_product_stock();
