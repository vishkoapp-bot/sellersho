DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM warehouse_return_bin_entries) OR EXISTS(SELECT 1 FROM warehouse_placements WHERE source_kind='customer_return') THEN RAISE EXCEPTION 'cannot discard retained customer return bin evidence'; END IF;
END $$;
DROP TRIGGER customer_return_bin_entry_apply ON warehouse_return_bin_entries;
DROP FUNCTION apply_customer_return_bin_entry();
DROP TRIGGER warehouse_return_placement_insert ON warehouse_placements;
DROP FUNCTION guard_return_warehouse_placement();
DROP TRIGGER customer_return_bin_entry_guard ON warehouse_return_bin_entries;
DROP FUNCTION guard_customer_return_bin_entry();
DROP VIEW warehouse_customer_return_bin_sources;
DROP TRIGGER warehouse_placement_insert ON warehouse_placements;
ALTER TABLE warehouse_placements DROP CONSTRAINT warehouse_placement_source;
ALTER TABLE warehouse_placements DROP COLUMN return_entry_id;
ALTER TABLE warehouse_placements DROP COLUMN source_kind;
ALTER TABLE warehouse_placements ALTER COLUMN task_id SET NOT NULL;
CREATE TRIGGER warehouse_placement_insert BEFORE INSERT ON warehouse_placements FOR EACH ROW EXECUTE FUNCTION guard_warehouse_placement_insert();
DROP TABLE warehouse_return_bin_entries;
