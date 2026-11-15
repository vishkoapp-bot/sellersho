-- Read-only evidence for physical counts. Do not apply stock/counter corrections
-- from this report. Set a transaction-local statement_timeout (e.g.10s).
-- Optional sellersho.audit_warehouse_id narrows a warehouse; empty means all.
-- Continue after the last bin_code with sellersho.audit_after_bin_code.
WITH candidates AS MATERIALIZED (
 SELECT bin_code FROM warehouse_bins
 WHERE bin_code COLLATE "C">COALESCE(current_setting('sellersho.audit_after_bin_code',TRUE),'') COLLATE "C"
 AND (COALESCE(current_setting('sellersho.audit_warehouse_id',TRUE),'')='' OR warehouse_id=COALESCE(current_setting('sellersho.audit_warehouse_id',TRUE),''))
 ORDER BY bin_code COLLATE "C" LIMIT 501
), page AS (
 SELECT bin_code FROM candidates ORDER BY bin_code COLLATE "C" LIMIT 500
)
SELECT a.bin_code,a.warehouse_id,a.capacity_status,to_jsonb(a) AS physical_evidence,
 a.requires_reconciliation,
 CASE WHEN a.requires_reconciliation THEN 'counted_reconciliation_required'
      WHEN a.capacity_status='verified' THEN 'matching_measured_baseline'
      ELSE 'capacity_verification_pending' END AS next_step,
 (SELECT COUNT(*)>500 FROM candidates) AS has_more
FROM page p JOIN warehouse_occupancy_audit a USING(bin_code)
ORDER BY a.bin_code COLLATE "C";
