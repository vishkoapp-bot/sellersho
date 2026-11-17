-- Never discard recorded supplier origin mappings during rollback.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM supplier_warehouses
             WHERE tapin_province_id IS NOT NULL OR tapin_city_id IS NOT NULL) THEN
    RAISE EXCEPTION 'supplier warehouse geography in use; rollback requires audited reconciliation';
  END IF;
END $$;
ALTER TABLE supplier_warehouses
  DROP COLUMN IF EXISTS tapin_city_id,
  DROP COLUMN IF EXISTS tapin_province_id;
