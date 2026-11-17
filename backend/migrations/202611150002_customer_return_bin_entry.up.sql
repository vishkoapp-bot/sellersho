-- Explicit post-QC entry. No historical balances or counters are rewritten.
CREATE TABLE warehouse_return_bin_entries (
 id TEXT PRIMARY KEY,
 return_id UUID NOT NULL REFERENCES customer_returns(id) ON DELETE RESTRICT,
 return_item_id UUID NOT NULL REFERENCES return_items(id) ON DELETE RESTRICT,
 bin_code TEXT NOT NULL REFERENCES warehouse_bins(bin_code) ON DELETE RESTRICT,
 quantity INT NOT NULL CHECK(quantity>0),
 reason TEXT NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 2000),
 actor_user_id TEXT NOT NULL CHECK(length(btrim(actor_user_id))>0),
 idempotency_key TEXT NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 200),
 source_balance_id TEXT NOT NULL REFERENCES inventory_balances(id) ON DELETE RESTRICT,
 product_id TEXT NOT NULL,
 variant_id TEXT NOT NULL,
 supplier_id TEXT NOT NULL,
 warehouse_id TEXT NOT NULL,
 volume_cm3 BIGINT NOT NULL CHECK(volume_cm3>0),
 weight_grams BIGINT NOT NULL CHECK(weight_grams>0),
 measurement_revision_id TEXT NOT NULL REFERENCES inventory_measurement_revisions(id) ON DELETE RESTRICT,
 received_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(actor_user_id,idempotency_key)
);
CREATE INDEX warehouse_return_bin_entries_item ON warehouse_return_bin_entries(return_item_id,id);
CREATE TRIGGER warehouse_return_bin_entry_identity BEFORE UPDATE OR DELETE ON warehouse_return_bin_entries FOR EACH ROW EXECUTE FUNCTION immutable_warehouse_placement();

ALTER TABLE warehouse_placements ALTER COLUMN task_id DROP NOT NULL;
ALTER TABLE warehouse_placements ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'inbound_putaway';
ALTER TABLE warehouse_placements ADD COLUMN return_entry_id TEXT UNIQUE REFERENCES warehouse_return_bin_entries(id) ON DELETE RESTRICT;
ALTER TABLE warehouse_placements ADD CONSTRAINT warehouse_placement_source CHECK(
 (source_kind='inbound_putaway' AND task_id IS NOT NULL AND return_entry_id IS NULL) OR
 (source_kind='customer_return' AND task_id IS NULL AND return_entry_id IS NOT NULL));
-- Retain the original inbound guard verbatim, restricted to its source kind.
DROP TRIGGER warehouse_placement_insert ON warehouse_placements;
CREATE TRIGGER warehouse_placement_insert BEFORE INSERT ON warehouse_placements FOR EACH ROW WHEN(NEW.source_kind='inbound_putaway') EXECUTE FUNCTION guard_warehouse_placement_insert();

-- The receipt movement and received item prove origin. Legacy merged pools are
-- eligible only when exactly one QC-ok line could have credited that pool.
CREATE VIEW warehouse_customer_return_bin_sources AS
 SELECT cr.id AS return_id,ri.id AS return_item_id,cr.order_id,oi.product_id,
  CASE WHEN c.inventory_mode='tracked' THEN c.variant_id::text ELSE '' END AS variant_id,
  p.title AS product_title,COALESCE(oi.variant_name,'') AS variant_name,COALESCE(oi.variant_spnc,'') AS variant_spnc,
  cr.warehouse_id,ri.qty,ri.qc_status,ri.received_at,cr.status AS return_status,
  b.id AS source_balance_id,b.supplier_id,b.location_key,
  LEAST(COALESCE(b.quantity,0),ri.qty-COALESCE(e.quantity,0))::bigint AS available_quantity,
  (cr.status IN ('received','refunded') AND ri.qc_status='ok' AND ri.received_at IS NOT NULL
   AND cr.warehouse_id IS NOT NULL AND cr.destination_supplier_warehouse_id IS NULL
   AND b.id IS NOT NULL AND b.owner_type='supplier' AND b.location_type='warehouse' AND b.status='sellable'
   AND b.supplier_id=p.supplier_id AND m.supplier_id=b.supplier_id AND m.quantity=ri.qty
   AND COALESCE(b.variant_id::text,'')=CASE WHEN c.inventory_mode='tracked' THEN c.variant_id::text ELSE '' END
   AND ((c.inventory_mode='tracked' AND b.warehouse_id=cr.warehouse_id)
    OR (COALESCE(c.inventory_mode,'legacy')<>'tracked' AND (b.warehouse_id IS NULL OR b.warehouse_id=cr.warehouse_id)
     AND m.metadata->>'destination'='warehouse:'||cr.warehouse_id
     AND (SELECT COUNT(*) FROM return_items sibling JOIN order_items soi ON soi.id::text=sibling.order_item_id WHERE sibling.return_id=ri.return_id AND soi.product_id=oi.product_id AND sibling.qc_status='ok')=1))) IS TRUE AS source_proven
 FROM customer_returns cr JOIN return_items ri ON ri.return_id=cr.id
 JOIN order_items oi ON oi.id::text=ri.order_item_id JOIN products p ON p.id=oi.product_id
 LEFT JOIN order_inventory_captures c ON c.order_item_id=ri.order_item_id
 LEFT JOIN inventory_movements m ON m.idempotency_key=CASE WHEN c.inventory_mode='tracked' THEN 'sku-return:'||ri.id::text ELSE 'crstk:'||cr.id::text||':'||ri.id::text||':sellable' END
  AND m.product_id=oi.product_id AND m.to_status='sellable' AND m.reference_type='customer_return'
  AND m.reference_id=CASE WHEN c.inventory_mode='tracked' THEN ri.id::text ELSE cr.id::text END
  AND m.to_location_key=CASE WHEN c.inventory_mode='tracked' THEN 'warehouse:'||cr.warehouse_id||':return:'||ri.id::text ELSE 'custreturn:'||cr.id::text END
  AND COALESCE(m.variant_id::text,'')=CASE WHEN c.inventory_mode='tracked' THEN c.variant_id::text ELSE '' END
 LEFT JOIN inventory_balances b ON b.product_id=oi.product_id AND b.location_key=m.to_location_key AND b.status='sellable' AND b.owner_type='supplier' AND b.location_type='warehouse' AND b.supplier_id=m.supplier_id AND COALESCE(b.variant_id::text,'')=COALESCE(m.variant_id::text,'')
 LEFT JOIN LATERAL (SELECT SUM(quantity) AS quantity FROM warehouse_return_bin_entries e WHERE e.return_item_id=ri.id) e ON TRUE;

CREATE FUNCTION guard_customer_return_bin_entry() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s warehouse_customer_return_bin_sources%ROWTYPE; physical inventory_effective_measurements%ROWTYPE;
 target warehouse_bins%ROWTYPE; rq NUMERIC; rv NUMERIC; rw NUMERIC; reserved_v NUMERIC; reserved_w NUMERIC;
 owner TEXT; product TEXT; variant TEXT; order_ref TEXT; return_ref UUID;
BEGIN
 IF NEW.quantity<=0 OR length(btrim(NEW.reason)) NOT BETWEEN 1 AND 2000 OR length(btrim(NEW.actor_user_id))=0 OR length(NEW.idempotency_key) NOT BETWEEN 1 AND 200 THEN RAISE EXCEPTION 'invalid return bin command'; END IF;
 SELECT oi.product_id,p.supplier_id,COALESCE(c.variant_id::text,''),cr.order_id,cr.id INTO product,owner,variant,order_ref,return_ref
 FROM return_items ri JOIN customer_returns cr ON cr.id=ri.return_id JOIN order_items oi ON oi.id::text=ri.order_item_id JOIN products p ON p.id=oi.product_id LEFT JOIN order_inventory_captures c ON c.order_item_id=ri.order_item_id
 WHERE ri.id=NEW.return_item_id AND cr.id=NEW.return_id;
 IF NOT FOUND OR owner IS NULL THEN RAISE EXCEPTION 'return item identity missing'; END IF;
 -- Same parent order as receipt/refund; then the global account/product/SKU/work prefix.
 PERFORM 1 FROM orders WHERE id=order_ref FOR UPDATE;
 PERFORM 1 FROM suppliers WHERE id=owner FOR UPDATE;
 PERFORM 1 FROM products WHERE id=product FOR UPDATE;
 IF variant<>'' THEN PERFORM 1 FROM product_variants WHERE id::text=variant FOR UPDATE; END IF;
 PERFORM 1 FROM customer_returns WHERE id=return_ref FOR UPDATE;
 PERFORM 1 FROM return_items WHERE id=NEW.return_item_id FOR UPDATE;
 -- Parent guards prevent payment/return moves or measurement edits while we
 -- discover and lock both surviving source and any existing destination lots.
 PERFORM 1 FROM inventory_balances WHERE product_id=product ORDER BY id COLLATE "C" FOR UPDATE;
 SELECT * INTO s FROM warehouse_customer_return_bin_sources WHERE return_item_id=NEW.return_item_id AND return_id=NEW.return_id;
 IF NOT FOUND OR NOT s.source_proven OR s.supplier_id IS DISTINCT FROM owner OR s.available_quantity<NEW.quantity THEN RAISE EXCEPTION 'return source requires reconciliation or is no longer available'; END IF;
 SELECT * INTO physical FROM inventory_effective_measurements WHERE product_id=s.product_id AND variant_id=s.variant_id AND verified;
 IF NOT FOUND THEN RAISE EXCEPTION 'current verified return measurement required'; END IF;
 SELECT * INTO target FROM warehouse_bins WHERE bin_code=NEW.bin_code FOR UPDATE;
 IF NOT FOUND OR target.warehouse_id IS DISTINCT FROM s.warehouse_id OR target.capacity_status<>'verified' OR EXISTS(SELECT 1 FROM warehouse_occupancy_exceptions WHERE bin_code=NEW.bin_code) THEN RAISE EXCEPTION 'matching verified return bin required'; END IF;
 SELECT COALESCE(SUM(remaining_quantity),0),COALESCE(SUM(remaining_volume_cm3),0),COALESCE(SUM(remaining_weight_grams),0) INTO rq,rv,rw FROM warehouse_remaining_placements WHERE bin_code=NEW.bin_code;
 IF (target.quantity::numeric,target.occupied_volume_cm3::numeric,target.occupied_weight_grams::numeric) IS DISTINCT FROM (rq,rv,rw) THEN RAISE EXCEPTION 'return bin counters require reconciliation'; END IF;
 NEW.source_balance_id:=s.source_balance_id; NEW.product_id:=s.product_id; NEW.variant_id:=s.variant_id;
 NEW.supplier_id:=s.supplier_id; NEW.warehouse_id:=s.warehouse_id; NEW.received_at:=s.received_at;
 NEW.measurement_revision_id:=physical.revision;
 NEW.volume_cm3:=ceil(physical.length_mm::numeric*physical.width_mm*physical.height_mm*NEW.quantity/1000);
 NEW.weight_grams:=physical.weight_grams::numeric*NEW.quantity;
 SELECT COALESCE(SUM(volume_cm3),0),COALESCE(SUM(weight_grams),0) INTO reserved_v,reserved_w FROM putaway_capacity_reservations WHERE bin_code=NEW.bin_code AND status='active';
 IF rq+NEW.quantity>2147483647 OR target.capacity_volume_cm3 IS NULL OR target.max_weight_grams IS NULL
  OR rv+reserved_v+NEW.volume_cm3>target.capacity_volume_cm3 OR rw+reserved_w+NEW.weight_grams>target.max_weight_grams THEN RAISE EXCEPTION 'return bin capacity unavailable'; END IF;
 IF EXISTS(SELECT 1 FROM inventory_balances WHERE product_id=s.product_id AND COALESCE(variant_id::text,'')=s.variant_id AND location_key='bin:'||NEW.bin_code AND status='sellable' AND (supplier_id IS DISTINCT FROM s.supplier_id OR warehouse_id IS DISTINCT FROM s.warehouse_id OR owner_type<>'supplier' OR location_type<>'bin')) THEN RAISE EXCEPTION 'conflicting return bin ownership'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER customer_return_bin_entry_guard BEFORE INSERT ON warehouse_return_bin_entries FOR EACH ROW EXECUTE FUNCTION guard_customer_return_bin_entry();

CREATE FUNCTION guard_return_warehouse_placement() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM warehouse_return_bin_entries e JOIN inventory_movements m ON m.id='IM-'||e.id
  WHERE e.id=NEW.return_entry_id AND NEW.id='PLACE-'||e.id AND NEW.task_id IS NULL
   AND (e.product_id,e.variant_id,e.supplier_id,e.warehouse_id,e.bin_code,e.quantity,e.volume_cm3,e.weight_grams,e.measurement_revision_id)=(NEW.product_id,NEW.variant_id,NEW.supplier_id,NEW.warehouse_id,NEW.bin_code,NEW.quantity,NEW.volume_cm3,NEW.weight_grams,NEW.measurement_revision_id)
   AND m.reference_type='customer_return_putaway' AND m.reference_id=e.id AND m.quantity=e.quantity
   AND m.product_id=e.product_id AND COALESCE(m.variant_id::text,'')=e.variant_id AND m.supplier_id=e.supplier_id AND m.to_location_key='bin:'||e.bin_code)
 THEN RAISE EXCEPTION 'return placement requires exact immutable entry and transfer'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER warehouse_return_placement_insert BEFORE INSERT ON warehouse_placements FOR EACH ROW WHEN(NEW.source_kind='customer_return') EXECUTE FUNCTION guard_return_warehouse_placement();

-- One statement applies the whole command. A forged/orphan header cannot commit
-- without its conserved movement, new measured placement and bin counters.
CREATE FUNCTION apply_customer_return_bin_entry() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source TEXT;
BEGIN
 SELECT location_key INTO source FROM inventory_balances WHERE id=NEW.source_balance_id;
 UPDATE inventory_balances SET quantity=quantity-NEW.quantity,updated_at=NOW() WHERE id=NEW.source_balance_id AND status='sellable' AND quantity>=NEW.quantity;
 IF NOT FOUND THEN RAISE EXCEPTION 'return source no longer available'; END IF;
 IF NEW.variant_id='' THEN
  INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,warehouse_id,bin_code,reconciled)
   VALUES('INV-'||NEW.id,NEW.product_id,NEW.supplier_id,'supplier','bin','bin:'||NEW.bin_code,'sellable',NEW.quantity,NEW.warehouse_id,NEW.bin_code,TRUE)
   ON CONFLICT(product_id,owner_type,location_type,location_key,status) WHERE variant_id IS NULL DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW();
 ELSE
  INSERT INTO inventory_balances(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,status,quantity,warehouse_id,bin_code,reconciled)
   VALUES('INV-'||NEW.id,NEW.product_id,NEW.supplier_id,NEW.variant_id::uuid,'supplier','bin','bin:'||NEW.bin_code,'sellable',NEW.quantity,NEW.warehouse_id,NEW.bin_code,TRUE)
   ON CONFLICT(product_id,owner_type,location_type,location_key,status,variant_id) WHERE variant_id IS NOT NULL DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW();
 END IF;
 INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata)
 VALUES('IM-'||NEW.id,NEW.product_id,NEW.supplier_id,NULLIF(NEW.variant_id,'')::uuid,NEW.quantity,'sellable','sellable',source,'bin:'||NEW.bin_code,'customer_return_putaway',NEW.id,'return-bin:'||NEW.id,NEW.actor_user_id,jsonb_build_object('returnItemId',NEW.return_item_id,'reason',NEW.reason));
 INSERT INTO warehouse_placements(id,task_id,product_id,variant_id,supplier_id,warehouse_id,bin_code,quantity,volume_cm3,weight_grams,measurement_revision_id,source_kind,return_entry_id)
 VALUES('PLACE-'||NEW.id,NULL,NEW.product_id,NEW.variant_id,NEW.supplier_id,NEW.warehouse_id,NEW.bin_code,NEW.quantity,NEW.volume_cm3,NEW.weight_grams,NEW.measurement_revision_id,'customer_return',NEW.id);
 UPDATE warehouse_bins SET quantity=quantity+NEW.quantity,occupied_volume_cm3=occupied_volume_cm3+NEW.volume_cm3,occupied_weight_grams=occupied_weight_grams+NEW.weight_grams,updated_at=NOW() WHERE bin_code=NEW.bin_code;
 -- A separate returned-goods cohort retains receipt time, rather than inheriting
 -- an unrelated inbound's fee clock or resetting it to the bin-entry time.
 INSERT INTO warehouse_storage_positions(id,product_id,supplier_id,warehouse_id,inbound_id,goods_class,qty,stocked_at,original_stocked_at)
 VALUES('WSP-'||NEW.id,NEW.product_id,NEW.supplier_id,NEW.warehouse_id,NULL,'returned',NEW.quantity,NEW.received_at,NEW.received_at);
 RETURN NEW;
END $$;
CREATE TRIGGER customer_return_bin_entry_apply AFTER INSERT ON warehouse_return_bin_entries FOR EACH ROW EXECUTE FUNCTION apply_customer_return_bin_entry();
