-- 202611050008 (down): drop the Tapin box catalogue and the
-- supplier_shipments Tapin registration columns. Any shipment that
-- was registered through Tapin keeps its carrier/tracking_number;
-- only the registration metadata is lost.
ALTER TABLE supplier_shipments
    DROP COLUMN IF EXISTS tapin_service,
    DROP COLUMN IF EXISTS tapin_box_id,
    DROP COLUMN IF EXISTS tapin_registered_at,
    DROP COLUMN IF EXISTS tapin_last_status;

DROP TABLE IF EXISTS tapin_boxes;
