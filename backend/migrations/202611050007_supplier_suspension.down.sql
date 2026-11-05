-- 202611050007: revert the supplier suspension columns.
ALTER TABLE suppliers DROP COLUMN IF EXISTS suspension_reason;
ALTER TABLE suppliers DROP COLUMN IF EXISTS suspended_by;
ALTER TABLE suppliers DROP COLUMN IF EXISTS suspended_at;
