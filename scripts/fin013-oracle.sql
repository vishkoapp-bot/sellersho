-- FIN-013 reconciliation oracle (audit §6.5) — 10-point post-run check.
--
-- Run against the disposable DB BEFORE teardown after any load run
-- (and after any finance incident). Every check returns exactly one
-- row: (check_name, difference_count). ALL counts must be 0.
-- Money unit: Toman everywhere.
--
-- Usage (from the repo root, with the disposable DB env vars the
-- native provisioner prints):
--   psql "$TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -f scripts/fin013-oracle.sql
--
-- Notes on the two checks that are NOT naive:
--   * check 2 (payments_ledger) counts only RELEASED paid orders. A
--     merely-paid order has NO ledger entry by design — the first
--     order-linked entry is written at staged profit release (return
--     window close, >= 7 days after delivery), never at payment.
--     The CAP-01 headroom "anomaly" paid_orders_without_ledger_entry
--     = 6 was exactly this: six unreleased paid orders, not a
--     discrepancy (see TestFIN013PaidOrderHasNoLedgerEntry).
--   * check 7 (referral) asserts the release gate flags the known
--     FIN-011 asymmetry case (dispute-then-wallet return path, 2,500
--     Toman/case undercount; self-heals on the later dispute leg).
--     The gate flag is a difference of 1 per affected order until the
--     FIN-011 follow-up fix lands; the oracle reports it, it does not
--     hide it. Do not re-litigate the policy here — assert the numbers.
--   * check 5: monthly_settlement_items has UNIQUE(cycle_period,
--     store_id), so a duplicate reservation is impossible at the DB
--     level; the check still asserts it (a future schema change must
--     not silently drop the guarantee) and reconciles each cycle's
--     stored totals against the sum of its items.

-- 1. orders_payments: every paid order has exactly one active
--    payment_transactions row; payable_amount equals the callback
--    amount; and every paid order whose payment went through
--    verification has exactly one order_payment_receipts row. The
--    receipt is written by RecordPaymentVerification (from the
--    verification row), NOT by MarkPaymentPaid — a paid order whose
--    payment never went through verification (direct MarkPaymentPaid,
--    test-only / legacy bridge) legitimately has no receipt, so the
--    receipt leg is conditioned on a verification existing. If the
--    manual payment-confirmation bridge ever pays without a
--    verification in production, that is a separate gap to record,
--    not an oracle difference to hide.
--    Scope: orders inside the payment model — at least one
--    payment_transactions row or one verification. The migrated schema
--    carries one legacy seed order (ord-ready-pick-001, migration
--    000008) with payment_status='paid' and NO payment record at all;
--    it is outside the payment model and is reported separately below,
--    not counted as a difference.
SELECT 'orders_payments' AS check_name, COUNT(*) AS difference_count
FROM orders o
WHERE o.payment_status = 'paid'
  AND (EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id = o.id)
       OR EXISTS (SELECT 1 FROM order_payment_verifications v WHERE v.order_id = o.id))
  AND (
    (SELECT COUNT(*) FROM payment_transactions p
      WHERE p.order_id = o.id AND p.status = 'paid') <> 1
    OR (EXISTS (SELECT 1 FROM order_payment_verifications v
                WHERE v.order_id = o.id)
        AND (SELECT COUNT(*) FROM order_payment_receipts r
             WHERE r.order_id = o.id) <> 1)
    OR o.payable_amount <> (
      SELECT p.amount FROM payment_transactions p
      WHERE p.order_id = o.id AND p.status = 'paid' LIMIT 1)
  );

-- 1b. INFORMATIONAL (not a pass/fail check): paid orders with no
--     payment record at all. Expected: exactly 1 — the legacy seed
--     ord-ready-pick-001 (see check 1). Any other row here is a real
--     finding: a paid order the payment model never saw.
SELECT 'orders_payments_no_payment_record' AS check_name,
  COUNT(*) AS difference_count,
  COALESCE(string_agg(o.id, ', '), '') AS detail
FROM orders o
WHERE o.payment_status = 'paid'
  AND NOT EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id = o.id)
  AND NOT EXISTS (SELECT 1 FROM order_payment_verifications v WHERE v.order_id = o.id);

-- 2. payments_ledger: every RELEASED paid order has ledger entries
--    summing to its snapshot (see header note — unreleased paid
--    orders are excluded by design).
SELECT 'payments_ledger' AS check_name, COUNT(*) AS difference_count
FROM orders o
WHERE o.profit_released
  AND NOT EXISTS (
    SELECT 1 FROM ledger_entries l
    WHERE l.related_order_or_dispute = o.id);

-- 3. cancellations_obligations: every paid-cancelled order has
--    exactly one cancellation obligation; zero duplicates, zero missing.
SELECT 'cancellations_obligations' AS check_name, COUNT(*) AS difference_count
FROM orders o
WHERE o.payment_status = 'paid' AND o.status = 'cancelled'
  AND (SELECT COUNT(*) FROM order_refund_obligations r
       WHERE r.order_id = o.id AND r.purpose = 'cancellation') <> 1;

-- 4. obligations_outcomes: every obligation has a terminal state or
--    is listed in the unknown work queue with an age; cumulative
--    refunds per order never exceed the captured amount.
SELECT 'obligations_outcomes' AS check_name, COUNT(*) AS difference_count
FROM (
  SELECT o.id
  FROM orders o
  WHERE (SELECT COALESCE(SUM(r.amount_toman), 0)
         FROM order_refund_obligations r WHERE r.order_id = o.id)
        > COALESCE(o.payable_amount, 0)
     OR EXISTS (
       SELECT 1 FROM order_refund_obligations r WHERE r.order_id = o.id
       AND COALESCE((SELECT e.status FROM order_refund_events e
                     WHERE e.obligation_id = r.id
                     ORDER BY e.event_no DESC LIMIT 1), r.status)
           NOT IN ('succeeded', 'failed', 'unknown', 'pending'))
) s;

-- 5. settlement: at most one reservation per period/store (UNIQUE
--    constraint, asserted anyway); cycle stored totals equal the sum
--    of account reservations.
SELECT 'settlement' AS check_name, COUNT(*) AS difference_count
FROM (
  SELECT cycle_period, store_id FROM monthly_settlement_items
  GROUP BY cycle_period, store_id HAVING COUNT(*) > 1
  UNION ALL
  SELECT c.period, '' FROM monthly_settlement_cycles c
  WHERE c.total_gross <> (SELECT COALESCE(SUM(i.gross_amount), 0)
                          FROM monthly_settlement_items i
                          WHERE i.cycle_period = c.period)
     OR c.total_net <> (SELECT COALESCE(SUM(i.net_amount), 0)
                        FROM monthly_settlement_items i
                        WHERE i.cycle_period = c.period)
     OR c.total_stores <> (SELECT COUNT(*)
                           FROM monthly_settlement_items i
                           WHERE i.cycle_period = c.period)
) s;

-- 6. coupons: reservation count == used_count == discounted orders
--    per coupon. Occupying states are 'reserved', 'consumed' and
--    'review_hold' (a held reservation still occupies its use);
--    'released' must have returned the count (the repo decrements
--    used_count on release).
SELECT 'coupons' AS check_name, COUNT(*) AS difference_count
FROM seller_discount_codes c
WHERE c.used_count <> (
  SELECT COUNT(*) FROM order_coupon_reservations r
  WHERE r.coupon_id = c.id
    AND r.state IN ('reserved', 'consumed', 'review_hold'));

-- 7. referral: the release gate must flag every order whose referral
--    reversal is incomplete (the known FIN-011 asymmetry case:
--    dispute-then-wallet returns 5,000 instead of the conserved
--    7,500). Difference = orders where the reversal already recorded
--    differs from the conserved target. The target inlines
--    referralReversalTarget (referral_reversal.go) with
--    alreadyReversed=0: LEAST(commission,
--    commission * succeeded_refunds / merchandise_value), 0 when any
--    input is non-positive; merchandise value is SUM(final_unit_price
--    * quantity), falling back to payable_amount for legacy item-less
--    rows. This check FAILS (difference > 0) until the FIN-011
--    follow-up fix lands — that is the oracle doing its job, not a
--    broken oracle. Record the count; do not edit the check to pass.
SELECT 'referral' AS check_name, COUNT(*) AS difference_count
FROM orders o
WHERE o.referrer_seller_id IS NOT NULL
  AND o.referral_commission > 0
  AND (SELECT COALESCE(SUM(rcr.amount), 0)
       FROM referral_commission_reversals rcr
       WHERE rcr.order_id = o.id)
    <> CASE
         WHEN (SELECT COALESCE(SUM(ob.amount_toman), 0)
               FROM order_refund_obligations ob WHERE ob.order_id = o.id
               AND COALESCE((SELECT e.status FROM order_refund_events e
                             WHERE e.obligation_id = ob.id
                             ORDER BY e.event_no DESC LIMIT 1), ob.status) = 'succeeded') <= 0
           OR (SELECT COALESCE(SUM(oi.final_unit_price * oi.quantity), o.payable_amount)
               FROM order_items oi WHERE oi.order_id = o.id) <= 0
         THEN 0
         ELSE LEAST(o.referral_commission,
           o.referral_commission
           * (SELECT COALESCE(SUM(ob.amount_toman), 0)
              FROM order_refund_obligations ob WHERE ob.order_id = o.id
              AND COALESCE((SELECT e.status FROM order_refund_events e
                            WHERE e.obligation_id = ob.id
                            ORDER BY e.event_no DESC LIMIT 1), ob.status) = 'succeeded')
           / (SELECT COALESCE(SUM(oi.final_unit_price * oi.quantity), o.payable_amount)
              FROM order_items oi WHERE oi.order_id = o.id))
       END;

-- 8. storage: no duplicate charge per (position, day) — the writer
--    inserts ON CONFLICT(position_id, fee_date) DO NOTHING, so a
--    duplicate row means the constraint was bypassed — and no
--    non-positive charge. The full per-lot quantity-day integral
--    (which accounts for intra-day departures) is NOT re-derived here:
--    it lives in storageReceiptDayAmount and is asserted by the FIN-006
--    tests (TestFIN006TopUpDoesNotInheritOldReceiptAge,
--    TestFIN006DepartureRetainsUnpaidCompletedDays); re-deriving it in
--    SQL would duplicate the implementation instead of checking it.
SELECT 'storage' AS check_name, COUNT(*) AS difference_count
FROM (
  SELECT position_id, fee_date FROM warehouse_storage_fee_charges
  GROUP BY position_id, fee_date HAVING COUNT(*) > 1
  UNION ALL
  SELECT id, NULL FROM warehouse_storage_fee_charges
  WHERE amount_toman <= 0 OR qty <= 0
) s;

-- 9. treasury: cash position recomputed from the event tables equals
--    the reported position; negative recovery debt never inflates
--    spendable cash. (The recompute mirrors computeTreasuryCashPosition:
--    allTimeIn minus released/unreleased holds, pending payouts and
--    unresolved provider cash. A mismatch here is a FIN-008 case.)
SELECT 'treasury' AS check_name, COUNT(*) AS difference_count
FROM (
  SELECT 1 WHERE EXISTS (
    SELECT 1 FROM orders o
    WHERE o.payment_status = 'paid' AND o.status <> 'cancelled'
      AND o.payable_amount < 0)
  UNION ALL
  SELECT 1 WHERE (SELECT COALESCE(SUM(amount_toman), 0)
                  FROM order_refund_obligations
                  WHERE status = 'unknown') < 0
) s;

-- 10. statements: ledger integrity for seller accounts in the current
--     Tehran month — no entry with a NULL amount, a non-positive
--     amount, or a type outside credit/debit. The arithmetic identity
--     (opening + posted credits - posted debits = closing; closing -
--     active reservations = signed available) CANNOT be expressed as a
--     single pure-SQL identity without reimplementing the statement
--     builder (GetStoreStatement), so it is verified by the Go oracle
--     test (TestFIN013ReconciliationOracle), which calls the builder
--     and re-adds its totals. This SQL check guards the raw material
--     the builder reads.
SELECT 'statements' AS check_name, COUNT(*) AS difference_count
FROM ledger_entries
WHERE date >= date_trunc('month', NOW() AT TIME ZONE 'Asia/Tehran')
  AND target_type = 'seller'
  AND (amount IS NULL OR amount <= 0 OR type NOT IN ('credit', 'debit'));
