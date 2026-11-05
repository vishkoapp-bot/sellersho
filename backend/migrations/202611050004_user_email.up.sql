-- 202611050004: persist the supplier registration email on users.
--
-- The supplier registration endpoint has decoded an `email` field
-- since it was introduced and HandleRegister passes it to
-- RegisterSupplier, but the value was silently discarded: the users
-- table had no email column and the INSERT never stored it. This
-- adds the column so registration can keep the address it collected.
-- Nullable and unindexed: existing rows simply have no email until
-- one is provided through a flow that collects it.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email VARCHAR(254);
