-- FIN-014 dry-run finance reconciliation report (read-only SQL mirror).
--
-- This is the psql-runnable mirror of GetFIN014Report
-- (backend/internal/repository/fin014_report.go). The Go implementation
-- is the canonical detector source; this script exists so a reviewer
-- can run the same eight detectors from psql without building the
-- binary. If the two ever disagree, the Go report wins and this file
-- must be updated in the same change.
--
-- Usage (disposable DB ONLY — the guard block below refuses any DB
-- without the sellersho_test_guard marker):
--   psql "$FIN014_DATABASE_URL" -v prefix='f14f-' -f scripts/fin014-report.sql
--   psql "$FIN014_DATABASE_URL" -v prefix=''     -f scripts/fin014-report.sql
--
-- Read-only: only SELECTs. No history rewrite, no cash movement, and
-- a simulated receipt is never labelled real cash (cash_basis
-- 'simulated' / 'unverified' / 'unknown' must never fund a
-- payout/refund; only 'verified_gateway' is real cash).
-- FIN-011 asymmetry is pinned, not fixed (audit §8 Q6): oracle check
-- 7 is expected to fail and is recorded in the report note, never
-- edited away.

\pset pager off
\set ON_ERROR_STOP on

-- Disposable-guard: refuse any DB without the test marker (same rule
-- as backend/internal/testdb: name sellersho_test_* + guard row).
DO $$
DECLARE
  dbname text := current_database();
  marker text;
BEGIN
  IF dbname NOT LIKE 'sellersho_test_%' THEN
    RAISE EXCEPTION 'fin014-report.sql refuses non-disposable database %', dbname;
  END IF;
  SELECT database_name INTO marker FROM public.sellersho_test_guard WHERE singleton = TRUE;
  IF marker IS DISTINCT FROM dbname THEN
    RAISE EXCEPTION 'fin014-report.sql refuses unmarked database % (guard marker missing or mismatched)', dbname;
  END IF;
END $$;

-- The per-order cash-basis expression (shared by classes 2, 4, 7).
-- Mirrors fin014CashBasisSQL in fin014_report.go.
WITH cash_basis AS (
  SELECT o.id AS order_id,
    CASE
      WHEN EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id = o.id AND r.funding_kind = 'gateway') THEN 'verified_gateway'
      WHEN EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id = o.id AND r.funding_kind = 'simulated') THEN 'simulated'
      WHEN EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id = o.id) THEN 'unverified'
      ELSE 'unknown'
    END AS basis
  FROM orders o
),
rows AS (
  -- Class 1: payout_oversubscription ---------------------------------
  SELECT 'payout_oversubscription' AS class, p.id AS record_id, 'payout_request' AS record_kind,
    p.store_id AS store_id, NULL::text AS order_id, p.amount AS amount_toman,
    'unverified' AS cash_basis, 'certain' AS confidence,
    'payout request exceeds signed availability (seller ledger net minus other pending/approved payouts)' AS explanation,
    ARRAY['payout:' || p.id, 'store:' || p.store_id] AS evidence
  FROM payout_requests p
  WHERE p.id LIKE :'prefix' || '%' AND p.status IN ('pending', 'approved')
    AND p.amount > (
      SELECT COALESCE(SUM(CASE WHEN l.type = 'credit' THEN l.amount ELSE -l.amount END), 0)
      FROM ledger_entries l WHERE l.target_account = p.store_id AND l.target_type = 'seller')
    - (SELECT COALESCE(SUM(p2.amount), 0) FROM payout_requests p2
       WHERE p2.store_id = p.store_id AND p2.id <> p.id AND p2.status IN ('pending', 'approved'))
  UNION ALL
  SELECT 'payout_oversubscription', i.id, 'settlement_item',
    i.store_id, NULL, i.net_amount,
    'unverified', 'certain',
    'settlement item net exceeds the store released profit for the period',
    ARRAY['settlement_item:' || i.id, 'cycle:' || i.cycle_period]
  FROM monthly_settlement_items i
  WHERE i.id LIKE :'prefix' || '%' AND i.net_amount > (
    SELECT COALESCE(SUM(o.dropshipper_profit), 0) FROM orders o
    WHERE o.store_id = i.store_id AND o.profit_released)
  UNION ALL
  -- Class 2: paid_cancellation_no_refund ------------------------------
  SELECT 'paid_cancellation_no_refund', o.id, 'order',
    o.store_id, o.id, o.payable_amount,
    cb.basis, 'certain',
    'paid order cancelled with no cancellation refund obligation',
    ARRAY['order:' || o.id]
  FROM orders o JOIN cash_basis cb ON cb.order_id = o.id
  WHERE o.id LIKE :'prefix' || '%' AND o.payment_status = 'paid' AND o.status = 'cancelled'
    AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id = o.id AND r.purpose = 'cancellation')
  UNION ALL
  SELECT 'paid_cancellation_no_refund', o.id, 'order',
    o.store_id, o.id, o.payable_amount,
    cb.basis, 'certain',
    'paid order cancelled with duplicate cancellation refund obligations',
    ARRAY['order:' || o.id]
  FROM orders o JOIN cash_basis cb ON cb.order_id = o.id
  WHERE o.id LIKE :'prefix' || '%' AND o.payment_status = 'paid' AND o.status = 'cancelled'
    AND (SELECT COUNT(*) FROM order_refund_obligations r WHERE r.order_id = o.id AND r.purpose = 'cancellation') > 1
  UNION ALL
  SELECT 'paid_cancellation_no_refund', o.id, 'order',
    o.store_id, o.id, o.payable_amount,
    cb.basis, 'unresolved',
    'cancellation refund obligation exists but its outcome is unknown',
    ARRAY['order:' || o.id]
  FROM orders o JOIN order_refund_obligations r ON r.order_id = o.id AND r.purpose = 'cancellation'
  JOIN cash_basis cb ON cb.order_id = o.id
  WHERE o.id LIKE :'prefix' || '%' AND o.payment_status = 'paid' AND o.status = 'cancelled' AND r.status = 'unknown'
  UNION ALL
  -- Class 3: duplicate_parcel_charge ----------------------------------
  SELECT 'duplicate_parcel_charge', pr.id, 'parcel',
    NULL, NULL,
    (SELECT COALESCE(SUM(e.amount), 0) FROM supplier_return_financial_events e WHERE e.parcel_id = pr.id AND e.operation = 'debit'),
    'unknown', 'certain',
    'parcel charge anomaly: duplicate debit, missing ledger leg, or shared ledger entry — no correction recorded',
    ARRAY['parcel:' || pr.id]
  FROM supplier_return_parcels pr WHERE pr.id LIKE :'prefix' || '%'
    AND EXISTS (
      SELECT 1 FROM supplier_return_financial_events e WHERE e.parcel_id = pr.id AND e.operation = 'debit'
      AND (
        (SELECT COUNT(*) FROM supplier_return_financial_events e2 WHERE e2.parcel_id = pr.id AND e2.operation = 'debit' AND e2.amount = e.amount)
        - (SELECT COUNT(*) FROM supplier_return_financial_events e3 WHERE e3.parcel_id = pr.id AND e3.operation = 'reversal' AND e3.amount = e.amount) > 1
        OR (e.ledger_entry_id IS NULL AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%' || e.id || '%'))
        OR (e.ledger_entry_id IS NOT NULL
          AND (SELECT COUNT(*) FROM supplier_return_financial_events e4 WHERE e4.ledger_entry_id = e.ledger_entry_id AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%' || e4.id || '%')) > 1
          AND NOT EXISTS (SELECT 1 FROM ledger_entries l WHERE l.reason LIKE '%' || e.id || '%'))
      )
    )
  UNION ALL
  -- Class 4: resolved_but_held_dispute --------------------------------
  SELECT 'resolved_but_held_dispute', d.id, 'dispute',
    d.store_id, d.order_id, d.claim_amount,
    cb.basis, 'certain',
    'dispute resolved in the seller favour (release_to_seller) with an audited review row, but the order profit was never released',
    ARRAY['dispute:' || d.id, 'order:' || d.order_id]
  FROM disputes d JOIN orders o ON o.id = d.order_id
  JOIN cash_basis cb ON cb.order_id = d.order_id
  WHERE d.id LIKE :'prefix' || '%' AND d.status = 'resolved' AND d.resolution_action = 'release_to_seller'
    AND NOT o.profit_released
    AND EXISTS (SELECT 1 FROM audit_logs a WHERE a.action = 'FIN005 reviewed legacy evidence' AND a.details LIKE '%' || d.id || '%')
  UNION ALL
  SELECT 'resolved_but_held_dispute', d.id, 'dispute',
    d.store_id, d.order_id, d.claim_amount,
    cb.basis, 'certain',
    'dispute resolved with a refund verdict but no refund obligation was recorded',
    ARRAY['dispute:' || d.id, 'order:' || d.order_id]
  FROM disputes d JOIN orders o ON o.id = d.order_id
  JOIN cash_basis cb ON cb.order_id = d.order_id
  WHERE d.id LIKE :'prefix' || '%' AND d.status = 'resolved' AND d.resolution_action LIKE 'refund%'
    AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id = d.order_id AND r.purpose = 'dispute' AND r.business_id = d.id)
  UNION ALL
  SELECT 'resolved_but_held_dispute', d.id, 'dispute',
    d.store_id, d.order_id, d.claim_amount,
    cb.basis, 'ambiguous',
    'dispute resolved in the seller favour but no audited review row exists (review_provenance_missing) — held for reviewer',
    ARRAY['dispute:' || d.id, 'order:' || d.order_id]
  FROM disputes d JOIN orders o ON o.id = d.order_id
  JOIN cash_basis cb ON cb.order_id = d.order_id
  WHERE d.id LIKE :'prefix' || '%' AND d.status = 'resolved' AND d.resolution_action = 'release_to_seller'
    AND NOT o.profit_released
    AND NOT EXISTS (SELECT 1 FROM audit_logs a WHERE a.action = 'FIN005 reviewed legacy evidence' AND a.details LIKE '%' || d.id || '%')
  UNION ALL
  -- Class 5: storage_history_gap --------------------------------------
  SELECT 'storage_history_gap', l.id, 'receipt_lot',
    NULL, NULL, 0,
    'unknown', 'unresolved',
    'receipt lot history is unresolved (' || l.history_status || '): charges cannot be re-derived from receipt facts — never guessed',
    ARRAY['lot:' || l.id, 'status:' || l.history_status]
  FROM warehouse_storage_receipt_lots l WHERE l.id LIKE :'prefix' || '%' AND l.history_status <> 'verified'
  UNION ALL
  SELECT 'storage_history_gap', p.id, 'storage_position',
    NULL, NULL,
    (SELECT COALESCE(SUM(c.amount_toman), 0) FROM warehouse_storage_fee_charges c WHERE c.position_id = p.id),
    'unknown', 'unresolved',
    'storage position has fee charges but no receipt lot — charges cannot be re-derived from receipt facts, never guessed',
    ARRAY['position:' || p.id]
  FROM warehouse_storage_positions p WHERE p.id LIKE :'prefix' || '%'
    AND EXISTS (SELECT 1 FROM warehouse_storage_fee_charges c WHERE c.position_id = p.id)
    AND NOT EXISTS (SELECT 1 FROM warehouse_storage_receipt_lots l WHERE l.id = p.id)
  UNION ALL
  SELECT 'storage_history_gap', c.id, 'storage_charge',
    NULL, NULL, c.amount_toman,
    'unknown', 'certain',
    'storage charge violates the per-position-per-day uniqueness or positivity rule (FIN-013 oracle check 8)',
    ARRAY['charge:' || c.id, 'position:' || c.position_id]
  FROM warehouse_storage_fee_charges c WHERE c.id LIKE :'prefix' || '%'
    AND (c.amount_toman <= 0 OR c.qty <= 0 OR EXISTS (SELECT 1 FROM warehouse_storage_fee_charges c2 WHERE c2.position_id = c.position_id AND c2.fee_date = c.fee_date AND c2.id <> c.id))
  UNION ALL
  -- Class 6: coupon_oversubscription -----------------------------------
  SELECT 'coupon_oversubscription', c.id, 'coupon',
    c.store_id, NULL, NULL,
    'unknown', 'certain',
    'coupon uses exceed max_uses (used_count > max_uses)',
    ARRAY['coupon:' || c.id]
  FROM seller_discount_codes c
  WHERE c.id LIKE :'prefix' || '%' AND c.max_uses IS NOT NULL AND c.used_count > c.max_uses
  UNION ALL
  SELECT 'coupon_oversubscription', c.id, 'coupon',
    c.store_id, NULL, NULL,
    'unknown', 'certain',
    'coupon used_count disagrees with occupying reservations (reserved/consumed/review_hold)',
    ARRAY['coupon:' || c.id]
  FROM seller_discount_codes c
  WHERE c.id LIKE :'prefix' || '%'
    AND c.used_count <> (SELECT COUNT(*) FROM order_coupon_reservations r WHERE r.coupon_id = c.id AND r.state IN ('reserved', 'consumed', 'review_hold'))
  UNION ALL
  -- Class 7: statement_mismatch ---------------------------------------
  SELECT 'statement_mismatch', o.id, 'order',
    o.store_id, o.id, o.dropshipper_profit,
    cb.basis, 'certain',
    'released profit has a live dispute/admin refund obligation with no compensating seller-profit reversal',
    ARRAY['order:' || o.id]
  FROM orders o JOIN cash_basis cb ON cb.order_id = o.id
  WHERE o.id LIKE :'prefix' || '%' AND o.profit_released AND o.status IN ('delivered', 'delivered_confirmed')
    AND EXISTS (SELECT 1 FROM order_refund_obligations r WHERE r.order_id = o.id AND r.purpose IN ('dispute', 'admin_refund')
      AND COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id = r.id ORDER BY e.event_no DESC LIMIT 1), r.status) IN ('pending', 'succeeded', 'unknown'))
    AND NOT EXISTS (SELECT 1 FROM seller_profit_reversals s WHERE s.order_id = o.id)
  UNION ALL
  SELECT 'statement_mismatch', o.id, 'order',
    o.store_id, o.id, o.dropshipper_profit,
    cb.basis, 'ambiguous',
    'released profit is invisible to the statement builder (order status is outside the released_profit projection) — held for reviewer to confirm delivery',
    ARRAY['order:' || o.id]
  FROM orders o JOIN cash_basis cb ON cb.order_id = o.id
  WHERE o.id LIKE :'prefix' || '%' AND o.profit_released AND o.status NOT IN ('delivered', 'delivered_confirmed')
  UNION ALL
  SELECT 'statement_mismatch', s.store_id || ':2026-10', 'store_month',
    s.store_id, NULL, 0,
    'unknown', 'ambiguous',
    'statement opening for 2026-10 is unverified (no FIN-014 cutover record) — opening balance cannot be trusted',
    ARRAY['store:' || s.store_id, 'month:2026-10']
  FROM (SELECT DISTINCT target_account AS store_id FROM ledger_entries
        WHERE target_account LIKE :'prefix' || '%' AND target_type = 'seller' AND date < '2026-10-01'::timestamptz) s
  UNION ALL
  -- Class 8: uncertain_psp_outcome ------------------------------------
  SELECT 'uncertain_psp_outcome', o.id, 'order',
    o.store_id, o.id, o.payable_amount,
    'unknown', 'unresolved',
    'paid order has no payment record at all (legacy pre-payment-model shape)',
    ARRAY['order:' || o.id]
  FROM orders o WHERE o.id LIKE :'prefix' || '%' AND o.payment_status = 'paid' AND o.status <> 'cancelled'
    AND NOT EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id = o.id)
  UNION ALL
  SELECT 'uncertain_psp_outcome', o.id, 'order',
    o.store_id, o.id, o.payable_amount,
    'unverified', 'unresolved',
    'payment verification exists but no receipt was ever recorded — provider outcome unknown',
    ARRAY['order:' || o.id]
  FROM orders o WHERE o.id LIKE :'prefix' || '%' AND o.payment_status = 'paid' AND o.status <> 'cancelled'
    AND EXISTS (SELECT 1 FROM order_payment_verifications v WHERE v.order_id = o.id)
    AND NOT EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id = o.id)
  UNION ALL
  SELECT 'uncertain_psp_outcome', o.id, 'order',
    o.store_id, o.id, o.payable_amount,
    'simulated', 'ambiguous',
    'only a simulated receipt exists — simulated funding is not real cash',
    ARRAY['order:' || o.id]
  FROM orders o WHERE o.id LIKE :'prefix' || '%' AND o.payment_status = 'paid' AND o.status <> 'cancelled'
    AND EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id = o.id AND r.funding_kind = 'simulated')
    AND NOT EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id = o.id AND r.funding_kind = 'gateway')
  UNION ALL
  SELECT 'uncertain_psp_outcome', o.id, 'order',
    o.store_id, o.id, o.payable_amount,
    'unverified', 'unresolved',
    'paid payment exists but no verification and no receipt — manual-bridge shape, outcome unverified',
    ARRAY['order:' || o.id]
  FROM orders o WHERE o.id LIKE :'prefix' || '%' AND o.payment_status = 'paid' AND o.status <> 'cancelled'
    AND EXISTS (SELECT 1 FROM payment_transactions p WHERE p.order_id = o.id)
    AND NOT EXISTS (SELECT 1 FROM order_payment_verifications v WHERE v.order_id = o.id)
    AND NOT EXISTS (SELECT 1 FROM order_payment_receipts r WHERE r.order_id = o.id)
)
-- One statement: detail rows, then totals (same rows, counted once).
SELECT 'row' AS kind, class, record_id, record_kind, store_id, order_id, amount_toman,
  cash_basis, confidence, explanation, evidence
FROM rows
UNION ALL
SELECT 'total', 'TOTAL', COUNT(*)::text, '', '', '', NULL, '', '', '', '{}'::text[]
FROM rows
UNION ALL
SELECT 'total', 'certain', COUNT(*)::text, '', '', '', NULL, '', '', '', '{}'::text[] FROM rows WHERE confidence = 'certain'
UNION ALL
SELECT 'total', 'ambiguous', COUNT(*)::text, '', '', '', NULL, '', '', '', '{}'::text[] FROM rows WHERE confidence = 'ambiguous'
UNION ALL
SELECT 'total', 'unresolved', COUNT(*)::text, '', '', '', NULL, '', '', '', '{}'::text[] FROM rows WHERE confidence = 'unresolved'
ORDER BY 1, 2, 3;
