-- Read-only, bounded companion to audit-warehouse-occupancy.sql. These source
-- identities cannot justify a numerical repair or assignment to a guessed bin.
-- Continue with sellersho.audit_after_occupancy_source set to the last source_key.
WITH sources AS (
 SELECT 'capture:'||order_item_id||':'||COALESCE(missing_balance_id,'') AS source_key,
        'capture_balance_identity_missing'::text AS exception,to_jsonb(a) AS evidence
 FROM warehouse_occupancy_orphan_allocations a
 UNION ALL
 SELECT 'departure:'||event_type||':'||event_id||':'||balance_id,
        reason,to_jsonb(u) FROM warehouse_departure_unresolved_sources u
), candidates AS MATERIALIZED (
 SELECT * FROM sources
 WHERE source_key COLLATE "C">COALESCE(current_setting('sellersho.audit_after_occupancy_source',TRUE),'') COLLATE "C"
 ORDER BY source_key COLLATE "C" LIMIT 501
)
SELECT source_key,exception,evidence,(SELECT COUNT(*)>500 FROM candidates) AS has_more
FROM candidates ORDER BY source_key COLLATE "C" LIMIT 500;
