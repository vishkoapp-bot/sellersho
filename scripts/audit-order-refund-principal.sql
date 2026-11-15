-- FIN-003: read-only, no inferred PSP payouts or automatic historical credits.
-- At most 1000 source rows per page. To continue, set the two session settings
-- sellersho.refund_audit_after_order / sellersho.refund_audit_after_payment to
-- the last returned order_id / payment_id, then rerun this script.
-- Do not export runtime credentials, gateway tokens or provider references.
BEGIN READ ONLY;
SELECT o.id AS order_id,o.order_number,o.status AS fulfillment_status,
       o.payment_status AS recorded_order_payment_status,
       COALESCE(p.id,'') AS payment_id,p.status AS recorded_payment_status,
       p.amount AS recorded_source_toman,r.amount_toman AS verified_source_toman,
       r.funding_kind,
       COALESCE((SELECT SUM(c.amount_toman) FROM order_refund_obligations c
                 WHERE c.source_payment_id=p.id),0) AS reserved_source_toman,
       (SELECT COUNT(*) FROM refunds f WHERE f.order_id=o.id) AS old_psp_claims,
       (SELECT COUNT(*) FROM customer_returns cr WHERE cr.order_id=o.id AND cr.status='refunded'
          AND NOT EXISTS(SELECT 1 FROM order_refund_obligations c WHERE c.purpose='customer_return' AND c.business_id=cr.id::text)) AS old_unproved_returns,
       CASE WHEN r.id IS NULL THEN 'legacy_funding_unverified'
            WHEN r.funding_kind='simulated' THEN 'simulated_not_real_cash'
            WHEN NOT EXISTS(SELECT 1 FROM order_refund_obligations c WHERE c.source_payment_id=p.id)
              THEN 'verified_source_requires_reconciliation'
            WHEN EXISTS(SELECT 1 FROM order_refund_obligations c WHERE c.source_payment_id=p.id AND c.destination='review')
              THEN 'reserved_for_historical_review'
            ELSE 'source_has_retained_refund_reservation' END AS evidence
FROM orders o
LEFT JOIN payment_transactions p ON p.order_id=o.id
LEFT JOIN order_payment_receipts r ON r.payment_id=p.id
WHERE (o.status='cancelled' AND (p.status IN ('paid','refunded') OR r.id IS NOT NULL OR o.payment_status IN ('paid','refunded'))
       OR p.compensation_capture
       OR EXISTS(SELECT 1 FROM refunds f WHERE f.order_id=o.id)
       OR EXISTS(SELECT 1 FROM order_payment_verifications v JOIN order_payment_verification_events e ON e.verification_id=v.id
                 WHERE v.order_id=o.id AND e.status='amount_mismatch'))
  AND (o.id COLLATE "C",COALESCE(p.id,'') COLLATE "C")>
      (COALESCE(current_setting('sellersho.refund_audit_after_order',TRUE),'') COLLATE "C",
       COALESCE(current_setting('sellersho.refund_audit_after_payment',TRUE),'') COLLATE "C")
ORDER BY o.id COLLATE "C",COALESCE(p.id,'') COLLATE "C"
LIMIT 1000;
COMMIT;
