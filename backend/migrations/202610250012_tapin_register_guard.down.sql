-- Rollback for 202610250012_tapin_register_guard: drop the guard stamp.
-- (Any in-flight unknown-outcome attempt loses its protection; only
-- roll back when no ship-with-tapin retry is pending.)
ALTER TABLE supplier_shipments
    DROP COLUMN IF EXISTS tapin_register_attempted_at;
