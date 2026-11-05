-- 202611050007: real supplier suspension (gap 12).
--
-- Until now the only way to stop a supplier was blocking its owner's
-- USER account (users.is_active), which is a person-level switch: the
-- supplier's products kept selling and only panel access changed.
-- Suspension is the business-level state: approval_status gains the
-- value 'suspended' (the column is an unconstrained VARCHAR(20), so
-- no CHECK needs amending) and these columns record who suspended the
-- supplier, when, and why. State machine:
--   pending_approval -> approved <-> suspended
--   pending_approval -> rejected (terminal)
-- Reactivation returns the supplier to 'approved' and clears all
-- three columns; it never touches users.is_active (suspension and
-- user-block stay orthogonal).
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS suspended_by TEXT;
ALTER TABLE suppliers ADD COLUMN IF NOT EXISTS suspension_reason TEXT;
