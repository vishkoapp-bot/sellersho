DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM warehouse_departure_events) OR EXISTS(SELECT 1 FROM warehouse_occupancy_releases) OR EXISTS(SELECT 1 FROM warehouse_occupancy_exceptions) OR EXISTS(SELECT 1 FROM warehouse_departure_unresolved_sources) THEN RAISE EXCEPTION 'cannot discard persisted physical departure evidence'; END IF;
END $$;
DROP TRIGGER disputed_warehouse_capacity ON warehouse_bins;
DROP FUNCTION guard_disputed_warehouse_capacity();
DROP VIEW warehouse_occupancy_orphan_allocations;
DROP VIEW warehouse_occupancy_audit;
DROP VIEW warehouse_remaining_placements;
DROP TABLE warehouse_occupancy_releases;
DROP TABLE warehouse_departure_unresolved_sources;
DROP TABLE warehouse_departure_events;
DROP TABLE warehouse_occupancy_exceptions;
DROP FUNCTION guard_warehouse_release_insert();
DROP FUNCTION guard_warehouse_departure_insert();
DROP FUNCTION expected_warehouse_departure(TEXT,TEXT);
DROP FUNCTION warehouse_bin_identity(TEXT,TEXT);
DROP FUNCTION immutable_warehouse_departure();
