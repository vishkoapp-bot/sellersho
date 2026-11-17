-- Run scripts/carton-creation-preflight.sql before release. No history is
-- deleted, coalesced, assigned today's origin, or given an invented request key.
-- Duplicate lines require explicit audit-backed reconciliation before this
-- migration may install the final uniqueness constraint.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shipment_items GROUP BY shipment_id,variant_id HAVING COUNT(*)>1) THEN
  RAISE EXCEPTION 'carton duplicate lines require explicit reconciliation; run scripts/carton-creation-preflight.sql';
 END IF;
END $$;

ALTER TABLE shipments ADD COLUMN IF NOT EXISTS creation_key VARCHAR(128);
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS request_fingerprint VARCHAR(64);
ALTER TABLE shipments ADD CONSTRAINT carton_creation_identity_complete CHECK (
 (creation_key IS NULL AND request_fingerprint IS NULL) OR
 (creation_key IS NOT NULL AND length(btrim(creation_key)) BETWEEN 1 AND 128
  AND request_fingerprint IS NOT NULL AND request_fingerprint ~ '^[0-9a-f]{64}$')
);
CREATE UNIQUE INDEX carton_creation_key_unique ON shipments(supplier_id,creation_key) WHERE creation_key IS NOT NULL;
CREATE UNIQUE INDEX carton_variant_unique ON shipment_items(shipment_id,variant_id);

-- These are review candidates, not proof that two cartons are one physical
-- shipment. Repeated legitimate cartons can carry identical SKU quantities.
CREATE VIEW carton_creation_reconciliation AS
WITH signatures AS (
 SELECT s.id,s.supplier_id,s.supplier_warehouse_id,s.warehouse_id,
        COUNT(i.variant_id) AS line_count,
        COALESCE(jsonb_agg(jsonb_build_array(i.variant_id,i.expected_qty) ORDER BY i.variant_id,i.expected_qty)
                 FILTER(WHERE i.variant_id IS NOT NULL),'[]'::jsonb) AS lines
 FROM shipments s LEFT JOIN shipment_items i ON i.shipment_id=s.id
 GROUP BY s.id
), claims AS (
 SELECT *,COUNT(*) OVER(PARTITION BY supplier_id,supplier_warehouse_id,warehouse_id,lines) AS matching_claims FROM signatures
)
SELECT id AS shipment_id,'orphan_header'::text AS reason FROM claims WHERE line_count=0
UNION ALL
SELECT id,'matching_physical_claim_review' FROM claims WHERE line_count>0 AND matching_claims>1;
