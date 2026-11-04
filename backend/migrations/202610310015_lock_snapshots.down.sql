-- Reverse 202610310015_lock_snapshots. Locks themselves are untouched;
-- only the snapshot rows go. Unlocking after this down migration falls
-- back to keeping the current margin (the pre-Ruling-B behaviour).
DROP TABLE IF EXISTS store_margin_lock_snapshots;
