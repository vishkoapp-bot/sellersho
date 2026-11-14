ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_status_check;
ALTER TABLE shipments ADD CONSTRAINT shipments_status_check CHECK(status IN ('draft','sent','in_transit','received','partially_received','cancelled'));
-- Additive provenance only: historical shipments are never reserved or debited here.
ALTER TABLE inventory_balances DROP CONSTRAINT IF EXISTS inventory_balances_status_check;
ALTER TABLE inventory_balances ADD CONSTRAINT inventory_balances_status_check CHECK (status IN ('physical','sellable','sales_reserved','return_requested','quarantine','in_transit','damaged','unreconciled','inbound_reserved'));
CREATE TABLE inbound_inventory_lines (
 id TEXT PRIMARY KEY,
 shipment_id TEXT NOT NULL,
 supplier_id TEXT NOT NULL,
 warehouse_id TEXT NOT NULL,
 product_id TEXT NOT NULL,
 variant_id TEXT NOT NULL DEFAULT '',
 source_kind TEXT NOT NULL CHECK(source_kind IN ('transfer','external_replenishment')),
 source_warehouse_id TEXT NOT NULL DEFAULT '',
 external_reference TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL DEFAULT '',
 quantity INT NOT NULL CHECK(quantity>0),
 dispatched_quantity INT NOT NULL DEFAULT 0,
 received_quantity INT NOT NULL DEFAULT 0,
 cancelled_quantity INT NOT NULL DEFAULT 0,
 allocations JSONB NOT NULL DEFAULT '[]',
 version INT NOT NULL DEFAULT 1 CHECK(version=1),
 CHECK(dispatched_quantity>=0 AND received_quantity>=0 AND cancelled_quantity>=0 AND received_quantity<=dispatched_quantity AND dispatched_quantity+cancelled_quantity<=quantity),
 CHECK((source_kind='transfer' AND source_warehouse_id<>'' AND external_reference='') OR (source_kind='external_replenishment' AND source_warehouse_id='' AND external_reference<>'' AND reason<>'')),
 UNIQUE(shipment_id,product_id,variant_id)
);
CREATE UNIQUE INDEX inbound_external_reference_unique ON inbound_inventory_lines(external_reference) WHERE external_reference<>'';
CREATE FUNCTION inbound_inventory_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (NEW.id,NEW.shipment_id,NEW.supplier_id,NEW.warehouse_id,NEW.product_id,NEW.variant_id,NEW.source_kind,NEW.source_warehouse_id,NEW.external_reference,NEW.reason,NEW.quantity,NEW.allocations,NEW.version) IS DISTINCT FROM (OLD.id,OLD.shipment_id,OLD.supplier_id,OLD.warehouse_id,OLD.product_id,OLD.variant_id,OLD.source_kind,OLD.source_warehouse_id,OLD.external_reference,OLD.reason,OLD.quantity,OLD.allocations,OLD.version) THEN RAISE EXCEPTION 'immutable inbound provenance'; END IF;
 IF NEW.dispatched_quantity<OLD.dispatched_quantity OR NEW.received_quantity<OLD.received_quantity OR NEW.cancelled_quantity<OLD.cancelled_quantity THEN RAISE EXCEPTION 'inbound counters cannot regress'; END IF;
 RETURN NEW; END $$;
CREATE TRIGGER inbound_inventory_identity BEFORE UPDATE ON inbound_inventory_lines FOR EACH ROW EXECUTE FUNCTION inbound_inventory_identity_immutable();
CREATE VIEW inbound_transfer_reconciliation AS
 SELECT 'carton'::text AS kind,id::text AS shipment_id,status::text,
 CASE WHEN status IN ('received','partially_received') THEN 'historical_receipt_requires_movement_evidence' ELSE 'historical_unreserved_requires_source_reconciliation' END AS exception
 FROM shipments s WHERE NOT EXISTS(SELECT 1 FROM inbound_inventory_lines l WHERE l.shipment_id=s.id::text)
 UNION ALL SELECT 'asn',id::text,status::text,'historical_unallocated_requires_source_reconciliation'
 FROM inbound_shipments s WHERE NOT EXISTS(SELECT 1 FROM inbound_inventory_lines l WHERE l.shipment_id=s.id::text);
