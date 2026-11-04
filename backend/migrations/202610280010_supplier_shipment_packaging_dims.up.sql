-- 202610280010: packed-parcel packaging dimensions on the supplier
-- shipment record (Oct 28, 2026 — Tapin shipment-registration follow-up,
-- user spec ~23:10 Tehran: the ACTUAL packed parcel's L/W/H in cm,
-- captured/confirmed at pack time on the supplier order page).
--
-- ALIGNMENT with the concurrent dimensions mission (202610280008):
-- the SAME storage rule — millimetres in *_mm columns, centimetres
-- at the API/display boundary (cm x 10 on write, mm / 10 on read).
-- Same naming stem (packaging_length_mm / width_mm / height_mm).
-- The difference is the ROW, not the schema: 202610280008 stores the
-- pack-time confirmed parcel on orders (warehouse flow, one parcel
-- per order). A supplier-direct order can have SEVERAL shipments
-- (one per supplier leg), so one orders row cannot hold several
-- parcels — the supplier flow stores its confirmed parcel on its OWN
-- shipment row, alongside the Tapin barcode and send_price, and does
-- NOT overwrite orders.packaging_*_mm. Pricing readers: for a
-- supplier-direct leg read supplier_shipments.packaging_*_mm; for a
-- warehouse parcel read orders.packaging_*_mm. Never price from
-- product dimensions (products.length_mm etc.).
--
-- IDEMPOTENCY: ADD COLUMN IF NOT EXISTS, so re-running UP is a no-op.
-- Existing rows keep NULL (unknown) — no silent data change.
ALTER TABLE supplier_shipments
  ADD COLUMN IF NOT EXISTS packaging_length_mm INT,
  ADD COLUMN IF NOT EXISTS packaging_width_mm INT,
  ADD COLUMN IF NOT EXISTS packaging_height_mm INT;
