DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM inventory_movements LIMIT 1)
     OR EXISTS (SELECT 1 FROM supplier_return_parcels LIMIT 1)
     OR EXISTS (SELECT 1 FROM supplier_return_financial_events LIMIT 1)
     OR EXISTS (SELECT 1 FROM product_measurement_events LIMIT 1)
     OR EXISTS (SELECT 1 FROM putaway_capacity_reservations LIMIT 1) THEN
    RAISE EXCEPTION '000024 rollback blocked: operational data exists; archive and reconcile before rollback';
  END IF;
END $$;

DROP TABLE IF EXISTS putaway_capacity_reservations;
DROP TABLE IF EXISTS product_measurement_events;
DROP TABLE IF EXISTS carrier_tariffs;
DROP TABLE IF EXISTS carrier_configs;
DROP TABLE IF EXISTS supplier_return_financial_events;
DROP TABLE IF EXISTS supplier_return_parcel_items;
DROP TABLE IF EXISTS supplier_return_parcels;
DROP TABLE IF EXISTS supplier_return_events;
DROP TABLE IF EXISTS inventory_reservations;
DROP TABLE IF EXISTS inventory_movements;
DROP TABLE IF EXISTS inventory_balances;

ALTER TABLE supplier_return_requests
  DROP COLUMN IF EXISTS updated_at,
  DROP COLUMN IF EXISTS inventory_reference_id,
  DROP COLUMN IF EXISTS failed_quantity,
  DROP COLUMN IF EXISTS accepted_quantity,
  DROP COLUMN IF EXISTS inspection_status,
  DROP COLUMN IF EXISTS inspected_at,
  DROP COLUMN IF EXISTS received_at,
  DROP COLUMN IF EXISTS handover_at,
  DROP COLUMN IF EXISTS cancelled_by,
  DROP COLUMN IF EXISTS reviewed_by,
  DROP COLUMN IF EXISTS idempotency_key;

ALTER TABLE warehouse_bins
  DROP COLUMN IF EXISTS max_weight_grams,
  DROP COLUMN IF EXISTS capacity_status,
  DROP COLUMN IF EXISTS occupied_weight_grams,
  DROP COLUMN IF EXISTS occupied_volume_cm3,
  DROP COLUMN IF EXISTS capacity_volume_cm3,
  DROP COLUMN IF EXISTS warehouse_name;

ALTER TABLE products
  DROP COLUMN IF EXISTS measurement_version,
  DROP COLUMN IF EXISTS measurement_status,
  DROP COLUMN IF EXISTS weight_grams,
  DROP COLUMN IF EXISTS height_mm,
  DROP COLUMN IF EXISTS width_mm,
  DROP COLUMN IF EXISTS length_mm;