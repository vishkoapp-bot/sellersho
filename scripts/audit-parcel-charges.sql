-- Read-only FIN004 preflight AND postflight. Compatible with pre-160001 schema.
-- Any nonzero anomalies stop release. No rows are repaired by this audit.
BEGIN TRANSACTION READ ONLY;
WITH evidence AS (
 SELECT e.*,to_jsonb(e)->>'charge_component' AS component,
 p.return_request_id AS parcel_return,p.payment_method AS parcel_method,p.status AS parcel_status,
 p.final_charge,p.confirmed_by,r.supplier_id AS owner,r.status AS parent_status,
 l.id AS ledger_id,l.target_account,l.target_type,l.type AS ledger_type,l.amount AS ledger_amount,
 l.operator,l.related_order_or_dispute,
 count(*) OVER(PARTITION BY e.ledger_entry_id) AS ledger_uses
 FROM supplier_return_financial_events e
 LEFT JOIN supplier_return_parcels p ON p.id=e.parcel_id
 LEFT JOIN supplier_return_requests r ON r.id=e.return_request_id
 LEFT JOIN ledger_entries l ON l.id=e.ledger_entry_id WHERE e.parcel_id IS NOT NULL
), anomalies AS (
 SELECT id FROM evidence WHERE parcel_return IS DISTINCT FROM return_request_id
 OR owner IS DISTINCT FROM supplier_id OR parcel_method IS DISTINCT FROM 'sender_pays'
 OR payment_method <> 'sender_pays' OR amount NOT BETWEEN 1 AND 9007199254740991
 OR ledger_id IS NULL OR target_account IS DISTINCT FROM supplier_id
 OR target_type IS DISTINCT FROM 'supplier' OR ledger_amount IS DISTINCT FROM amount
 OR ledger_type IS DISTINCT FROM CASE WHEN operation='debit' THEN 'debit' ELSE 'credit' END
 OR operator IS DISTINCT FROM actor_user_id OR related_order_or_dispute IS DISTINCT FROM return_request_id
 OR ledger_uses<>1 OR octet_length(btrim(idempotency_key)) NOT BETWEEN 1 AND 128
 OR (component IS NULL AND (operation<>'debit' OR parcel_status<>'confirmed'
 OR COALESCE(confirmed_by,'')='' OR confirmed_by IS DISTINCT FROM actor_user_id OR parent_status NOT IN ('approved','packed')))
), originals AS (
 SELECT parcel_id,count(*) FILTER(WHERE component IS DISTINCT FROM 'adjustment') AS n FROM evidence GROUP BY parcel_id
), totals AS (
 SELECT parcel_id,sum(CASE WHEN operation='debit' THEN amount ELSE -amount END) AS amount FROM evidence GROUP BY parcel_id
)
SELECT (SELECT count(*) FROM supplier_return_parcels) AS parcels,
 (SELECT count(*) FROM evidence) AS parcel_events,
 (SELECT count(*) FROM anomalies) AS invalid_bindings,
 (SELECT count(*) FROM originals WHERE n<>1) AS duplicate_originals,
 (SELECT count(*) FROM totals t JOIN supplier_return_parcels p ON p.id=t.parcel_id WHERE t.amount IS DISTINCT FROM p.final_charge) AS effective_bill_mismatches;
-- Conditional metadata reconciliation uses dynamic SQL solely so this same file
-- executes before the additive migration. No SQL here mutates any relation.
DO $$ DECLARE n bigint; BEGIN
 IF to_regclass('parcel_charge_receipts') IS NOT NULL THEN
 -- Distinguish durable zero-event receiver confirmations from held legacy rows.
 EXECUTE $audit$ SELECT count(*) FROM supplier_return_parcels p WHERE p.status NOT IN ('draft','cancelled') AND NOT EXISTS(SELECT 1 FROM parcel_charge_receipts c WHERE c.parcel_id=p.id AND c.kind='original') $audit$ INTO n;
 RAISE NOTICE 'parcel charge held legacy confirmations: %',n;
 EXECUTE $audit$
 SELECT count(*) FROM supplier_return_financial_events e
 LEFT JOIN parcel_charge_receipts c ON c.event_id=e.id
 WHERE e.parcel_id IS NOT NULL AND (c.id IS NULL OR c.parcel_id<>e.parcel_id
 OR c.return_request_id<>e.return_request_id OR c.supplier_id<>e.supplier_id
 OR c.payment_method<>e.payment_method OR c.kind<>e.charge_component
 OR (c.kind='original' AND c.final_amount<>e.amount)
 OR (c.kind='adjustment' AND (abs(c.final_amount-c.expected_amount)<>e.amount
 OR e.operation<>CASE WHEN c.final_amount>c.expected_amount THEN 'debit' ELSE 'reversal' END)))
 $audit$ INTO n;
 RAISE NOTICE 'parcel charge receipt/event mismatches: %',n;
 IF n<>0 THEN RAISE EXCEPTION 'parcel charge reconciliation required'; END IF;
 EXECUTE $audit$
 SELECT count(*) FROM supplier_return_parcels p
 JOIN LATERAL (SELECT * FROM parcel_charge_receipts WHERE parcel_id=p.id ORDER BY sequence DESC LIMIT 1) c ON true
 WHERE p.return_request_id<>c.return_request_id OR p.payment_method<>c.payment_method
 OR COALESCE(p.final_charge,0)<>c.final_amount OR (p.final_charge IS NULL)<>c.amount_was_null
 OR COALESCE(p.final_charge_source,'')<>c.source OR COALESCE(p.final_charge_reference,'')<>c.reference
 $audit$ INTO n;
 RAISE NOTICE 'parcel charge effective receipt mismatches: %',n;
 IF n<>0 THEN RAISE EXCEPTION 'parcel charge reconciliation required'; END IF;
 END IF;
END $$;
COMMIT;
