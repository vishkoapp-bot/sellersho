-- Down for 202610230008 (category visibility): drop the is_active flag.
-- Visibility state is not preserved across a rollback (all categories
-- become visible again, matching the pre-202610230008 behavior).

ALTER TABLE categories DROP COLUMN IF EXISTS is_active;
