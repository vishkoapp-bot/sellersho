-- Preserve every pre-existing position, charge, timestamp and financial link.
-- A missing receipt lot is explicit unknown history, never invented old debt.
CREATE TABLE warehouse_storage_history_policy (
 id INT PRIMARY KEY CHECK(id=1),
 version TEXT NOT NULL CHECK(version='storage_calendar_v2'),
 effective_at TIMESTAMPTZ NOT NULL
);
INSERT INTO warehouse_storage_history_policy(id,version,effective_at) VALUES(1,'storage_calendar_v2',clock_timestamp());
CREATE TABLE warehouse_storage_receipt_lots (
 id TEXT PRIMARY KEY REFERENCES warehouse_storage_positions(id) ON DELETE RESTRICT,
 source_kind TEXT NOT NULL CHECK(source_kind IN ('inbound_qc','customer_return','warehouse_transfer')),
 source_event_id TEXT NOT NULL CHECK(source_event_id<>''),
 receipt_line_id TEXT NOT NULL DEFAULT '',
 receipt_movement_id TEXT NOT NULL REFERENCES inventory_movements(id) ON DELETE RESTRICT,
 source_balance_id TEXT NOT NULL REFERENCES inventory_balances(id) ON DELETE RESTRICT,
 product_id TEXT NOT NULL,
 variant_id TEXT NOT NULL DEFAULT '',
 supplier_id TEXT NOT NULL,
 warehouse_id TEXT NOT NULL,
 initial_quantity INT NOT NULL CHECK(initial_quantity>0),
 physical_received_at TIMESTAMPTZ NOT NULL,
 billing_start_at TIMESTAMPTZ NOT NULL,
 held_from TIMESTAMPTZ NOT NULL,
 measurement_revision_id TEXT REFERENCES inventory_measurement_revisions(id) ON DELETE RESTRICT,
 length_mm INT,width_mm INT,height_mm INT,weight_grams INT,
 free_days INT NOT NULL CHECK(free_days BETWEEN 0 AND 36500),
 per_kg_day_toman BIGINT NOT NULL CHECK(per_kg_day_toman BETWEEN 0 AND 9007199254740991),
 per_m3_day_toman BIGINT NOT NULL CHECK(per_m3_day_toman BETWEEN 0 AND 9007199254740991),
 unit_day_toman BIGINT CHECK(unit_day_toman BETWEEN 0 AND 9007199254740991),
 policy_snapshot JSONB NOT NULL,
 policy_version TEXT NOT NULL DEFAULT 'storage_calendar_v2' CHECK(policy_version='storage_calendar_v2'),
 history_status TEXT NOT NULL CHECK(history_status IN ('verified','unresolved_measurement','unresolved_policy')),
 unresolved_reason TEXT NOT NULL DEFAULT '',
 parent_lot_id TEXT REFERENCES warehouse_storage_receipt_lots(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(source_kind,source_event_id,receipt_movement_id),
 CHECK(physical_received_at<=held_from AND billing_start_at<=held_from),
 CHECK(history_status<>'verified' OR (measurement_revision_id IS NOT NULL AND unit_day_toman IS NOT NULL
  AND length_mm IS NOT NULL AND width_mm IS NOT NULL AND height_mm IS NOT NULL AND weight_grams IS NOT NULL
  AND length_mm BETWEEN 1 AND 1000000 AND width_mm BETWEEN 1 AND 1000000 AND height_mm BETWEEN 1 AND 1000000 AND weight_grams BETWEEN 1 AND 1000000000))
);
CREATE INDEX warehouse_storage_lots_receipt ON warehouse_storage_receipt_lots(receipt_line_id,source_event_id,id);
CREATE INDEX warehouse_storage_lots_source ON warehouse_storage_receipt_lots(source_balance_id,id);
CREATE INDEX warehouse_storage_lots_owner ON warehouse_storage_receipt_lots(supplier_id,warehouse_id,product_id,variant_id,id);
CREATE TABLE warehouse_storage_lot_departures (
 id TEXT PRIMARY KEY,
 ordinal BIGSERIAL NOT NULL UNIQUE,
 position_id TEXT NOT NULL REFERENCES warehouse_storage_positions(id) ON DELETE RESTRICT,
 event_type TEXT NOT NULL CHECK(event_type IN ('warehouse_shipment','supplier_return','warehouse_transfer')),
 event_id TEXT NOT NULL CHECK(event_id<>''),
 source_balance_id TEXT NOT NULL REFERENCES inventory_balances(id) ON DELETE RESTRICT,
 placement_id TEXT NOT NULL DEFAULT '',
 physical_variant_id TEXT NOT NULL DEFAULT '',
 quantity INT NOT NULL CHECK(quantity>0),
 remaining_quantity_after INT NOT NULL CHECK(remaining_quantity_after>=0),
 effective_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 actor_user_id TEXT NOT NULL CHECK(length(btrim(actor_user_id))>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(position_id,event_type,event_id,source_balance_id,placement_id)
);
CREATE INDEX warehouse_storage_departures_position_time ON warehouse_storage_lot_departures(position_id,effective_at DESC,ordinal DESC) INCLUDE(remaining_quantity_after);
CREATE INDEX warehouse_storage_departures_event ON warehouse_storage_lot_departures(event_type,event_id,id);
CREATE INDEX warehouse_storage_departures_placement ON warehouse_storage_lot_departures(placement_id,position_id) INCLUDE(quantity);
CREATE TABLE warehouse_storage_placement_lots (
 placement_id TEXT NOT NULL REFERENCES warehouse_placements(id) ON DELETE RESTRICT,
 position_id TEXT NOT NULL REFERENCES warehouse_storage_receipt_lots(id) ON DELETE RESTRICT,
 quantity INT NOT NULL CHECK(quantity>0),
 PRIMARY KEY(placement_id,position_id)
);
CREATE INDEX warehouse_storage_placement_position ON warehouse_storage_placement_lots(position_id,placement_id);
-- The platform-settings guard serializes cursor and assessment transactions.
CREATE TABLE warehouse_storage_assessment_cursor(id INT PRIMARY KEY CHECK(id=1),last_receipt_id TEXT NOT NULL DEFAULT '',cycle_pending BOOLEAN NOT NULL DEFAULT FALSE);
INSERT INTO warehouse_storage_assessment_cursor(id) VALUES(1);
CREATE INDEX warehouse_storage_lots_frontier ON warehouse_storage_receipt_lots(id COLLATE "C");
CREATE INDEX warehouse_storage_verified_assessment ON warehouse_storage_receipt_lots(history_status,id);
CREATE INDEX warehouse_storage_position_assessment ON warehouse_storage_positions(last_assessed_date,id);
CREATE TABLE warehouse_storage_certified_charges (
 charge_id TEXT PRIMARY KEY REFERENCES warehouse_storage_fee_charges(id) ON DELETE RESTRICT,
 position_id TEXT NOT NULL REFERENCES warehouse_storage_receipt_lots(id) ON DELETE RESTRICT,
 ordinal BIGSERIAL NOT NULL UNIQUE,
 cumulative_total_toman BIGINT NOT NULL CHECK(cumulative_total_toman BETWEEN 0 AND 9007199254740991)
);
CREATE INDEX warehouse_storage_certified_charge_position ON warehouse_storage_certified_charges(position_id,ordinal DESC) INCLUDE(cumulative_total_toman);
CREATE FUNCTION immutable_storage_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'immutable storage receipt and quantity history';
END $$;
CREATE TRIGGER storage_receipt_lot_identity BEFORE UPDATE OR DELETE ON warehouse_storage_receipt_lots FOR EACH ROW EXECUTE FUNCTION immutable_storage_history();
CREATE TRIGGER storage_departure_identity BEFORE UPDATE OR DELETE ON warehouse_storage_lot_departures FOR EACH ROW EXECUTE FUNCTION immutable_storage_history();
CREATE TRIGGER storage_placement_lot_identity BEFORE UPDATE OR DELETE ON warehouse_storage_placement_lots FOR EACH ROW EXECUTE FUNCTION immutable_storage_history();
CREATE TRIGGER storage_certified_charge_identity BEFORE UPDATE OR DELETE ON warehouse_storage_certified_charges FOR EACH ROW EXECUTE FUNCTION immutable_storage_history();
CREATE FUNCTION guard_storage_policy_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots) THEN RAISE EXCEPTION 'cannot rewrite the effective policy of retained storage receipts'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_history_policy_identity BEFORE UPDATE OR DELETE ON warehouse_storage_history_policy FOR EACH ROW EXECUTE FUNCTION guard_storage_policy_identity();

CREATE FUNCTION guard_storage_receipt_lot_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s RECORD; m RECORD; p warehouse_storage_positions%ROWTYPE; cfg RECORD; expected_unit NUMERIC;
BEGIN
 IF NEW.source_kind='inbound_qc' THEN
 SELECT l.id::text AS line_id,l.product_id::text AS product_id,l.variant_id::text AS variant_id,l.supplier_id::text AS supplier_id,l.warehouse_id::text AS warehouse_id,b.id::text AS balance_id,
  d.created_at AS billing_at,r.created_at AS physical_at,
  GREATEST(LEAST(r.upper_units,d.cumulative_disposed-d.failed_delta)-GREATEST(r.lower_units,d.cumulative_disposed-d.failed_delta-d.passed_delta),0)::numeric AS units
 INTO s
 FROM inbound_qc_dispositions d JOIN inbound_inventory_lines l ON l.id=d.receipt_line_id
 JOIN inventory_balances b ON b.product_id=l.product_id AND COALESCE(b.variant_id::text,'')=l.variant_id AND b.supplier_id=l.supplier_id AND b.warehouse_id=l.warehouse_id AND b.owner_type='supplier' AND b.location_type='warehouse' AND b.status='qc_accepted' AND b.location_key='qc-accepted:'||d.id AND b.quantity>=d.passed_delta
 CROSS JOIN LATERAL (
  SELECT id,created_at,SUM(quantity) OVER(ORDER BY created_at,id COLLATE "C") AS upper_units,
   COALESCE(SUM(quantity) OVER(ORDER BY created_at,id COLLATE "C" ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING),0) AS lower_units,
   SUM(quantity) OVER() AS total_units
  FROM inventory_movements WHERE reference_type='inbound_transfer' AND reference_id=l.id AND to_status='quarantine' AND to_location_key='inbound-line:'||l.id AND product_id=l.product_id AND supplier_id=l.supplier_id AND COALESCE(variant_id::text,'')=l.variant_id
 ) r
 WHERE d.id=NEW.source_event_id AND r.id=NEW.receipt_movement_id AND r.total_units>=d.received_quantity AND l.received_quantity>=d.received_quantity
  AND d.created_at>=(SELECT effective_at FROM warehouse_storage_history_policy WHERE id=1);
 IF NOT FOUND OR s.units<=0 THEN RAISE EXCEPTION 'known post-cutover QC receipt source required'; END IF;
 ELSIF NEW.source_kind='customer_return' THEN
  SELECT r.return_item_id::text AS line_id,r.product_id::text AS product_id,r.variant_id::text AS variant_id,r.supplier_id::text AS supplier_id,r.warehouse_id::text AS warehouse_id,r.source_balance_id::text AS balance_id,r.received_at AS billing_at,receipt.created_at AS physical_at,r.qty::numeric AS units INTO s
  FROM warehouse_customer_return_bin_sources r JOIN inventory_movements receipt ON receipt.id=NEW.receipt_movement_id
   AND receipt.idempotency_key=CASE WHEN r.variant_id<>'' THEN 'sku-return:'||r.return_item_id::text ELSE 'crstk:'||r.return_id::text||':'||r.return_item_id::text||':sellable' END
  WHERE r.return_item_id::text=NEW.source_event_id AND r.source_proven AND r.available_quantity=r.qty
   AND EXISTS(SELECT 1 FROM return_items ri JOIN order_inventory_captures c ON c.order_item_id=ri.order_item_id WHERE ri.id=r.return_item_id)
   AND r.received_at>=(SELECT effective_at FROM warehouse_storage_history_policy WHERE id=1);
  IF NOT FOUND THEN RAISE EXCEPTION 'known post-cutover customer return receipt required'; END IF;
 ELSE RAISE EXCEPTION 'storage receipt source requires its supported physical event';
 END IF;
 IF (NEW.receipt_line_id,NEW.product_id,NEW.variant_id,NEW.supplier_id,NEW.warehouse_id,NEW.source_balance_id,NEW.initial_quantity::numeric,NEW.physical_received_at,NEW.billing_start_at,NEW.held_from,NEW.parent_lot_id)
  IS DISTINCT FROM (s.line_id,s.product_id,s.variant_id,s.supplier_id,s.warehouse_id,s.balance_id,s.units,s.physical_at,s.billing_at,s.billing_at,NULL::text)
 THEN RAISE EXCEPTION 'storage receipt quantity, physical arrival or QC source mismatch'; END IF;
 SELECT * INTO p FROM warehouse_storage_positions WHERE id=NEW.id;
 IF NOT FOUND OR p.closed_at IS NOT NULL OR p.last_fee_date IS NOT NULL OR p.last_assessed_date IS NOT NULL OR p.total_fee_charged_toman<>0 OR EXISTS(SELECT 1 FROM warehouse_storage_fee_charges WHERE position_id=NEW.id) OR (p.product_id,p.supplier_id,p.warehouse_id,p.qty,p.stocked_at,p.original_stocked_at,p.goods_class)
  IS DISTINCT FROM (NEW.product_id,NEW.supplier_id,NEW.warehouse_id,NEW.initial_quantity,NEW.billing_start_at,NEW.billing_start_at,(CASE WHEN NEW.source_kind='customer_return' THEN 'returned' ELSE 'normal' END)::varchar)
 THEN RAISE EXCEPTION 'storage position must retain exact receipt identity and initial quantity'; END IF;
 SELECT * INTO cfg FROM platform_settings WHERE id=1;
 NEW.policy_snapshot:=jsonb_build_object('freeDays',cfg.storage_fee_free_days,'perKgDay',cfg.storage_fee_per_kg_day,'perM3Day',cfg.storage_fee_per_m3_day);
 SELECT * INTO m FROM inventory_effective_measurements WHERE product_id=NEW.product_id AND variant_id=NEW.variant_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'storage physical identity missing'; END IF;
 IF m.verified THEN expected_unit:=floor((m.weight_grams::numeric*cfg.storage_fee_per_kg_day*1000000+m.length_mm::numeric*m.width_mm*m.height_mm*cfg.storage_fee_per_m3_day)/1000000000+0.5); END IF;
 IF cfg.storage_fee_free_days NOT BETWEEN 0 AND 36500 OR cfg.storage_fee_per_kg_day NOT BETWEEN 0 AND 9007199254740991 OR cfg.storage_fee_per_m3_day NOT BETWEEN 0 AND 9007199254740991 OR expected_unit>9007199254740991 THEN
  IF NEW.history_status<>'unresolved_policy' OR NEW.free_days<>0 OR NEW.per_kg_day_toman<>0 OR NEW.per_m3_day_toman<>0 OR NEW.measurement_revision_id IS NOT NULL OR NEW.unit_day_toman IS NOT NULL OR NEW.length_mm IS NOT NULL OR NEW.width_mm IS NOT NULL OR NEW.height_mm IS NOT NULL OR NEW.weight_grams IS NOT NULL THEN RAISE EXCEPTION 'invalid receipt-time policy must remain unbillable with its raw snapshot'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.free_days,NEW.per_kg_day_toman,NEW.per_m3_day_toman) IS DISTINCT FROM (cfg.storage_fee_free_days,cfg.storage_fee_per_kg_day,cfg.storage_fee_per_m3_day) THEN RAISE EXCEPTION 'storage receipt policy snapshot mismatch'; END IF;
 IF m.verified THEN
  IF NEW.history_status<>'verified' OR (NEW.measurement_revision_id,NEW.length_mm,NEW.width_mm,NEW.height_mm,NEW.weight_grams,NEW.unit_day_toman::numeric)
   IS DISTINCT FROM (m.revision,m.length_mm,m.width_mm,m.height_mm,m.weight_grams,expected_unit)
   OR NOT EXISTS(SELECT 1 FROM inventory_measurement_revisions r WHERE r.id=m.revision AND r.created_at<=NEW.billing_start_at)
  THEN RAISE EXCEPTION 'storage requires the actual verified receipt-time physical and integer rate snapshot'; END IF;
 ELSE
  IF NEW.history_status<>'unresolved_measurement' OR NEW.measurement_revision_id IS NOT NULL OR NEW.unit_day_toman IS NOT NULL OR NEW.length_mm IS NOT NULL OR NEW.width_mm IS NOT NULL OR NEW.height_mm IS NOT NULL OR NEW.weight_grams IS NOT NULL THEN RAISE EXCEPTION 'unverified receipt cannot invent physical debt'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_receipt_lot_insert BEFORE INSERT ON warehouse_storage_receipt_lots FOR EACH ROW EXECUTE FUNCTION guard_storage_receipt_lot_insert();

CREATE FUNCTION guard_storage_placement_lot() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p warehouse_placements%ROWTYPE; l warehouse_storage_receipt_lots%ROWTYPE; expected_event TEXT; expected_kind TEXT; used NUMERIC; used_placement NUMERIC; floor_departed NUMERIC;
BEGIN
 SELECT * INTO p FROM warehouse_placements WHERE id=NEW.placement_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'storage placement missing'; END IF;
 PERFORM 1 FROM warehouse_storage_positions WHERE id=NEW.position_id FOR UPDATE;
 SELECT * INTO l FROM warehouse_storage_receipt_lots WHERE id=NEW.position_id;
 IF NOT FOUND OR (l.product_id,l.variant_id,l.supplier_id,l.warehouse_id) IS DISTINCT FROM (p.product_id,p.variant_id,p.supplier_id,p.warehouse_id) THEN RAISE EXCEPTION 'storage placement source identity mismatch'; END IF;
 IF p.source_kind='inbound_putaway' THEN
  SELECT disposition_id INTO expected_event FROM inbound_putaway_allocations WHERE task_id=p.task_id;
  expected_kind:='inbound_qc';
 ELSE
  SELECT return_item_id::text INTO expected_event FROM warehouse_return_bin_entries WHERE id=p.return_entry_id;
  expected_kind:='customer_return';
 END IF;
 IF (l.source_kind,l.source_event_id) IS DISTINCT FROM (expected_kind,expected_event) THEN RAISE EXCEPTION 'storage placement receipt mismatch'; END IF;
 SELECT COALESCE(SUM(quantity),0) INTO used FROM warehouse_storage_placement_lots WHERE position_id=l.id;
 SELECT COALESCE(SUM(quantity),0) INTO used_placement FROM warehouse_storage_placement_lots WHERE placement_id=p.id;
 SELECT COALESCE(SUM(quantity),0) INTO floor_departed FROM warehouse_storage_lot_departures WHERE position_id=l.id AND placement_id='';
 IF used+floor_departed+NEW.quantity>l.initial_quantity OR used_placement+NEW.quantity>p.quantity THEN RAISE EXCEPTION 'storage placement exceeds unplaced receipt units'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_placement_lot_insert BEFORE INSERT ON warehouse_storage_placement_lots FOR EACH ROW EXECUTE FUNCTION guard_storage_placement_lot();

CREATE FUNCTION link_storage_placement_lots() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected_event TEXT; expected_kind TEXT; available NUMERIC; needed INT; take INT; l RECORD; found_receipts BOOLEAN:=FALSE;
BEGIN
 IF NEW.source_kind='inbound_putaway' THEN
  SELECT disposition_id INTO expected_event FROM inbound_putaway_allocations WHERE task_id=NEW.task_id;
  expected_kind:='inbound_qc';
 ELSE
  SELECT return_item_id::text INTO expected_event FROM warehouse_return_bin_entries WHERE id=NEW.return_entry_id;
  expected_kind:='customer_return';
 END IF;
 PERFORM 1 FROM warehouse_storage_positions p JOIN warehouse_storage_receipt_lots r ON r.id=p.id WHERE r.source_kind=expected_kind AND r.source_event_id=expected_event ORDER BY p.id COLLATE "C" FOR UPDATE OF p;
 needed:=NEW.quantity;
 FOR l IN SELECT * FROM warehouse_storage_receipt_lots WHERE source_kind=expected_kind AND source_event_id=expected_event ORDER BY physical_received_at,id COLLATE "C" LOOP
  found_receipts:=TRUE;
  SELECT l.initial_quantity-COALESCE((SELECT SUM(quantity) FROM warehouse_storage_placement_lots WHERE position_id=l.id),0)-COALESCE((SELECT SUM(quantity) FROM warehouse_storage_lot_departures WHERE position_id=l.id AND placement_id=''),0) INTO available;
  take:=LEAST(needed,GREATEST(available,0));
  IF take>0 THEN INSERT INTO warehouse_storage_placement_lots(placement_id,position_id,quantity) VALUES(NEW.id,l.id,take); needed:=needed-take; END IF;
  EXIT WHEN needed=0;
 END LOOP;
 -- Old pre-cutover tasks have no certified fee history. Their existing physical
 -- flow remains available, with explicit legacy-unreconciled accounting evidence.
 IF found_receipts AND needed<>0 THEN RAISE EXCEPTION 'storage placement lacks complete receipt coverage'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_placement_receipt_link AFTER INSERT ON warehouse_placements FOR EACH ROW EXECUTE FUNCTION link_storage_placement_lots();

CREATE FUNCTION expected_storage_departure_sources(departure_kind TEXT,business_id TEXT)
RETURNS TABLE(balance_id TEXT,product_id TEXT,variant_id TEXT,supplier_id TEXT,warehouse_id TEXT,bin_code TEXT,location_type TEXT,location_key TEXT,quantity BIGINT) LANGUAGE SQL STABLE AS $$
 WITH actual AS (
  SELECT b.id::text AS balance_id,b.product_id::text AS product_id,COALESCE(b.variant_id::text,'') AS variant_id,COALESCE(b.supplier_id,'')::text AS supplier_id,COALESCE(b.warehouse_id,'') AS warehouse_id,
   CASE WHEN b.location_type='bin' THEN warehouse_bin_identity(b.location_key,b.bin_code) ELSE '' END AS bin_code,b.location_type::text AS location_type,b.location_key::text AS location_key,(a->>'quantity')::bigint AS units
  FROM order_inventory_captures c JOIN orders o ON o.id=c.order_id CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a JOIN inventory_balances b ON b.id=a->>'balanceId'
  WHERE departure_kind='warehouse_shipment' AND c.order_id=business_id AND o.payment_status='paid' AND o.shipped_at IS NOT NULL AND b.owner_type='supplier' AND b.location_type IN ('bin','warehouse') AND b.product_id=c.product_id
   AND ((c.inventory_mode='tracked' AND b.variant_id=c.variant_id) OR (c.inventory_mode<>'tracked' AND b.variant_id IS NULL))
  UNION ALL
  SELECT b.id::text,b.product_id::text,COALESCE(b.variant_id::text,''),COALESCE(b.supplier_id,'')::text,COALESCE(b.warehouse_id,''),CASE WHEN b.location_type='bin' THEN warehouse_bin_identity(b.location_key,b.bin_code) ELSE '' END,b.location_type::text,b.location_key::text,ir.quantity::bigint
  FROM inventory_reservations ir JOIN supplier_return_requests r ON r.id=ir.reference_id
  JOIN inventory_balances b ON b.product_id=ir.product_id AND b.supplier_id=ir.supplier_id AND COALESCE(b.variant_id::text,'')=COALESCE(ir.variant_id::text,'') AND b.location_key=ir.location_key AND b.status='return_requested'
  WHERE departure_kind='supplier_return' AND ir.reference_type='supplier_return' AND ir.reference_id=business_id AND ir.status='consumed' AND r.supplier_id=ir.supplier_id AND r.status IN ('packed','pickup_pending','postal_pending','in_transit','delivered','completed') AND b.owner_type='supplier' AND b.location_type IN ('bin','warehouse')
 ) SELECT a.balance_id,a.product_id,a.variant_id,a.supplier_id,a.warehouse_id,a.bin_code,a.location_type,a.location_key,SUM(a.units)::bigint FROM actual a GROUP BY 1,2,3,4,5,6,7,8;
$$;
CREATE TABLE warehouse_storage_history_exceptions (
 id TEXT PRIMARY KEY,event_type TEXT NOT NULL,event_id TEXT NOT NULL,
 source_balance_id TEXT NOT NULL REFERENCES inventory_balances(id) ON DELETE RESTRICT,
 product_id TEXT NOT NULL,variant_id TEXT NOT NULL,supplier_id TEXT NOT NULL,warehouse_id TEXT NOT NULL,
 bin_code TEXT NOT NULL,location_type TEXT NOT NULL,location_key TEXT NOT NULL,
 quantity INT NOT NULL CHECK(quantity>0),reason TEXT NOT NULL CHECK(length(btrim(reason))>0),
 actor_user_id TEXT NOT NULL CHECK(length(btrim(actor_user_id))>0),created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(event_type,event_id,source_balance_id)
);
CREATE INDEX warehouse_storage_exception_scope ON warehouse_storage_history_exceptions(supplier_id,product_id,variant_id,warehouse_id,bin_code);
CREATE TRIGGER storage_history_exception_identity BEFORE UPDATE OR DELETE ON warehouse_storage_history_exceptions FOR EACH ROW EXECUTE FUNCTION immutable_storage_history();
CREATE FUNCTION guard_storage_history_exception() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s RECORD;
BEGIN
 SELECT * INTO s FROM expected_storage_departure_sources(NEW.event_type,NEW.event_id) WHERE balance_id=NEW.source_balance_id;
 IF NOT FOUND OR (NEW.product_id,NEW.variant_id,NEW.supplier_id,NEW.warehouse_id,NEW.bin_code,NEW.location_type,NEW.location_key,NEW.quantity::bigint)
  IS DISTINCT FROM (s.product_id,s.variant_id,s.supplier_id,s.warehouse_id,s.bin_code,s.location_type,s.location_key,s.quantity)
  OR EXISTS(SELECT 1 FROM warehouse_storage_lot_departures WHERE event_type=NEW.event_type AND event_id=NEW.event_id AND source_balance_id=NEW.source_balance_id)
 THEN RAISE EXCEPTION 'storage exception requires the whole actual unresolved physical source'; END IF;
 NEW.created_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER storage_history_exception_insert BEFORE INSERT ON warehouse_storage_history_exceptions FOR EACH ROW EXECUTE FUNCTION guard_storage_history_exception();
CREATE FUNCTION storage_receipt_is_blocked(position_ref TEXT) RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots l JOIN warehouse_storage_positions current_position ON current_position.id=l.id JOIN inventory_balances b ON b.id=l.source_balance_id JOIN warehouse_storage_history_exceptions x ON x.product_id=l.product_id AND x.variant_id=l.variant_id AND x.supplier_id=l.supplier_id AND (x.warehouse_id='' OR x.warehouse_id=l.warehouse_id)
  WHERE l.id=position_ref AND (current_position.closed_at IS NULL OR current_position.closed_at>x.created_at) AND ((x.bin_code='' AND x.location_key=b.location_key) OR (x.bin_code<>'' AND EXISTS(SELECT 1 FROM warehouse_storage_placement_lots m JOIN warehouse_placements p ON p.id=m.placement_id WHERE m.position_id=l.id AND p.bin_code=x.bin_code))));
$$;
CREATE FUNCTION guard_storage_physical_departure() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l warehouse_storage_receipt_lots%ROWTYPE;p warehouse_storage_positions%ROWTYPE;s RECORD; wp warehouse_placements%ROWTYPE; original inventory_balances%ROWTYPE; used NUMERIC;allowed NUMERIC; remaining INT; previous_time TIMESTAMPTZ;
BEGIN
 SELECT * INTO s FROM expected_storage_departure_sources(NEW.event_type,NEW.event_id) WHERE balance_id=NEW.source_balance_id;
 IF NOT FOUND OR s.variant_id IS DISTINCT FROM NEW.physical_variant_id THEN RAISE EXCEPTION 'actual storage departure source required'; END IF;
 SELECT * INTO p FROM warehouse_storage_positions WHERE id=NEW.position_id FOR UPDATE;
 SELECT * INTO l FROM warehouse_storage_receipt_lots WHERE id=NEW.position_id;
 IF NOT FOUND OR (l.product_id,l.variant_id,l.supplier_id) IS DISTINCT FROM (s.product_id,s.variant_id,s.supplier_id) OR (s.warehouse_id<>'' AND l.warehouse_id<>s.warehouse_id) OR storage_receipt_is_blocked(l.id) THEN RAISE EXCEPTION 'storage departure cannot use another receipt, SKU or warehouse'; END IF;
 SELECT COALESCE(SUM(quantity),0) INTO used FROM warehouse_storage_lot_departures WHERE event_type=NEW.event_type AND event_id=NEW.event_id AND source_balance_id=NEW.source_balance_id;
 IF used+NEW.quantity>s.quantity OR EXISTS(SELECT 1 FROM warehouse_storage_history_exceptions WHERE event_type=NEW.event_type AND event_id=NEW.event_id AND source_balance_id=NEW.source_balance_id) THEN RAISE EXCEPTION 'storage departure exceeds actual physical event'; END IF;
 IF NEW.placement_id<>'' THEN
  SELECT * INTO wp FROM warehouse_placements WHERE id=NEW.placement_id;
  IF NOT FOUND OR (wp.product_id,wp.variant_id,wp.supplier_id,wp.warehouse_id,wp.bin_code) IS DISTINCT FROM (l.product_id,l.variant_id,l.supplier_id,l.warehouse_id,s.bin_code) OR s.location_type<>'bin' THEN RAISE EXCEPTION 'storage bin placement identity mismatch'; END IF;
  SELECT quantity INTO allowed FROM warehouse_occupancy_releases WHERE event_type=NEW.event_type AND event_id=NEW.event_id AND placement_id=NEW.placement_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'storage bin departure requires actual measured placement release'; END IF;
  SELECT COALESCE(SUM(quantity),0) INTO used FROM warehouse_storage_lot_departures WHERE event_type=NEW.event_type AND event_id=NEW.event_id AND placement_id=NEW.placement_id;
  IF used+NEW.quantity>allowed THEN RAISE EXCEPTION 'storage departure exceeds released placement'; END IF;
  SELECT quantity INTO allowed FROM warehouse_storage_placement_lots WHERE placement_id=NEW.placement_id AND position_id=l.id;
  IF NOT FOUND THEN RAISE EXCEPTION 'storage receipt placement allocation missing'; END IF;
  SELECT COALESCE(SUM(quantity),0) INTO used FROM warehouse_storage_lot_departures WHERE placement_id=NEW.placement_id AND position_id=l.id;
  IF used+NEW.quantity>allowed THEN RAISE EXCEPTION 'storage receipt placement over-consumed'; END IF;
 ELSE
  SELECT * INTO original FROM inventory_balances WHERE id=l.source_balance_id;
  IF (original.product_id,COALESCE(original.variant_id::text,''),COALESCE(original.supplier_id,''),original.location_type::text,original.location_key::text) IS DISTINCT FROM (s.product_id,s.variant_id,s.supplier_id,s.location_type,s.location_key) OR s.location_type<>'warehouse' THEN RAISE EXCEPTION 'storage floor receipt pool mismatch'; END IF;
  SELECT l.initial_quantity-COALESCE((SELECT SUM(quantity) FROM warehouse_storage_placement_lots WHERE position_id=l.id),0) INTO allowed;
  SELECT COALESCE(SUM(quantity),0) INTO used FROM warehouse_storage_lot_departures WHERE position_id=l.id AND placement_id='';
  IF used+NEW.quantity>allowed THEN RAISE EXCEPTION 'storage departure reuses placed or departed floor units'; END IF;
 END IF;
 SELECT remaining_quantity_after,effective_at INTO remaining,previous_time FROM warehouse_storage_lot_departures WHERE position_id=l.id ORDER BY effective_at DESC,ordinal DESC LIMIT 1;
 IF NOT FOUND THEN remaining:=l.initial_quantity;previous_time:=l.held_from; END IF;
 NEW.effective_at:=clock_timestamp();
 IF p.qty<>remaining OR NEW.quantity>remaining OR NEW.effective_at<previous_time OR NEW.effective_at<l.held_from OR (p.last_assessed_date IS NOT NULL AND NEW.effective_at<((p.last_assessed_date+1)::timestamp AT TIME ZONE 'Asia/Tehran')) THEN RAISE EXCEPTION 'storage departure violates quantity history or fee checkpoint'; END IF;
 NEW.remaining_quantity_after:=remaining-NEW.quantity;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_physical_departure_insert BEFORE INSERT ON warehouse_storage_lot_departures FOR EACH ROW EXECUTE FUNCTION guard_storage_physical_departure();
CREATE FUNCTION project_storage_physical_departure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 UPDATE warehouse_storage_positions SET qty=NEW.remaining_quantity_after,closed_at=CASE WHEN NEW.remaining_quantity_after=0 THEN NEW.effective_at ELSE NULL END,updated_at=clock_timestamp() WHERE id=NEW.position_id;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_physical_departure_projection AFTER INSERT ON warehouse_storage_lot_departures FOR EACH ROW EXECUTE FUNCTION project_storage_physical_departure();
CREATE FUNCTION complete_storage_departure_source() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected NUMERIC;actual NUMERIC;
BEGIN
 SELECT quantity INTO expected FROM expected_storage_departure_sources(NEW.event_type,NEW.event_id) WHERE balance_id=NEW.source_balance_id;
 SELECT COALESCE((SELECT SUM(quantity) FROM warehouse_storage_lot_departures WHERE event_type=NEW.event_type AND event_id=NEW.event_id AND source_balance_id=NEW.source_balance_id),0)+COALESCE((SELECT quantity FROM warehouse_storage_history_exceptions WHERE event_type=NEW.event_type AND event_id=NEW.event_id AND source_balance_id=NEW.source_balance_id),0) INTO actual;
 IF expected IS NULL OR actual<>expected THEN RAISE EXCEPTION 'storage physical source must be fully accounted or explicitly unresolved'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER storage_departure_source_complete AFTER INSERT ON warehouse_storage_lot_departures DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION complete_storage_departure_source();
CREATE CONSTRAINT TRIGGER storage_exception_source_complete AFTER INSERT ON warehouse_storage_history_exceptions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION complete_storage_departure_source();

CREATE FUNCTION storage_receipt_quantity_at(position_ref TEXT,fee_day DATE) RETURNS BIGINT LANGUAGE SQL STABLE AS $$
 SELECT CASE WHEN l.history_status='verified' AND l.held_from<=(fee_day::timestamp AT TIME ZONE 'Asia/Tehran')
  AND fee_day>=(l.billing_start_at AT TIME ZONE 'Asia/Tehran')::date+l.free_days
  THEN COALESCE((SELECT d.remaining_quantity_after FROM warehouse_storage_lot_departures d WHERE d.position_id=l.id AND d.effective_at<=(fee_day::timestamp AT TIME ZONE 'Asia/Tehran') ORDER BY d.effective_at DESC,d.ordinal DESC LIMIT 1),l.initial_quantity) ELSE 0 END::bigint
 FROM warehouse_storage_receipt_lots l WHERE l.id=position_ref;
$$;
CREATE FUNCTION guard_storage_daily_charge() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l warehouse_storage_receipt_lots%ROWTYPE; units BIGINT; expected NUMERIC;
BEGIN
 PERFORM 1 FROM warehouse_storage_positions WHERE id=NEW.position_id FOR UPDATE;
 SELECT * INTO l FROM warehouse_storage_receipt_lots WHERE id=NEW.position_id AND history_status='verified';
 IF NOT FOUND OR storage_receipt_is_blocked(l.id) THEN RAISE EXCEPTION 'uncertified storage history cannot create new debt'; END IF;
 units:=storage_receipt_quantity_at(l.id,NEW.fee_date);
 expected:=units::numeric*l.unit_day_toman;
 IF NEW.fee_date>=(clock_timestamp() AT TIME ZONE 'Asia/Tehran')::date OR units<=0 OR units>l.initial_quantity OR expected>9007199254740991
  OR (NEW.supplier_id,NEW.product_id,NEW.warehouse_id,NEW.qty::bigint,NEW.fee_unit_day_toman,NEW.amount_toman::numeric,NEW.estimated)
   IS DISTINCT FROM (l.supplier_id,l.product_id,l.warehouse_id,units,l.unit_day_toman,expected,FALSE)
  OR NEW.goods_class IS DISTINCT FROM (SELECT goods_class FROM warehouse_storage_positions WHERE id=l.id)
 THEN RAISE EXCEPTION 'storage debt must match the completed receipt quantity-day and frozen rate'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_daily_charge_insert BEFORE INSERT ON warehouse_storage_fee_charges FOR EACH ROW EXECUTE FUNCTION guard_storage_daily_charge();
CREATE FUNCTION guard_storage_charge_certificate() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected NUMERIC; fee BIGINT; actual_position TEXT;
BEGIN
 SELECT position_id,amount_toman INTO actual_position,fee FROM warehouse_storage_fee_charges WHERE id=NEW.charge_id;
 IF NOT FOUND OR actual_position IS DISTINCT FROM NEW.position_id THEN RAISE EXCEPTION 'storage charge certificate source missing'; END IF;
 PERFORM 1 FROM warehouse_storage_positions WHERE id=NEW.position_id FOR UPDATE;
 SELECT COALESCE((SELECT cumulative_total_toman FROM warehouse_storage_certified_charges WHERE position_id=NEW.position_id ORDER BY ordinal DESC LIMIT 1),0)::numeric+fee INTO expected;
 IF NEW.cumulative_total_toman IS DISTINCT FROM expected OR expected>9007199254740991 THEN RAISE EXCEPTION 'storage cumulative debt mismatch'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_charge_certificate_insert BEFORE INSERT ON warehouse_storage_certified_charges FOR EACH ROW EXECUTE FUNCTION guard_storage_charge_certificate();
CREATE FUNCTION record_storage_charge_certificate() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total NUMERIC;
BEGIN
 SELECT COALESCE((SELECT cumulative_total_toman FROM warehouse_storage_certified_charges WHERE position_id=NEW.position_id ORDER BY ordinal DESC LIMIT 1),0)::numeric+NEW.amount_toman INTO total;
 IF total>9007199254740991 THEN RAISE EXCEPTION 'storage cumulative debt overflow'; END IF;
 INSERT INTO warehouse_storage_certified_charges(charge_id,position_id,cumulative_total_toman) VALUES(NEW.id,NEW.position_id,total);
 UPDATE warehouse_storage_positions SET total_fee_charged_toman=total,updated_at=clock_timestamp() WHERE id=NEW.position_id;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_daily_charge_certificate AFTER INSERT ON warehouse_storage_fee_charges FOR EACH ROW EXECUTE FUNCTION record_storage_charge_certificate();
CREATE FUNCTION guard_storage_charge_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots WHERE id=OLD.position_id) THEN
  IF TG_OP='DELETE' OR (to_jsonb(NEW)-ARRAY['journal_debit_id','journal_credit_id']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['journal_debit_id','journal_credit_id']) THEN RAISE EXCEPTION 'immutable certified storage debt; use a reviewed adjustment'; END IF;
  IF OLD.journal_debit_id IS NOT NULL AND NEW.journal_debit_id IS DISTINCT FROM OLD.journal_debit_id THEN RAISE EXCEPTION 'storage debit journal cannot be reassigned'; END IF;
  IF OLD.journal_credit_id IS NOT NULL AND NEW.journal_credit_id IS DISTINCT FROM OLD.journal_credit_id THEN RAISE EXCEPTION 'storage credit journal cannot be reassigned'; END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_daily_charge_identity BEFORE UPDATE OR DELETE ON warehouse_storage_fee_charges FOR EACH ROW EXECUTE FUNCTION guard_storage_charge_identity();

CREATE FUNCTION guard_storage_position_history_projection() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l warehouse_storage_receipt_lots%ROWTYPE; expected_quantity INT; expected_closed TIMESTAMPTZ; expected_total BIGINT; first_day DATE;
BEGIN
 SELECT * INTO l FROM warehouse_storage_receipt_lots WHERE id=OLD.id;
 IF NOT FOUND THEN RETURN NEW; END IF;
 IF (NEW.id,NEW.product_id,NEW.supplier_id,NEW.warehouse_id,NEW.inbound_id,NEW.goods_class,NEW.stocked_at,NEW.original_stocked_at)
  IS DISTINCT FROM (OLD.id,OLD.product_id,OLD.supplier_id,OLD.warehouse_id,OLD.inbound_id,OLD.goods_class,OLD.stocked_at,OLD.original_stocked_at) THEN RAISE EXCEPTION 'immutable storage receipt position identity'; END IF;
 SELECT remaining_quantity_after,effective_at INTO expected_quantity,expected_closed FROM warehouse_storage_lot_departures WHERE position_id=OLD.id ORDER BY effective_at DESC,ordinal DESC LIMIT 1;
 IF NOT FOUND THEN expected_quantity:=l.initial_quantity;expected_closed:=NULL; ELSIF expected_quantity>0 THEN expected_closed:=NULL; END IF;
 SELECT COALESCE((SELECT cumulative_total_toman FROM warehouse_storage_certified_charges WHERE position_id=OLD.id ORDER BY ordinal DESC LIMIT 1),0) INTO expected_total;
 IF (NEW.qty,NEW.closed_at,NEW.total_fee_charged_toman) IS DISTINCT FROM (expected_quantity,expected_closed,expected_total) THEN RAISE EXCEPTION 'storage counters require immutable physical departures and certified charges'; END IF;
 IF (OLD.last_assessed_date IS NOT NULL AND (NEW.last_assessed_date IS NULL OR NEW.last_assessed_date<OLD.last_assessed_date)) OR (OLD.last_fee_date IS NOT NULL AND (NEW.last_fee_date IS NULL OR NEW.last_fee_date<OLD.last_fee_date)) THEN RAISE EXCEPTION 'storage checkpoints cannot move backwards'; END IF;
 IF NEW.last_fee_date IS NOT NULL AND (NEW.last_assessed_date IS NULL OR NEW.last_fee_date>NEW.last_assessed_date OR NOT EXISTS(SELECT 1 FROM warehouse_storage_fee_charges WHERE position_id=OLD.id AND fee_date=NEW.last_fee_date)) THEN RAISE EXCEPTION 'storage fee checkpoint requires recorded complete-day evidence'; END IF;
 IF NEW.last_assessed_date IS DISTINCT FROM OLD.last_assessed_date THEN
  IF l.history_status<>'verified' OR storage_receipt_is_blocked(l.id) OR NEW.last_assessed_date IS NULL OR NEW.last_assessed_date>=(clock_timestamp() AT TIME ZONE 'Asia/Tehran')::date THEN RAISE EXCEPTION 'unverified or incomplete storage days cannot be checkpointed'; END IF;
  first_day:=GREATEST((l.billing_start_at AT TIME ZONE 'Asia/Tehran')::date+l.free_days,(l.held_from AT TIME ZONE 'Asia/Tehran')::date,COALESCE(OLD.last_assessed_date+1,'-infinity'::date));
  IF NEW.last_assessed_date-first_day>=32 THEN RAISE EXCEPTION 'storage assessment pass exceeds its bounded window'; END IF;
  IF EXISTS(SELECT 1 FROM generate_series(first_day::timestamp,NEW.last_assessed_date::timestamp,INTERVAL '1 day') day
   WHERE storage_receipt_quantity_at(l.id,day::date)>0 AND l.unit_day_toman>0 AND NOT EXISTS(
    SELECT 1 FROM warehouse_storage_fee_charges f JOIN warehouse_storage_certified_charges c ON c.charge_id=f.id WHERE f.position_id=l.id AND f.fee_date=day::date AND f.qty=storage_receipt_quantity_at(l.id,day::date) AND f.fee_unit_day_toman=l.unit_day_toman AND f.amount_toman::numeric=f.qty::numeric*l.unit_day_toman
   )) THEN RAISE EXCEPTION 'storage checkpoint would erase unpaid completed quantity-days'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER storage_position_history_projection BEFORE UPDATE ON warehouse_storage_positions FOR EACH ROW EXECUTE FUNCTION guard_storage_position_history_projection();
CREATE VIEW warehouse_storage_history_audit AS
 SELECT p.id AS position_id,p.product_id,p.supplier_id,p.warehouse_id,COALESCE(l.variant_id,'') AS variant_id,
  p.qty AS current_quantity,l.initial_quantity,COALESCE(l.initial_quantity-d.remaining_quantity_after,0) AS departed_quantity,
  CASE WHEN l.id IS NOT NULL AND storage_receipt_is_blocked(l.id) THEN 'blocked_departure_history' ELSE COALESCE(l.history_status,'legacy_unreconciled') END AS history_status,
  CASE WHEN l.id IS NULL THEN 'legacy_receipt_age_quantity_and_tariff_require_review'
   WHEN storage_receipt_is_blocked(l.id) THEN 'physical_departure_history_requires_reconciliation'
   WHEN COALESCE(d.remaining_quantity_after,l.initial_quantity)<>p.qty THEN 'quantity_projection_mismatch'
   WHEN l.history_status<>'verified' THEN l.unresolved_reason ELSE '' END AS issue
 FROM warehouse_storage_positions p LEFT JOIN warehouse_storage_receipt_lots l ON l.id=p.id
 LEFT JOIN LATERAL (SELECT remaining_quantity_after FROM warehouse_storage_lot_departures WHERE position_id=p.id ORDER BY effective_at DESC,ordinal DESC LIMIT 1) d ON TRUE;

-- New returned lots already start at QC acceptance. Retain the original bin-entry
-- command and all its physical effects; omit only its redundant accounting credit.
-- Pre-cutover commands without known receipts keep their unresolved legacy row.
CREATE FUNCTION skip_known_return_bin_storage_credit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e warehouse_return_bin_entries%ROWTYPE;
BEGIN
 SELECT * INTO e FROM warehouse_return_bin_entries WHERE 'WSP-'||id=NEW.id;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM warehouse_storage_receipt_lots WHERE source_kind='customer_return' AND source_event_id=e.return_item_id::text) THEN RETURN NEW; END IF;
 IF (NEW.product_id,NEW.supplier_id,NEW.warehouse_id,NEW.goods_class,NEW.qty,NEW.stocked_at,NEW.original_stocked_at)
  IS DISTINCT FROM (e.product_id,e.supplier_id,e.warehouse_id,'returned'::varchar,e.quantity,e.received_at,e.received_at) THEN RAISE EXCEPTION 'return bin storage identity mismatch'; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER storage_return_bin_credit BEFORE INSERT ON warehouse_storage_positions FOR EACH ROW EXECUTE FUNCTION skip_known_return_bin_storage_credit();
