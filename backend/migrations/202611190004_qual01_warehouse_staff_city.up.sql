-- The historic Tehran default is not evidence that a staff member is assigned
-- to Tehran. Require explicit confirmation before it can authorize site work.
ALTER TABLE users ADD COLUMN warehouse_city_confirmed BOOLEAN NOT NULL DEFAULT FALSE;
