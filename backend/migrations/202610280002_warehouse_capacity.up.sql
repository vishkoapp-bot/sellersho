-- Central-warehouse capacity + receiving gate (2026-10-28).
-- Design §6.2 (base) + addendum §5.2 (dual caps).
--
-- DUAL INDEPENDENT CAPS (addendum §5.2): capacity_units_total (item
-- count) and capacity_m3_total (cubic metres) apply independently
-- (AND): a shipment must pass every cap that is set; NULL = that cap
-- is unlimited. No capacity_mode column — the design makes an
-- inconsistent state ("mode says volume but only units is set")
-- impossible. Volume is stored in m³ (NUMERIC, 3 decimals); all
-- internal math is done in integer cm³ (the warehouse_bins precedent,
-- §0.2) and converted only at the storage/display boundary.
--
-- is_receiving_open is independent of both caps: an active warehouse
-- may temporarily stop receiving (repairs, stock-count) while its
-- stocked inventory keeps allocating for sales.
ALTER TABLE warehouses
  ADD COLUMN IF NOT EXISTS capacity_units_total INTEGER
    CHECK (capacity_units_total IS NULL OR capacity_units_total >= 0),
  ADD COLUMN IF NOT EXISTS capacity_m3_total NUMERIC(10,3)
    CHECK (capacity_m3_total IS NULL OR capacity_m3_total > 0),
  ADD COLUMN IF NOT EXISTS is_receiving_open BOOLEAN NOT NULL DEFAULT TRUE;
