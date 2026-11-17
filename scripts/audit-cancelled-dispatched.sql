-- INV-01: read-only, itemized reconciliation candidates. Run in a read-only
-- transaction; export results for human reconciliation. NEVER subtract stock:
-- restored units may have been resold. Historical flag-only captures cannot
-- establish exact restored quantities and are explicitly marked as uncertain.
BEGIN READ ONLY;
SELECT o.id AS order_id,o.order_number,o.payment_status,o.shipped_at AS order_shipped_at,
       oi.id AS order_item_id,oi.product_id,oi.variant_id,oi.quantity AS ordered_quantity,
       c.quantity AS captured_quantity,c.restored_quantity,
       o.inventory_captured AS historical_capture_flag,
       CASE WHEN c.restored_quantity>0 THEN 'recorded_restore_after_dispatch'
            ELSE 'legacy_capture_restore_unproven' END AS evidence,
       (SELECT jsonb_agg(jsonb_build_object('shipment_id',s.id,'supplier_id',s.supplier_id,
                 'status',s.status,'shipped_at',s.shipped_at,'delivered_at',s.delivered_at) ORDER BY s.id)
        FROM supplier_shipments s WHERE s.order_id=o.id AND
          (s.shipped_at IS NOT NULL OR s.delivered_at IS NOT NULL OR s.status IN ('shipped','in_transit','delivered'))) AS dispatched_supplier_legs
FROM orders o
JOIN order_items oi ON oi.order_id=o.id
LEFT JOIN order_inventory_captures c ON c.order_item_id=oi.id::text
WHERE o.status='cancelled'
 AND (o.shipped_at IS NOT NULL OR EXISTS(SELECT 1 FROM supplier_shipments s WHERE s.order_id=o.id AND
       (s.shipped_at IS NOT NULL OR s.delivered_at IS NOT NULL OR s.status IN ('shipped','in_transit','delivered'))))
 AND (c.restored_quantity>0 OR (c.order_item_id IS NULL AND o.inventory_captured))
ORDER BY o.id,oi.id;
COMMIT;
