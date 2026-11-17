-- Extend measured placements without modifying their historical snapshots.
CREATE TABLE warehouse_departure_events (
 event_type TEXT NOT NULL CHECK(event_type IN ('warehouse_shipment','supplier_return')),
 event_id TEXT NOT NULL,
 payload_hash TEXT NOT NULL,
 payload_json TEXT NOT NULL CHECK(jsonb_typeof(payload_json::jsonb)='array'),
 actor_user_id TEXT NOT NULL CHECK(actor_user_id<>''),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(event_type,event_id),
 CHECK(payload_hash=encode(sha256(convert_to(payload_json,'UTF8')),'hex'))
);
CREATE TABLE warehouse_occupancy_releases (
 id TEXT PRIMARY KEY,
 event_type TEXT NOT NULL,
 event_id TEXT NOT NULL,
 placement_id TEXT NOT NULL REFERENCES warehouse_placements(id) ON DELETE RESTRICT,
 quantity INT NOT NULL CHECK(quantity>0),
 volume_cm3 BIGINT NOT NULL CHECK(volume_cm3>=0),
 weight_grams BIGINT NOT NULL CHECK(weight_grams>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(event_type,event_id,placement_id),
 FOREIGN KEY(event_type,event_id) REFERENCES warehouse_departure_events(event_type,event_id) ON DELETE RESTRICT
);
CREATE INDEX warehouse_occupancy_releases_placement ON warehouse_occupancy_releases(placement_id,id);
CREATE FUNCTION warehouse_bin_identity(location TEXT,declared_bin TEXT) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
 SELECT CASE WHEN left(k,4)='bin:' THEN substring(k FROM 5) ELSE k END FROM (SELECT COALESCE(NULLIF(declared_bin,''),location,'') AS k) s;
$$;
CREATE FUNCTION expected_warehouse_departure(kind TEXT,business_id TEXT) RETURNS JSONB LANGUAGE SQL STABLE AS $$
 WITH actual AS (
  SELECT b.id AS balance_id,b.product_id,COALESCE(to_jsonb(b)->>'variant_id','') AS variant_id,COALESCE(b.supplier_id,'') AS supplier_id,
   COALESCE(to_jsonb(b)->>'warehouse_id','') AS warehouse_id,warehouse_bin_identity(b.location_key,to_jsonb(b)->>'bin_code') AS bin_code,(a->>'quantity')::bigint AS quantity
  FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a JOIN inventory_balances b ON b.id=a->>'balanceId'
  WHERE kind='warehouse_shipment' AND c.order_id=business_id AND b.location_type='bin' AND b.owner_type='supplier' AND b.product_id=c.product_id AND ((c.inventory_mode='tracked' AND COALESCE(to_jsonb(b)->>'variant_id','')=COALESCE(to_jsonb(c)->>'variant_id','')) OR (c.inventory_mode<>'tracked' AND COALESCE(to_jsonb(b)->>'variant_id','')=''))
  UNION ALL
  SELECT b.id,b.product_id,COALESCE(to_jsonb(b)->>'variant_id',''),COALESCE(b.supplier_id,''),COALESCE(to_jsonb(b)->>'warehouse_id',''),warehouse_bin_identity(b.location_key,to_jsonb(b)->>'bin_code'),ir.quantity
  FROM inventory_reservations ir JOIN inventory_balances b ON b.product_id=ir.product_id AND b.supplier_id=ir.supplier_id AND COALESCE(to_jsonb(b)->>'variant_id','')=COALESCE(to_jsonb(ir)->>'variant_id','') AND b.location_key=ir.location_key AND b.status='return_requested'
  WHERE kind='supplier_return' AND ir.reference_type='supplier_return' AND ir.reference_id=business_id AND ir.status='consumed' AND b.location_type='bin'
 ), grouped AS (
  SELECT balance_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,SUM(quantity) AS quantity FROM actual GROUP BY 1,2,3,4,5,6
 )
 SELECT COALESCE(jsonb_agg(jsonb_build_object('balanceId',balance_id,'productId',product_id,'variantId',variant_id,'supplierId',supplier_id,'warehouseId',warehouse_id,'binCode',bin_code,'quantity',quantity) ORDER BY balance_id COLLATE "C"),'[]'::jsonb) FROM grouped;
$$;
CREATE FUNCTION guard_warehouse_departure_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.event_type='warehouse_shipment' AND NOT EXISTS(SELECT 1 FROM orders o WHERE o.id=NEW.event_id AND o.status='shipped' AND o.payment_status='paid' AND o.shipped_at IS NOT NULL) THEN RAISE EXCEPTION 'physical shipment evidence required'; END IF;
 IF NEW.event_type='supplier_return' AND NOT EXISTS(SELECT 1 FROM supplier_return_requests r WHERE r.id=NEW.event_id AND r.status IN ('packed','pickup_pending','postal_pending','in_transit') AND EXISTS(SELECT 1 FROM inventory_reservations ir WHERE ir.reference_type='supplier_return' AND ir.reference_id=r.id AND ir.status='consumed')) THEN RAISE EXCEPTION 'physical return handover evidence required'; END IF;
 IF NEW.payload_json::jsonb IS DISTINCT FROM expected_warehouse_departure(NEW.event_type,NEW.event_id) THEN RAISE EXCEPTION 'departure allocations do not match captured or handed-over lots'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER warehouse_departure_insert BEFORE INSERT ON warehouse_departure_events FOR EACH ROW EXECUTE FUNCTION guard_warehouse_departure_insert();
CREATE FUNCTION guard_warehouse_release_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p warehouse_placements%ROWTYPE; m inventory_measurement_revisions%ROWTYPE; departed BIGINT; left_units BIGINT; before_volume NUMERIC; after_volume NUMERIC; allowed NUMERIC; event_released NUMERIC; recorded_q NUMERIC; recorded_v NUMERIC; recorded_w NUMERIC; known_q NUMERIC; known_v NUMERIC; known_w NUMERIC; bin_status TEXT;
BEGIN
 -- Acquire the physical bin before its placement, including direct SQL writers.
 PERFORM 1 FROM warehouse_bins b JOIN warehouse_placements wp ON wp.bin_code=b.bin_code WHERE wp.id=NEW.placement_id FOR UPDATE OF b;
 SELECT * INTO p FROM warehouse_placements WHERE id=NEW.placement_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'measured placement missing'; END IF;
 IF EXISTS(SELECT 1 FROM warehouse_occupancy_exceptions x WHERE x.bin_code=p.bin_code) THEN RAISE EXCEPTION 'unresolved physical occupancy cannot be released'; END IF;
 SELECT capacity_status,quantity,occupied_volume_cm3,occupied_weight_grams INTO bin_status,recorded_q,recorded_v,recorded_w FROM warehouse_bins WHERE bin_code=p.bin_code;
 SELECT COALESCE(SUM(remaining_quantity),0),COALESCE(SUM(remaining_volume_cm3),0),COALESCE(SUM(remaining_weight_grams),0) INTO known_q,known_v,known_w FROM warehouse_remaining_placements WHERE bin_code=p.bin_code;
 IF bin_status IS DISTINCT FROM 'verified' OR (recorded_q,recorded_v,recorded_w) IS DISTINCT FROM (known_q,known_v,known_w) THEN RAISE EXCEPTION 'verified physical counter baseline required'; END IF;
 SELECT * INTO m FROM inventory_measurement_revisions WHERE id=p.measurement_revision_id AND verified;
 IF NOT FOUND THEN RAISE EXCEPTION 'archived physical measurement missing'; END IF;
 SELECT COALESCE(SUM(quantity),0) INTO departed FROM warehouse_occupancy_releases WHERE placement_id=p.id;
 left_units:=p.quantity-departed;
 IF NEW.quantity>left_units THEN RAISE EXCEPTION 'departure exceeds remaining placed units'; END IF;
 before_volume:=ceil(m.length_mm::numeric*m.width_mm*m.height_mm*left_units/1000);
 after_volume:=ceil(m.length_mm::numeric*m.width_mm*m.height_mm*(left_units-NEW.quantity)/1000);
 IF NEW.volume_cm3<>before_volume-after_volume OR NEW.weight_grams<>m.weight_grams::numeric*NEW.quantity THEN RAISE EXCEPTION 'departure physical snapshot mismatch'; END IF;
 SELECT COALESCE(SUM((a->>'quantity')::numeric),0) INTO allowed FROM warehouse_departure_events e CROSS JOIN LATERAL jsonb_array_elements(e.payload_json::jsonb) a WHERE e.event_type=NEW.event_type AND e.event_id=NEW.event_id AND a->>'productId'=p.product_id AND a->>'variantId'=p.variant_id AND a->>'supplierId'=p.supplier_id AND a->>'warehouseId'=p.warehouse_id AND a->>'binCode'=p.bin_code;
 SELECT COALESCE(SUM(r.quantity),0) INTO event_released FROM warehouse_occupancy_releases r JOIN warehouse_placements wp ON wp.id=r.placement_id WHERE r.event_type=NEW.event_type AND r.event_id=NEW.event_id AND (wp.product_id,wp.variant_id,wp.supplier_id,wp.warehouse_id,wp.bin_code)=(p.product_id,p.variant_id,p.supplier_id,p.warehouse_id,p.bin_code);
 IF event_released+NEW.quantity>allowed THEN RAISE EXCEPTION 'departure exceeds exact business allocations'; END IF;
 UPDATE warehouse_bins SET quantity=quantity-NEW.quantity,occupied_volume_cm3=occupied_volume_cm3-NEW.volume_cm3,occupied_weight_grams=occupied_weight_grams-NEW.weight_grams,updated_at=NOW() WHERE bin_code=p.bin_code AND warehouse_id=p.warehouse_id AND quantity>=NEW.quantity AND occupied_volume_cm3>=NEW.volume_cm3 AND occupied_weight_grams>=NEW.weight_grams;
 IF NOT FOUND THEN RAISE EXCEPTION 'physical bin counters require reconciliation'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER warehouse_release_insert BEFORE INSERT ON warehouse_occupancy_releases FOR EACH ROW EXECUTE FUNCTION guard_warehouse_release_insert();
CREATE TABLE warehouse_occupancy_exceptions (
 bin_code TEXT PRIMARY KEY,
 reason TEXT NOT NULL,
 event_type TEXT NOT NULL,
 event_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE warehouse_departure_unresolved_sources (
 event_type TEXT NOT NULL,event_id TEXT NOT NULL,balance_id TEXT NOT NULL,bin_code TEXT NOT NULL,reason TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(event_type,event_id,balance_id),
 FOREIGN KEY(event_type,event_id) REFERENCES warehouse_departure_events(event_type,event_id) ON DELETE RESTRICT
);
CREATE FUNCTION immutable_warehouse_departure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'immutable warehouse departure evidence'; END $$;
CREATE TRIGGER warehouse_departure_identity BEFORE UPDATE OR DELETE ON warehouse_departure_events FOR EACH ROW EXECUTE FUNCTION immutable_warehouse_departure();
CREATE TRIGGER warehouse_release_identity BEFORE UPDATE OR DELETE ON warehouse_occupancy_releases FOR EACH ROW EXECUTE FUNCTION immutable_warehouse_departure();
CREATE TRIGGER warehouse_occupancy_exception_identity BEFORE UPDATE OR DELETE ON warehouse_occupancy_exceptions FOR EACH ROW EXECUTE FUNCTION immutable_warehouse_departure();
CREATE TRIGGER warehouse_unresolved_source_identity BEFORE UPDATE OR DELETE ON warehouse_departure_unresolved_sources FOR EACH ROW EXECUTE FUNCTION immutable_warehouse_departure();
CREATE VIEW warehouse_remaining_placements AS
 SELECT p.*,p.quantity-COALESCE(r.quantity,0) AS remaining_quantity,
 p.volume_cm3-COALESCE(r.volume_cm3,0) AS remaining_volume_cm3,
 p.weight_grams-COALESCE(r.weight_grams,0) AS remaining_weight_grams
 FROM warehouse_placements p LEFT JOIN (
 SELECT placement_id,SUM(quantity) AS quantity,SUM(volume_cm3) AS volume_cm3,SUM(weight_grams) AS weight_grams
 FROM warehouse_occupancy_releases GROUP BY placement_id) r ON r.placement_id=p.id;

-- Audit physical custody, including paid units still held in the bin. Never
-- rebuild historical counters from sellable availability or today's dimensions.
CREATE VIEW warehouse_occupancy_audit AS
 WITH measured AS (
  SELECT bin_code,SUM(remaining_quantity) AS quantity,SUM(remaining_volume_cm3) AS volume,SUM(remaining_weight_grams) AS weight FROM warehouse_remaining_placements GROUP BY bin_code
 ), held AS (
  SELECT warehouse_bin_identity(location_key,to_jsonb(inventory_balances)->>'bin_code') AS bin_code,SUM(quantity) AS quantity FROM inventory_balances WHERE location_type='bin' AND status IN ('sellable','return_requested','quarantine','qc_accepted','allocated','reserved') GROUP BY 1
 ), captured AS (
  SELECT warehouse_bin_identity(b.location_key,to_jsonb(b)->>'bin_code') AS bin_code,(a->>'quantity')::numeric AS quantity,c.restored_quantity,c.quantity AS captured_quantity,o.payment_status,o.status,o.shipped_at,c.order_id
  FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a JOIN inventory_balances b ON b.id=a->>'balanceId' JOIN orders o ON o.id=c.order_id WHERE b.location_type='bin'
 ), capture_totals AS (
  SELECT bin_code,COALESCE(SUM(quantity) FILTER(WHERE payment_status='paid' AND shipped_at IS NULL AND status NOT IN ('cancelled','shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed') AND restored_quantity=0),0) AS pending_quantity,
   COUNT(*) FILTER(WHERE restored_quantity>0 AND restored_quantity<captured_quantity) AS partial_restore_count,
   COUNT(*) FILTER(WHERE (shipped_at IS NOT NULL OR status IN ('shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed')) AND NOT EXISTS(SELECT 1 FROM warehouse_departure_events e WHERE e.event_type='warehouse_shipment' AND e.event_id=captured.order_id)) AS historical_departures
  FROM captured GROUP BY bin_code
 ), return_history AS (
  SELECT warehouse_bin_identity(b.location_key,to_jsonb(b)->>'bin_code') AS bin_code,COUNT(*) AS historical_handovers FROM inventory_reservations ir JOIN inventory_balances b ON b.product_id=ir.product_id AND b.supplier_id=ir.supplier_id AND b.location_key=ir.location_key AND COALESCE(to_jsonb(b)->>'variant_id','')=COALESCE(to_jsonb(ir)->>'variant_id','') AND b.status='return_requested' AND b.location_type='bin' WHERE ir.reference_type='supplier_return' AND ir.status='consumed' AND NOT EXISTS(SELECT 1 FROM warehouse_departure_events e WHERE e.event_type='supplier_return' AND e.event_id=ir.reference_id) GROUP BY 1
 ), event_units AS (
  SELECT e.event_type,e.event_id,a->>'binCode' AS bin_code,SUM((a->>'quantity')::numeric) AS quantity FROM warehouse_departure_events e CROSS JOIN LATERAL jsonb_array_elements(e.payload_json::jsonb) a GROUP BY 1,2,3
 ), released_units AS (
  SELECT r.event_type,r.event_id,p.bin_code,SUM(r.quantity) AS quantity FROM warehouse_occupancy_releases r JOIN warehouse_placements p ON p.id=r.placement_id GROUP BY 1,2,3
 ), incomplete AS (
  SELECT e.bin_code,COUNT(*) AS incomplete_departures FROM event_units e LEFT JOIN released_units r USING(event_type,event_id,bin_code) WHERE e.quantity<>COALESCE(r.quantity,0) GROUP BY e.bin_code
 ), evidence AS (
  SELECT b.bin_code,b.warehouse_id,b.capacity_status,b.quantity AS recorded_quantity,b.occupied_volume_cm3 AS recorded_volume_cm3,b.occupied_weight_grams AS recorded_weight_grams,
   COALESCE(m.quantity,0) AS measured_quantity,COALESCE(m.volume,0) AS measured_volume_cm3,COALESCE(m.weight,0) AS measured_weight_grams,
   COALESCE(h.quantity,0) AS held_balance_quantity,COALESCE(c.pending_quantity,0) AS paid_unshipped_quantity,
   COALESCE(c.partial_restore_count,0) AS partial_restore_count,COALESCE(c.historical_departures,0) AS historical_departures,
   COALESCE(r.historical_handovers,0) AS historical_handovers,COALESCE(i.incomplete_departures,0) AS incomplete_departures,
   COALESCE(x.reason,'') AS persisted_exception
  FROM warehouse_bins b LEFT JOIN measured m USING(bin_code) LEFT JOIN held h USING(bin_code) LEFT JOIN capture_totals c USING(bin_code) LEFT JOIN return_history r USING(bin_code) LEFT JOIN incomplete i USING(bin_code) LEFT JOIN warehouse_occupancy_exceptions x USING(bin_code)
 )
 SELECT *,recorded_quantity<>measured_quantity OR recorded_volume_cm3<>measured_volume_cm3 OR recorded_weight_grams<>measured_weight_grams OR measured_quantity<>held_balance_quantity+paid_unshipped_quantity OR partial_restore_count>0 OR historical_departures>0 OR historical_handovers>0 OR incomplete_departures>0 OR persisted_exception<>'' AS requires_reconciliation FROM evidence;
CREATE VIEW warehouse_occupancy_orphan_allocations AS
 SELECT c.order_id,c.order_item_id,c.product_id,a->>'balanceId' AS missing_balance_id,a->>'quantity' AS recorded_quantity
 FROM order_inventory_captures c CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a LEFT JOIN inventory_balances b ON b.id=a->>'balanceId' WHERE b.id IS NULL;
INSERT INTO warehouse_occupancy_exceptions(bin_code,reason,event_type,event_id)
 SELECT bin_code,CASE WHEN historical_departures>0 OR historical_handovers>0 THEN 'historical_departure_without_measured_release' ELSE 'historical_physical_counter_requires_count' END,'migration','202611150001_warehouse_occupancy'
 FROM warehouse_occupancy_audit WHERE requires_reconciliation;
UPDATE warehouse_bins b SET capacity_status='disputed' FROM warehouse_occupancy_exceptions x WHERE x.bin_code=b.bin_code;
CREATE FUNCTION guard_disputed_warehouse_capacity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.capacity_status='verified' AND EXISTS(SELECT 1 FROM warehouse_occupancy_exceptions x WHERE x.bin_code=NEW.bin_code) THEN RAISE EXCEPTION 'counted occupancy reconciliation required before bin verification'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER disputed_warehouse_capacity BEFORE UPDATE OF capacity_status ON warehouse_bins FOR EACH ROW EXECUTE FUNCTION guard_disputed_warehouse_capacity();
