DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM inbound_putaway_allocations) THEN RAISE EXCEPTION 'cannot remove exact putaway provenance after use'; END IF;
END $$;
DROP VIEW inbound_putaway_reconciliation;
DROP TRIGGER inbound_putaway_task_identity ON putaway_tasks;
DROP FUNCTION inbound_putaway_task_guard();
DROP TRIGGER inbound_putaway_allocation_identity ON inbound_putaway_allocations;
DROP FUNCTION inbound_putaway_allocation_immutable();
DROP TABLE inbound_putaway_allocations;
