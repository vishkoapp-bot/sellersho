-- 202611050004: revert the users email column.
ALTER TABLE users DROP COLUMN IF EXISTS email;
