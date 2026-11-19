DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM users WHERE warehouse_city_confirmed) THEN
    RAISE EXCEPTION 'warehouse city confirmations exist; refusing to discard authorization evidence';
  END IF;
END $$;
ALTER TABLE users DROP COLUMN warehouse_city_confirmed;
