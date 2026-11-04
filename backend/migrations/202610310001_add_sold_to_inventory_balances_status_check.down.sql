-- PENDING-LIVE-CHECK — قبل از دیپلوی، مجاز بودن sold در لایو باید تایید شود
--
-- Down migration for 202610310001_add_sold_to_inventory_balances_status_check.
-- Status: NOT deploy-ready, NOT applied to live — same PENDING-LIVE-CHECK
-- condition as the up migration. NEVER run a down migration on live without
-- a verified backup and an explicit rollback decision.
--
-- This restores the CHECK to the phase-2-recorded list WITHOUT 'sold'
-- (physical, sellable, sales_reserved, return_requested, quarantine,
-- in_transit, damaged, unreconciled). That list is the staging-chain record,
-- NOT a verified live definition: before any use, confirm the true prior
-- live definition read-only (see the up file header) and adjust this file
-- if live differed. Note: rolling back while 'sold' balance rows exist will
-- fail on the CHECK — any rollback must first account for those rows.
ALTER TABLE inventory_balances
  DROP CONSTRAINT IF EXISTS inventory_balances_status_check;
ALTER TABLE inventory_balances
  ADD CONSTRAINT inventory_balances_status_check
  CHECK (status IN (
    'physical', 'sellable', 'sales_reserved', 'return_requested',
    'quarantine', 'in_transit', 'damaged', 'unreconciled'
  ));
