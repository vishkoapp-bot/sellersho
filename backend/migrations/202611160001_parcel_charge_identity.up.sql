-- The old backend is still serving while this bounded migration transaction runs.
-- Reserve the supplier FK DDL lock first, allowing old account FOR UPDATE reads
-- (RowShareLock) to finish. ACCESS EXCLUSIVE here would trap discovery readers
-- holding a return AccessShareLock while waiting for that supplier guard.
LOCK TABLE suppliers IN SHARE ROW EXCLUSIVE MODE;
-- Drain old return/parcel row-lock holders before taking later evidence locks:
-- weaker SHARE modes allow FOR UPDATE holders to enter and then deadlock when
-- their later UPDATE upgrades to RowExclusiveLock. Ledger precedes events just
-- as the old debit writer does. All locks survive preflight through backfill and
-- transaction commit/rollback; the runner's existing timeouts bound this pause.
LOCK TABLE supplier_return_requests IN ACCESS EXCLUSIVE MODE;
LOCK TABLE supplier_return_parcels IN ACCESS EXCLUSIVE MODE;
LOCK TABLE ledger_entries IN ACCESS EXCLUSIVE MODE;
LOCK TABLE supplier_return_financial_events IN ACCESS EXCLUSIVE MODE;

-- Fail closed before mapping any historical parcel money.
DO $$ BEGIN
 IF EXISTS (
 SELECT 1 FROM supplier_return_financial_events e
 LEFT JOIN supplier_return_parcels p ON p.id=e.parcel_id
 LEFT JOIN supplier_return_requests r ON r.id=e.return_request_id
 LEFT JOIN ledger_entries l ON l.id=e.ledger_entry_id
 WHERE e.parcel_id IS NOT NULL AND (
 e.operation <> 'debit' OR p.id IS NULL OR r.id IS NULL OR p.return_request_id IS DISTINCT FROM e.return_request_id
 OR r.supplier_id IS DISTINCT FROM e.supplier_id OR p.payment_method <> 'sender_pays' OR e.payment_method <> 'sender_pays'
 OR p.status <> 'confirmed' OR COALESCE(p.confirmed_by,'')='' OR r.status NOT IN ('approved','packed')
 OR e.amount NOT BETWEEN 1 AND 9007199254740991 OR p.final_charge IS DISTINCT FROM e.amount
 OR l.id IS NULL OR l.target_account IS DISTINCT FROM e.supplier_id OR l.target_type IS DISTINCT FROM 'supplier'
 OR l.type IS DISTINCT FROM 'debit' OR l.amount IS DISTINCT FROM e.amount
 OR l.related_order_or_dispute IS DISTINCT FROM e.return_request_id
 OR l.operator IS DISTINCT FROM e.actor_user_id OR p.confirmed_by IS DISTINCT FROM e.actor_user_id
 OR (e.event_identity IS NOT NULL AND e.event_identity <> 'return-parcel-charge:'||e.parcel_id)
 OR (SELECT count(*) FROM supplier_return_financial_events x WHERE x.parcel_id=e.parcel_id) <> 1
 OR (SELECT count(*) FROM supplier_return_financial_events x WHERE x.ledger_entry_id=e.ledger_entry_id) <> 1
 OR octet_length(btrim(e.idempotency_key)) NOT BETWEEN 1 AND 128
 )) THEN RAISE EXCEPTION 'parcel charge reconciliation required'; END IF;
END $$;
ALTER TABLE supplier_return_financial_events ADD COLUMN charge_component TEXT;
UPDATE supplier_return_financial_events SET charge_component='original' WHERE parcel_id IS NOT NULL;
ALTER TABLE supplier_return_financial_events ADD CONSTRAINT parcel_charge_component CHECK
 ((parcel_id IS NULL AND charge_component IS NULL) OR (parcel_id IS NOT NULL AND charge_component IS NOT NULL AND amount BETWEEN 1 AND 9007199254740991 AND payment_method='sender_pays' AND charge_component IN ('original','adjustment') AND (charge_component<>'original' OR operation='debit')));
CREATE UNIQUE INDEX supplier_parcel_original_charge ON supplier_return_financial_events(parcel_id) WHERE charge_component='original';
CREATE TABLE parcel_charge_receipts (
 sequence BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('original','adjustment')),
 parcel_id VARCHAR(80) NOT NULL REFERENCES supplier_return_parcels(id) ON DELETE RESTRICT,
 return_request_id VARCHAR(60) NOT NULL REFERENCES supplier_return_requests(id) ON DELETE RESTRICT,
 supplier_id VARCHAR(50) NOT NULL REFERENCES suppliers(id) ON DELETE RESTRICT,
 payment_method TEXT NOT NULL CHECK(payment_method IN ('sender_pays','receiver_pays')),
 expected_amount BIGINT,
 amount_was_null BOOLEAN NOT NULL DEFAULT false,
 final_amount BIGINT NOT NULL CHECK(final_amount BETWEEN 0 AND 9007199254740991),
 source TEXT NOT NULL, reference TEXT NOT NULL, reason TEXT NOT NULL,
 actor_user_id TEXT NOT NULL, client_key TEXT NOT NULL UNIQUE CHECK(octet_length(client_key) BETWEEN 1 AND 128),
 event_id VARCHAR(80) UNIQUE REFERENCES supplier_return_financial_events(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 CHECK ((kind='original' AND expected_amount IS NULL AND ((payment_method='sender_pays' AND final_amount>0 AND NOT amount_was_null AND event_id IS NOT NULL) OR (payment_method='receiver_pays' AND event_id IS NULL))) OR (kind='adjustment' AND payment_method='sender_pays' AND expected_amount IS NOT NULL AND expected_amount BETWEEN 0 AND 9007199254740991 AND expected_amount<>final_amount AND source IN ('tariff','api','manual') AND btrim(reference)<>'' AND btrim(reason)<>'' AND event_id IS NOT NULL))
);
CREATE UNIQUE INDEX parcel_charge_original_receipt ON parcel_charge_receipts(parcel_id) WHERE kind='original';
INSERT INTO parcel_charge_receipts(id,kind,parcel_id,return_request_id,supplier_id,payment_method,final_amount,source,reference,reason,actor_user_id,client_key,event_id,created_at)
 SELECT 'legacy:'||e.id,'original',e.parcel_id,e.return_request_id,e.supplier_id,e.payment_method,e.amount,COALESCE(p.final_charge_source,''),COALESCE(p.final_charge_reference,''),e.reason,e.actor_user_id,btrim(e.idempotency_key),e.id,e.created_at
 FROM supplier_return_financial_events e JOIN supplier_return_parcels p ON p.id=e.parcel_id WHERE e.charge_component='original';
CREATE TABLE parcel_charge_keys (
 client_key TEXT PRIMARY KEY CHECK(octet_length(client_key) BETWEEN 1 AND 128),
 receipt_id TEXT NOT NULL REFERENCES parcel_charge_receipts(id) ON DELETE RESTRICT
);
INSERT INTO parcel_charge_keys SELECT client_key,id FROM parcel_charge_receipts;
CREATE FUNCTION parcel_charge_append_only() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'parcel charge evidence is append only';
END $$;
CREATE TRIGGER parcel_charge_receipts_immutable BEFORE UPDATE OR DELETE ON parcel_charge_receipts FOR EACH ROW EXECUTE FUNCTION parcel_charge_append_only();
CREATE TRIGGER parcel_charge_events_immutable BEFORE UPDATE OR DELETE ON supplier_return_financial_events FOR EACH ROW WHEN (OLD.charge_component IS NOT NULL) EXECUTE FUNCTION parcel_charge_append_only();

CREATE TRIGGER parcel_charge_keys_immutable BEFORE UPDATE OR DELETE ON parcel_charge_keys FOR EACH ROW EXECUTE FUNCTION parcel_charge_append_only();
