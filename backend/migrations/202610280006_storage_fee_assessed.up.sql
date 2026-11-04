-- Storage-fee assessment horizon (rework v2 §5.5, test 22).
--
-- The design pins two facts that one checkpoint cannot satisfy at
-- once: when a goods class is toggled OFF, the pass charges nothing
-- AND last_fee_date does NOT advance — yet when the class is turned
-- back ON, the dark days must NEVER be charged retroactively. With a
-- single checkpoint, the re-enable pass would see the dark days as
-- "uncharged" (they are after last_fee_date) and charge them.
--
-- last_assessed_date is the second marker: the last calendar day the
-- pass has LOOKED at, whether it charged it or skipped it (class
-- off). The charge window starts after MAX(last_fee_date,
-- last_assessed_date), so dark days fall below the window on
-- re-enable, while last_fee_date still only moves on real charges.
-- All amounts are integer Tomans.

ALTER TABLE warehouse_storage_positions
  ADD COLUMN IF NOT EXISTS last_assessed_date DATE;
