-- PENDING-LIVE-CHECK — قبل از دیپلوی، مجاز بودن sold در لایو باید تایید شود
--
-- Status: NOT deploy-ready, NOT applied to live.
-- This is a production schema change: deploying it to live requires a
-- verified backup and a rollback plan, and it has NOT been applied to live.
--
-- Why this file exists: decrementCanonicalSaleTx (internal/repository/
-- inventory_sync.go) books a sale as sellable -> sold via moveInventoryTx,
-- which inserts an inventory_balances row with status='sold'. The phase-2
-- staging migration chain records inventory_balances_status_check as
-- allowing only physical, sellable, sales_reserved, return_requested,
-- quarantine, in_transit, damaged, unreconciled (see the staging fix
-- report) — 'sold' is NOT in that list, so the insert fails on that chain.
-- Whether the LIVE database allows 'sold' could NOT be verified from this
-- session: no Selersho server SSH address/alias was supplied with this task,
-- ~/.ssh/config has no Selersho entry, and no Selersho address is recorded
-- in memory, so the read-only live constraint check could not be run.
--
-- Before ANY use: read the live constraint definition read-only
-- (information_schema / pg_constraint for inventory_balances, or \d in the
-- production Postgres container — no UPDATE/INSERT/DELETE, no container
-- stop/restart) and confirm:
--   1) the live constraint name (assumed below to be
--      inventory_balances_status_check, taken from the staging error name —
--      if live uses a different name, adjust the DROP below, otherwise the
--      old constraint stays in force and 'sold' stays blocked);
--   2) the exact live allowed-status list, so the new list below is the
--      live list PLUS 'sold' and nothing else is silently dropped or added.
-- If live already allows 'sold', DO NOT apply this migration at all; only
-- the staging chain needs aligning. Do NOT substitute another already-
-- allowed status with a wrong meaning for 'sold'.
--
-- Up: rebuild the status CHECK to add 'sold' to the phase-2-recorded list.
-- (The list below is the phase-2-recorded list + 'sold', pending the live
-- confirmation above — it is NOT a verified live definition.)
ALTER TABLE inventory_balances
  DROP CONSTRAINT IF EXISTS inventory_balances_status_check;
ALTER TABLE inventory_balances
  ADD CONSTRAINT inventory_balances_status_check
  CHECK (status IN (
    'physical', 'sellable', 'sales_reserved', 'return_requested',
    'quarantine', 'in_transit', 'damaged', 'unreconciled', 'sold'
  ));
