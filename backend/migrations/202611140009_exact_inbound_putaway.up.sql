-- New acceptance allocations only. Historical staging/captures are never moved.
CREATE TABLE inbound_putaway_allocations (
 task_id TEXT PRIMARY KEY,
 disposition_id TEXT NOT NULL UNIQUE,
 receipt_line_id TEXT NOT NULL,
 shipment_id TEXT NOT NULL,
 supplier_id TEXT NOT NULL,
 warehouse_id TEXT NOT NULL,
 product_id TEXT NOT NULL,
 variant_id TEXT NOT NULL DEFAULT '',
 source_location_key TEXT NOT NULL UNIQUE,
 quantity INT NOT NULL CHECK(quantity>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE FUNCTION inbound_putaway_allocation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'immutable inbound putaway allocation';
END $$;
CREATE TRIGGER inbound_putaway_allocation_identity BEFORE UPDATE OR DELETE ON inbound_putaway_allocations FOR EACH ROW EXECUTE FUNCTION inbound_putaway_allocation_immutable();
CREATE FUNCTION inbound_putaway_task_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF EXISTS(SELECT 1 FROM inbound_putaway_allocations a WHERE a.task_id=OLD.id) THEN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'allocated putaway task cannot be deleted'; END IF;
  IF (NEW.id,NEW.product_id,NEW.variant_id,NEW.inbound_id,NEW.warehouse_id,NEW.inventory_source_location_key,NEW.quantity) IS DISTINCT FROM (OLD.id,OLD.product_id,OLD.variant_id,OLD.inbound_id,OLD.warehouse_id,OLD.inventory_source_location_key,OLD.quantity) THEN
   RAISE EXCEPTION 'immutable putaway task identity';
  END IF;
  IF NEW.status='completed' AND OLD.status<>'completed' AND NOT EXISTS(SELECT 1 FROM inventory_movements m JOIN inbound_putaway_allocations a ON a.task_id=NEW.id WHERE m.idempotency_key='putaway:'||NEW.id AND m.quantity=a.quantity AND m.from_location_key=a.source_location_key AND m.from_status='qc_accepted' AND m.to_status='sellable') THEN
   RAISE EXCEPTION 'putaway completion requires exact acceptance movement';
  END IF;
  IF OLD.status='completed' AND NEW.status<>'completed' THEN RAISE EXCEPTION 'completed putaway cannot reopen'; END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER inbound_putaway_task_identity BEFORE UPDATE OR DELETE ON putaway_tasks FOR EACH ROW EXECUTE FUNCTION inbound_putaway_task_guard();
CREATE VIEW inbound_putaway_reconciliation AS
 SELECT pt.id AS task_id,pt.product_id,pt.quantity,'historical_task_without_exact_acceptance'::text AS exception
 FROM putaway_tasks pt WHERE pt.status IN ('pending','assigned') AND NOT EXISTS(SELECT 1 FROM inbound_putaway_allocations a WHERE a.task_id=pt.id)
 UNION ALL
 SELECT d.id,l.product_id,d.passed_delta,'accepted_history_without_exact_task'::text
 FROM inbound_qc_dispositions d JOIN inbound_inventory_lines l ON l.id=d.receipt_line_id
 WHERE d.passed_delta>0 AND NOT EXISTS(SELECT 1 FROM inbound_putaway_allocations a WHERE a.disposition_id=d.id);
