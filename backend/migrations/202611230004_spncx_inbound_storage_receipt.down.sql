CREATE OR REPLACE FUNCTION guard_storage_receipt_lot_insert() RETURNS trigger LANGUAGE plpgsql AS $$
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
