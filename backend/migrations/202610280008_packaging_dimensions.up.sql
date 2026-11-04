-- 202610280008: packaging dimensions (Oct 28, 2026 — user order ~23:10 Tehran).
--
--   products.packaging_length_mm / packaging_width_mm / packaging_height_mm
--   orders.packaging_length_mm   / packaging_width_mm   / packaging_height_mm
--
-- Postal cost is priced on the PACKAGING dimensions, not the product
-- dimensions — so registration captures BOTH:
--   - product dimensions (length_mm/width_mm/height_mm, mandatory at
--     approval since 20261028) — the product itself;
--   - packaging dimensions — the per-product DEFAULT parcel size the
--     supplier declares at registration (optional, NOT part of the
--     approval gate: packaging varies per order, so the default is
--     confirmed / adjusted at pack time and stored on the order).
--
-- STORAGE (single source of truth, same rule as product dimensions):
-- millimetres in *_mm columns; entry/display is CENTIMETRES
-- everywhere (cm × 10 on write, mm ÷ 10 on display). No parallel cm
-- columns — they would drift apart.
--
-- READERS (pricing missions): products.packaging_*_mm is the
-- per-product default (Tapin quote v2 box selection, return
-- settlement); orders.packaging_*_mm is the pack-time confirmed
-- parcel for THAT shipment and wins over the product default.
--
-- IDEMPOTENCY: ADD COLUMN IF NOT EXISTS, so re-running UP is a no-op.
-- Existing rows keep NULL (unknown) — no silent data change, no
-- unpublishing; they surface in the missing-measurements backlog.
ALTER TABLE products
  ADD COLUMN IF NOT EXISTS packaging_length_mm INT,
  ADD COLUMN IF NOT EXISTS packaging_width_mm INT,
  ADD COLUMN IF NOT EXISTS packaging_height_mm INT;
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS packaging_length_mm INT,
  ADD COLUMN IF NOT EXISTS packaging_width_mm INT,
  ADD COLUMN IF NOT EXISTS packaging_height_mm INT;
