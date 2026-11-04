-- 202610250006_refund_status.down.sql
-- Reverts 202610250006: removes 'refunded' from both payment-status CHECK
-- constraints. FAILS if any order / payment transaction is currently in
-- status 'refunded' (correct: the down migration must not silently orphan
-- refunded rows — move them to a legal status first).
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_status_check
    CHECK ((payment_status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'failed'::character varying, 'cancelled'::character varying, 'refund_pending'::character varying])::text[]));

ALTER TABLE payment_transactions DROP CONSTRAINT IF EXISTS payment_transactions_status_check;
ALTER TABLE payment_transactions ADD CONSTRAINT payment_transactions_status_check
    CHECK ((status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'failed'::character varying])::text[]));
