-- Run only against an explicitly selected review database. Read-only classification.
BEGIN READ ONLY;
SELECT kind,status,exception,COUNT(*) AS records FROM inbound_transfer_reconciliation GROUP BY kind,status,exception ORDER BY kind,status,exception;
SELECT * FROM inbound_transfer_reconciliation ORDER BY kind,shipment_id;
SELECT id,shipment_id,source_kind,quantity,dispatched_quantity,received_quantity,cancelled_quantity FROM inbound_inventory_lines ORDER BY shipment_id,id;
ROLLBACK;
