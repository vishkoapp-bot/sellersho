-- 202610250006_refund_status.up.sql
-- Money-fix W1 (2026-10-25), P0-6 / audit M-P0-4 (ledger F1):
-- MarkPaymentRefunded writes payment_status='refunded' on orders and
-- status='refunded' on payment_transactions, but NEITHER check constraint
-- allowed 'refunded', so the admin PSP-refund endpoint could never succeed:
-- the PSP refund executed first, then the status update failed and the
-- deferred rollback discarded the journal — money moved with no record,
-- and every retry re-refunded. Adding 'refunded' to both constraints.
-- All amounts in the system are integer Toman; this migration moves no money.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_check
    CHECK ((payment_status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'failed'::character varying, 'cancelled'::character varying, 'refund_pending'::character varying, 'refunded'::character varying])::text[]));

ALTER TABLE payment_transactions DROP CONSTRAINT IF EXISTS payment_transactions_status_check;
ALTER TABLE payment_transactions ADD CONSTRAINT payment_transactions_status_check
    CHECK ((status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'failed'::character varying, 'refunded'::character varying])::text[]));
