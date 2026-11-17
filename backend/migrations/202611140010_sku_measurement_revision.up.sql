-- Existing SKU overrides are declarations, not warehouse verification.
ALTER TABLE products ADD COLUMN measurement_revision_id TEXT;
ALTER TABLE product_variants
 ADD COLUMN measurement_revision_id TEXT,
 ADD COLUMN measurement_status TEXT NOT NULL DEFAULT 'unverified' CHECK(measurement_status IN ('unverified','verified','disputed')),
 ADD COLUMN measurement_source TEXT,
 ADD COLUMN measurement_version INT NOT NULL DEFAULT 0;
CREATE TABLE inventory_measurement_revisions (
 id TEXT PRIMARY KEY,
 product_id TEXT NOT NULL,
 variant_id TEXT NOT NULL DEFAULT '',
 scope TEXT NOT NULL CHECK(scope IN ('product','sku')),
 length_mm INT NOT NULL CHECK(length_mm BETWEEN 1 AND 1000000),
 width_mm INT NOT NULL CHECK(width_mm BETWEEN 1 AND 1000000),
 height_mm INT NOT NULL CHECK(height_mm BETWEEN 1 AND 1000000),
 weight_grams INT NOT NULL CHECK(weight_grams BETWEEN 1 AND 1000000000),
 verified BOOLEAN NOT NULL,
 reason TEXT NOT NULL CHECK(reason<>''),
 actor_user_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK((scope='product' AND variant_id='') OR (scope='sku' AND variant_id<>''))
);
-- Preserve the currently verified parent reading as a snapshot, never infer
-- verification for a variant override or recalculate old bin occupancy.
INSERT INTO inventory_measurement_revisions(id,product_id,scope,length_mm,width_mm,height_mm,weight_grams,verified,reason,actor_user_id)
 SELECT 'MEAS-'||gen_random_uuid()::text,id,'product',length_mm,width_mm,height_mm,weight_grams,TRUE,'existing_product_verification_snapshot','migration'
 FROM products WHERE measurement_status='verified' AND length_mm BETWEEN 1 AND 1000000 AND width_mm BETWEEN 1 AND 1000000 AND height_mm BETWEEN 1 AND 1000000 AND weight_grams BETWEEN 1 AND 1000000000;
UPDATE products p SET measurement_revision_id=r.id FROM inventory_measurement_revisions r WHERE r.product_id=p.id AND r.scope='product';
CREATE FUNCTION immutable_inventory_measurement() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'immutable inventory measurement revision'; END $$;
CREATE TRIGGER inventory_measurement_identity BEFORE UPDATE OR DELETE ON inventory_measurement_revisions FOR EACH ROW EXECUTE FUNCTION immutable_inventory_measurement();
CREATE FUNCTION guard_current_inventory_measurement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE variant TEXT; kind TEXT;
BEGIN
 kind:=CASE WHEN TG_TABLE_NAME='products' THEN 'product' ELSE 'sku' END;
 variant:=CASE WHEN kind='sku' THEN NEW.id::text ELSE '' END;
 IF TG_OP='UPDATE' AND (NEW.length_mm,NEW.width_mm,NEW.height_mm,NEW.weight_grams) IS DISTINCT FROM (OLD.length_mm,OLD.width_mm,OLD.height_mm,OLD.weight_grams)
    AND NEW.measurement_revision_id IS NOT DISTINCT FROM OLD.measurement_revision_id THEN
  NEW.measurement_revision_id:=NULL;NEW.measurement_status:='unverified';NEW.measurement_source:='supplier_declared';
 END IF;
 IF NEW.measurement_revision_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM inventory_measurement_revisions r WHERE r.id=NEW.measurement_revision_id AND r.scope=kind
   AND r.product_id=CASE WHEN kind='product' THEN NEW.id::text ELSE to_jsonb(NEW)->>'product_id' END
   AND r.variant_id=variant AND r.verified
   AND (r.length_mm,r.width_mm,r.height_mm,r.weight_grams)=(NEW.length_mm,NEW.width_mm,NEW.height_mm,NEW.weight_grams)
 ) THEN RAISE EXCEPTION 'measurement reference does not match physical identity'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER product_current_measurement BEFORE INSERT OR UPDATE ON products FOR EACH ROW EXECUTE FUNCTION guard_current_inventory_measurement();
CREATE TRIGGER sku_current_measurement BEFORE INSERT OR UPDATE ON product_variants FOR EACH ROW EXECUTE FUNCTION guard_current_inventory_measurement();
CREATE VIEW inventory_effective_measurements AS
 WITH effective AS (
  SELECT p.id::text AS product_id,''::text AS variant_id,COALESCE(p.length_mm,0) AS length_mm,COALESCE(p.width_mm,0) AS width_mm,COALESCE(p.height_mm,0) AS height_mm,COALESCE(p.weight_grams,0) AS weight_grams,
   'product'::text AS source,'product'::text AS scope,''::text AS revision_variant,p.measurement_status AS stored_status,COALESCE(p.measurement_revision_id,'') AS revision,TRUE AS complete
  FROM products p
  UNION ALL
  SELECT p.id::text,v.id::text,COALESCE(v.length_mm,p.length_mm,0),COALESCE(v.width_mm,p.width_mm,0),COALESCE(v.height_mm,p.height_mm,0),COALESCE(v.weight_grams,p.weight_grams,0),
   CASE WHEN s.overridden THEN 'sku' ELSE 'product' END,CASE WHEN s.overridden THEN 'sku' ELSE 'product' END,CASE WHEN s.overridden THEN v.id::text ELSE '' END,
   CASE WHEN s.overridden THEN v.measurement_status ELSE p.measurement_status END,COALESCE(CASE WHEN s.overridden THEN v.measurement_revision_id ELSE p.measurement_revision_id END,''),
   NOT s.overridden OR (v.length_mm IS NOT NULL AND v.width_mm IS NOT NULL AND v.height_mm IS NOT NULL AND v.weight_grams IS NOT NULL)
  FROM products p JOIN product_variants v ON v.product_id=p.id
  CROSS JOIN LATERAL (SELECT v.length_mm IS NOT NULL OR v.width_mm IS NOT NULL OR v.height_mm IS NOT NULL OR v.weight_grams IS NOT NULL AS overridden) s
 )
 SELECT e.product_id,e.variant_id,e.length_mm,e.width_mm,e.height_mm,e.weight_grams,e.source,e.revision,
  COALESCE(r.verified,FALSE) AND e.stored_status='verified' AND e.complete AS verified,
  CASE WHEN e.stored_status='disputed' THEN 'disputed' WHEN COALESCE(r.verified,FALSE) AND e.stored_status='verified' AND e.complete THEN 'verified' ELSE 'unverified' END AS measurement_status
 FROM effective e LEFT JOIN inventory_measurement_revisions r ON r.id=e.revision AND r.product_id=e.product_id AND r.variant_id=e.revision_variant AND r.scope=e.scope AND (r.length_mm,r.width_mm,r.height_mm,r.weight_grams)=(e.length_mm,e.width_mm,e.height_mm,e.weight_grams);
ALTER TABLE putaway_capacity_reservations ADD COLUMN measurement_revision_id TEXT;
CREATE TABLE warehouse_placements (
 id TEXT PRIMARY KEY,
 task_id TEXT NOT NULL UNIQUE,
 product_id TEXT NOT NULL,
 variant_id TEXT NOT NULL DEFAULT '',
 supplier_id TEXT NOT NULL,
 warehouse_id TEXT NOT NULL,
 bin_code TEXT NOT NULL,
 quantity INT NOT NULL CHECK(quantity>0),
 volume_cm3 BIGINT NOT NULL CHECK(volume_cm3>0),
 weight_grams BIGINT NOT NULL CHECK(weight_grams>0),
 measurement_revision_id TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX warehouse_placements_product_bin ON warehouse_placements(product_id,variant_id,bin_code,id);
CREATE FUNCTION immutable_warehouse_placement() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'immutable warehouse placement snapshot'; END $$;
CREATE TRIGGER warehouse_placement_identity BEFORE UPDATE OR DELETE ON warehouse_placements FOR EACH ROW EXECUTE FUNCTION immutable_warehouse_placement();
CREATE FUNCTION guard_warehouse_placement_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(
  SELECT 1 FROM putaway_tasks pt
  JOIN inbound_putaway_allocations a ON a.task_id=pt.id
  JOIN putaway_capacity_reservations r ON r.task_id=pt.id AND r.status='active'
  JOIN inventory_effective_measurements m ON m.product_id=pt.product_id AND m.variant_id=COALESCE(pt.variant_id::text,'') AND m.verified
  WHERE pt.id=NEW.task_id AND pt.status='assigned' AND pt.bin_location=NEW.bin_code
   AND (a.product_id,COALESCE(a.variant_id::text,''),a.supplier_id,a.warehouse_id,a.quantity)=(NEW.product_id,NEW.variant_id,NEW.supplier_id,NEW.warehouse_id,NEW.quantity)
   AND (r.bin_code,r.volume_cm3,r.weight_grams,r.measurement_revision_id)=(NEW.bin_code,NEW.volume_cm3,NEW.weight_grams,NEW.measurement_revision_id)
   AND m.revision=NEW.measurement_revision_id
   AND ceil(m.length_mm::numeric*m.width_mm*m.height_mm*NEW.quantity/1000)=NEW.volume_cm3
   AND m.weight_grams::numeric*NEW.quantity=NEW.weight_grams
 ) THEN RAISE EXCEPTION 'placement requires exact current verified allocation and active reservation'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER warehouse_placement_insert BEFORE INSERT ON warehouse_placements FOR EACH ROW EXECUTE FUNCTION guard_warehouse_placement_insert();
CREATE FUNCTION guard_putaway_measurement_reservation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r inventory_measurement_revisions%ROWTYPE; product TEXT; variant TEXT; units INT; reference TEXT; verification TEXT; l INT; w INT; h INT; g INT;
BEGIN
 IF TG_OP='UPDATE' AND OLD.status='consumed' THEN
  IF (NEW.task_id,NEW.bin_code,NEW.volume_cm3,NEW.weight_grams,NEW.measurement_revision_id,NEW.status) IS DISTINCT FROM (OLD.task_id,OLD.bin_code,OLD.volume_cm3,OLD.weight_grams,OLD.measurement_revision_id,OLD.status) THEN RAISE EXCEPTION 'consumed capacity snapshot is immutable'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.status='released' THEN RETURN NEW; END IF;
 SELECT pt.product_id,COALESCE(pt.variant_id::text,''),pt.quantity INTO product,variant,units FROM putaway_tasks pt WHERE pt.id=NEW.task_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'putaway task missing'; END IF;
 SELECT * INTO r FROM inventory_measurement_revisions WHERE id=NEW.measurement_revision_id AND product_id=product AND verified;
 IF NOT FOUND THEN RAISE EXCEPTION 'verified measurement revision required'; END IF;
 IF variant<>'' AND EXISTS(SELECT 1 FROM product_variants v WHERE v.id::text=variant AND v.product_id=product AND (v.length_mm IS NOT NULL OR v.width_mm IS NOT NULL OR v.height_mm IS NOT NULL OR v.weight_grams IS NOT NULL)) THEN
  SELECT v.measurement_revision_id,v.measurement_status,v.length_mm,v.width_mm,v.height_mm,v.weight_grams INTO reference,verification,l,w,h,g FROM product_variants v WHERE v.id::text=variant AND v.product_id=product;
  IF r.scope<>'sku' OR r.variant_id<>variant THEN RAISE EXCEPTION 'SKU measurement identity mismatch'; END IF;
 ELSE
  IF variant<>'' AND NOT EXISTS(SELECT 1 FROM product_variants v WHERE v.id::text=variant AND v.product_id=product) THEN RAISE EXCEPTION 'foreign SKU measurement'; END IF;
  SELECT p.measurement_revision_id,p.measurement_status,p.length_mm,p.width_mm,p.height_mm,p.weight_grams INTO reference,verification,l,w,h,g FROM products p WHERE p.id=product;
  IF r.scope<>'product' OR r.variant_id<>'' THEN RAISE EXCEPTION 'parent measurement identity mismatch'; END IF;
 END IF;
 IF reference IS DISTINCT FROM r.id OR verification IS DISTINCT FROM 'verified' OR (l,w,h,g) IS DISTINCT FROM (r.length_mm,r.width_mm,r.height_mm,r.weight_grams)
    OR ceil(r.length_mm::numeric*r.width_mm*r.height_mm*units/1000)<>NEW.volume_cm3 OR r.weight_grams::numeric*units<>NEW.weight_grams THEN RAISE EXCEPTION 'stale or inconsistent capacity measurement'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER putaway_measurement_reservation BEFORE INSERT OR UPDATE ON putaway_capacity_reservations FOR EACH ROW EXECUTE FUNCTION guard_putaway_measurement_reservation();
CREATE FUNCTION guard_putaway_placement_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.status='completed' AND OLD.status<>'completed' AND EXISTS(SELECT 1 FROM inbound_putaway_allocations a WHERE a.task_id=NEW.id)
 AND NOT EXISTS(SELECT 1 FROM warehouse_placements p JOIN putaway_capacity_reservations r ON r.task_id=p.task_id AND r.status='active' JOIN inventory_effective_measurements m ON m.product_id=p.product_id AND m.variant_id=p.variant_id AND m.verified AND m.revision=p.measurement_revision_id JOIN inbound_putaway_allocations a ON a.task_id=p.task_id AND a.supplier_id=p.supplier_id WHERE p.task_id=NEW.id AND p.product_id=NEW.product_id AND p.variant_id=COALESCE(NEW.variant_id::text,'') AND p.quantity=NEW.quantity AND p.bin_code=NEW.bin_location AND p.warehouse_id=NEW.warehouse_id AND p.volume_cm3=r.volume_cm3 AND p.weight_grams=r.weight_grams AND p.measurement_revision_id=r.measurement_revision_id) THEN RAISE EXCEPTION 'completion requires immutable capacity placement'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER putaway_placement_completion BEFORE UPDATE ON putaway_tasks FOR EACH ROW EXECUTE FUNCTION guard_putaway_placement_completion();
CREATE VIEW warehouse_measurement_reconciliation AS
 SELECT pt.id AS task_id,pt.product_id,pt.quantity,'historical_completed_placement_without_snapshot'::text AS exception FROM putaway_tasks pt WHERE pt.status='completed' AND NOT EXISTS(SELECT 1 FROM warehouse_placements p WHERE p.task_id=pt.id)
 UNION ALL SELECT pt.id,pt.product_id,pt.quantity,'pending_reservation_requires_measurement_reassignment'::text FROM putaway_tasks pt JOIN putaway_capacity_reservations r ON r.task_id=pt.id WHERE r.status='active' AND r.measurement_revision_id IS NULL;
