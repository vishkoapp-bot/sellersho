DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM order_coupon_reservations) OR EXISTS(SELECT 1 FROM order_coupon_events) OR EXISTS(SELECT 1 FROM coupon_reconciliation_issues) THEN RAISE EXCEPTION 'FIN007 populated evidence: downgrade refused'; END IF;
END $$;
DROP TRIGGER fin007_order_fence ON orders;
DROP TRIGGER fin007_shipment_fence ON supplier_shipments;
DROP TRIGGER fin007_order_consistency ON orders;
DROP TRIGGER fin007_payment_fence ON payment_transactions;
DROP TRIGGER fin007_inquiry_fence ON order_payment_verifications;
DROP TRIGGER fin007_receipt_fence ON order_payment_receipts;
DROP TRIGGER fin007_receipt_consistency ON order_payment_receipts;
DROP TRIGGER fin007_verification_event_fence ON order_payment_verification_events;
DROP TRIGGER fin007_coupon_terms ON seller_discount_codes;
DROP TRIGGER fin007_count_consistency ON seller_discount_codes;
DROP TABLE order_coupon_events,order_coupon_reservations,coupon_reconciliation_issues;
DROP FUNCTION fin007_consistency(),fin007_ledger_fence(),fin007_payment_fence(),fin007_order_fence(),fin007_coupon_terms(),fin007_shipment_fence();
ALTER TABLE orders DROP COLUMN coupon_discount_amount;
ALTER TABLE seller_discount_codes DROP COLUMN revision;
