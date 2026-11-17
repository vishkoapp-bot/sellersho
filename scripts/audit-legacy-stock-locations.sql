-- Run in a read-only transaction. Evidence for counted reconciliation only.
-- No balance subtraction/backfill is justified by this report alone.
-- First page: leave sellersho.audit_after_product_id unset/empty. To continue,
-- set it transaction-locally to the last returned product_id. has_more says
-- whether another page exists; IDs use the same bytewise order on every page.
WITH legacy_candidates AS MATERIALIZED (
 SELECT id,supplier_id,stock FROM products
 WHERE schema_version<2
   AND id COLLATE "C">COALESCE(current_setting('sellersho.audit_after_product_id',TRUE),'') COLLATE "C"
 ORDER BY id COLLATE "C" LIMIT 501
), legacy_page AS (
 SELECT * FROM legacy_candidates ORDER BY id COLLATE "C" LIMIT 500
)
SELECT p.id AS product_id,p.supplier_id,p.stock AS projection,
       COALESCE(l.sellable,0) AS recorded_sellable,
       COALESCE(l.lots,'[]'::jsonb) AS physical_lots,
       COALESCE(m.events,'[]'::jsonb) AS recent_inventory_events,
       COALESCE(a.events,'[]'::jsonb) AS candidate_supplier_audits,
       'counted_reconciliation_required'::text AS next_step,
       (SELECT COUNT(*)>500 FROM legacy_candidates) AS has_more
FROM legacy_page p
LEFT JOIN LATERAL (
 SELECT SUM(b.quantity) FILTER(WHERE b.status='sellable') AS sellable,
        jsonb_agg(jsonb_build_object('balanceId',b.id,'supplierId',b.supplier_id,
          'variantId',b.variant_id,'warehouseId',b.warehouse_id,
          'supplierWarehouseId',b.supplier_warehouse_id,'location',b.location_key,
          'status',b.status,'quantity',b.quantity) ORDER BY b.id COLLATE "C") AS lots
 FROM inventory_balances b WHERE b.product_id=p.id
) l ON TRUE
LEFT JOIN LATERAL (
 SELECT jsonb_agg(to_jsonb(recent)) AS events FROM (
  SELECT id,created_at,quantity,from_status,to_status,from_location_key,to_location_key,
         reference_type,reference_id,idempotency_key,actor_user_id,metadata
  FROM inventory_movements WHERE product_id=p.id ORDER BY created_at DESC,id DESC LIMIT 25
 ) recent
) m ON TRUE
LEFT JOIN LATERAL (
 -- Historical details are plain text and may not be valid JSON. These are
 -- candidates for human review, never machine-derived inventory evidence.
 SELECT jsonb_agg(to_jsonb(recent)) AS events FROM (
  SELECT id,timestamp,action,details FROM audit_logs
  WHERE operator='supplier:'||p.supplier_id
    AND action IN ('supplier_stock_update','supplier_sku_stock_update')
    AND POSITION(p.id IN details)>0 ORDER BY timestamp DESC,id DESC LIMIT 25
 ) recent
) a ON TRUE
ORDER BY p.id COLLATE "C";
