-- Execute with psql -X -v ON_ERROR_STOP=1 -f scripts/carton-creation-preflight.sql.
-- Read-only and executable before migration 202611140005. Review every result
-- with receipt/audit evidence; do not delete headers or merge received units.
BEGIN READ ONLY;
SELECT s.id AS orphan_shipment_id,s.supplier_id,s.status,s.created_at
FROM shipments s WHERE NOT EXISTS(SELECT 1 FROM shipment_items i WHERE i.shipment_id=s.id)
ORDER BY s.id;

SELECT shipment_id,variant_id,COUNT(*) AS duplicate_lines,
       jsonb_agg(jsonb_build_object('id',id,'expected_qty',expected_qty,'received_qty',received_qty) ORDER BY id) AS audit_lines
FROM shipment_items GROUP BY shipment_id,variant_id HAVING COUNT(*)>1
ORDER BY shipment_id,variant_id;

WITH signatures AS (
 SELECT s.id,s.supplier_id,s.supplier_warehouse_id,s.warehouse_id,
        jsonb_agg(jsonb_build_array(i.variant_id,i.expected_qty) ORDER BY i.variant_id,i.expected_qty) AS lines
 FROM shipments s JOIN shipment_items i ON i.shipment_id=s.id GROUP BY s.id
)
SELECT supplier_id,supplier_warehouse_id,warehouse_id,lines,
       array_agg(id ORDER BY id) AS matching_physical_claim_candidates
FROM signatures GROUP BY supplier_id,supplier_warehouse_id,warehouse_id,lines HAVING COUNT(*)>1
ORDER BY supplier_id,supplier_warehouse_id,warehouse_id;
COMMIT;
