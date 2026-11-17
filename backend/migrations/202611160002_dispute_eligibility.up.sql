-- Additive FIN005 fence. NOWAIT contention aborts the entire migrator transaction;
-- retry the complete runner after the conflicting transaction finishes.
LOCK TABLE orders, disputes, customer_returns, customer_return_financial_events, ledger_entries IN ACCESS EXCLUSIVE MODE NOWAIT;

CREATE TABLE dispute_order_evidence (
 dispute_id TEXT PRIMARY KEY REFERENCES disputes(id), order_id TEXT NOT NULL REFERENCES orders(id),
 prior_status TEXT NOT NULL, prior_disputed_at TIMESTAMPTZ, prior_delivered_at TIMESTAMPTZ,
 prior_confirmed_at TIMESTAMPTZ, prior_window_ends_at TIMESTAMPTZ,
 prior_profit_released BOOLEAN NOT NULL, prior_profit_released_at TIMESTAMPTZ,
 prior_referral_released BOOLEAN NOT NULL, hold_at TIMESTAMPTZ NOT NULL,
 captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), evidence_kind TEXT NOT NULL CHECK(evidence_kind IN ('admission','reviewed_legacy')),
 actor_user_id TEXT NOT NULL, review_reference TEXT NOT NULL DEFAULT '',
 CHECK(evidence_kind<>'reviewed_legacy' OR length(review_reference)>0)
);
CREATE TABLE dispute_financial_decisions (
 dispute_id TEXT PRIMARY KEY REFERENCES disputes(id), order_id TEXT NOT NULL REFERENCES orders(id),
 action TEXT NOT NULL CHECK(action IN ('release_to_seller','refund_supplier','refund_seller','refund_carrier','refund_warehouse')),
 responsible_party TEXT NOT NULL, claim_amount BIGINT NOT NULL CHECK(claim_amount>0 AND claim_amount<=9007199254740991),
 actor_user_id TEXT NOT NULL CHECK(length(actor_user_id)>0), decided_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 evidence_reference TEXT REFERENCES dispute_order_evidence(dispute_id), decision_kind TEXT NOT NULL DEFAULT 'adjudication',
 CHECK((action='release_to_seller' AND responsible_party='seller') OR action='refund_'||responsible_party)
);
CREATE TABLE dispute_order_transitions (
 id TEXT PRIMARY KEY, dispute_id TEXT NOT NULL REFERENCES disputes(id), order_id TEXT NOT NULL REFERENCES orders(id),
 old_status TEXT NOT NULL,new_status TEXT NOT NULL,actor_user_id TEXT NOT NULL,reason TEXT NOT NULL,
 evidence_reference TEXT REFERENCES dispute_order_evidence(dispute_id), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 result TEXT NOT NULL CHECK(result IN ('restored','already_current','needs_review')),
 UNIQUE(dispute_id,reason,result,new_status)
);
CREATE INDEX dispute_evidence_order ON dispute_order_evidence(order_id);
CREATE INDEX dispute_decisions_order ON dispute_financial_decisions(order_id,action);
CREATE INDEX dispute_transitions_order ON dispute_order_transitions(order_id,result);
CREATE INDEX fin005_pending_disputes ON disputes(order_id) WHERE status='pending';
CREATE INDEX fin005_active_returns ON customer_returns(order_id,status);

CREATE FUNCTION fin005_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'FIN005 immutable financial evidence'; END $$;
CREATE TRIGGER fin005_evidence_immutable BEFORE UPDATE OR DELETE ON dispute_order_evidence FOR EACH ROW EXECUTE FUNCTION fin005_immutable();
CREATE TRIGGER fin005_decision_immutable BEFORE UPDATE OR DELETE ON dispute_financial_decisions FOR EACH ROW EXECUTE FUNCTION fin005_immutable();
CREATE TRIGGER fin005_transition_immutable BEFORE UPDATE OR DELETE ON dispute_order_transitions FOR EACH ROW EXECUTE FUNCTION fin005_immutable();
CREATE TRIGGER fin005_reversal_immutable BEFORE UPDATE OR DELETE ON customer_return_financial_events FOR EACH ROW EXECUTE FUNCTION fin005_immutable();
CREATE FUNCTION fin005_linked_ledger_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM customer_return_financial_events WHERE ledger_entry_id=OLD.id) THEN
  RAISE EXCEPTION 'FIN005 immutable linked ledger evidence';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fin005_linked_ledger_immutable BEFORE UPDATE OR DELETE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION fin005_linked_ledger_immutable();

-- Reject BEFORE acquiring parent locks. Old child-first callers must never wait
-- for the parent from inside a newly installed trigger.
CREATE FUNCTION fin005_writer_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE protected BOOLEAN:=FALSE;
BEGIN
 IF TG_TABLE_NAME='disputes' THEN
  IF TG_OP IN ('INSERT','DELETE') THEN protected:=TRUE;
  ELSE protected:=ROW(NEW.id,NEW.order_number,NEW.store_id,NEW.type,NEW.order_id,NEW.claim_amount,NEW.status,NEW.resolution_action,NEW.responsible_party,NEW.resolution_date) IS DISTINCT FROM ROW(OLD.id,OLD.order_number,OLD.store_id,OLD.type,OLD.order_id,OLD.claim_amount,OLD.status,OLD.resolution_action,OLD.responsible_party,OLD.resolution_date); END IF;
 ELSIF TG_TABLE_NAME='customer_returns' THEN
  IF TG_OP IN ('INSERT','DELETE') THEN protected:=TRUE;
  ELSE protected:=ROW(NEW.id,NEW.order_id,NEW.customer_id,NEW.status) IS DISTINCT FROM ROW(OLD.id,OLD.order_id,OLD.customer_id,OLD.status); END IF;
 ELSE
  protected:=(NEW.profit_released AND NOT OLD.profit_released) OR (NEW.referral_commission_released AND NOT OLD.referral_commission_released) OR (NEW.status='delivery_disputed' AND OLD.status<>'delivery_disputed');
 END IF;
 IF protected AND current_setting('sellersho.fin005_writer',TRUE) IS DISTINCT FROM 'v1' THEN
  RAISE EXCEPTION 'FIN005-writer-required' USING ERRCODE='55000';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fin005_disputes_fence BEFORE INSERT OR UPDATE OR DELETE ON disputes FOR EACH ROW EXECUTE FUNCTION fin005_writer_fence();
CREATE TRIGGER fin005_returns_fence BEFORE INSERT OR UPDATE OR DELETE ON customer_returns FOR EACH ROW EXECUTE FUNCTION fin005_writer_fence();
CREATE TRIGGER fin005_orders_fence BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION fin005_writer_fence();

CREATE FUNCTION fin005_dispute_consistency() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d disputes%ROWTYPE;
BEGIN
 SELECT * INTO d FROM disputes WHERE id=NEW.id;
 IF TG_OP='INSERT' AND NOT EXISTS(SELECT 1 FROM dispute_order_evidence e JOIN orders o ON o.id=e.order_id WHERE e.dispute_id=d.id AND e.order_id=d.order_id AND e.hold_at=o.disputed_at AND o.status='delivery_disputed') THEN
  RAISE EXCEPTION 'FIN005 admission evidence required';
 END IF;
 IF d.status='resolved' AND (TG_OP='INSERT' OR OLD.status IS DISTINCT FROM d.status OR OLD.resolution_action IS DISTINCT FROM d.resolution_action OR OLD.responsible_party IS DISTINCT FROM d.responsible_party) AND NOT EXISTS(SELECT 1 FROM dispute_financial_decisions f WHERE f.dispute_id=d.id AND f.order_id=d.order_id AND f.action=d.resolution_action AND f.responsible_party=d.responsible_party AND f.claim_amount=d.claim_amount AND f.decided_at=d.resolution_date) THEN
  RAISE EXCEPTION 'FIN005 immutable decision required';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER fin005_dispute_consistency AFTER INSERT OR UPDATE ON disputes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fin005_dispute_consistency();

CREATE FUNCTION fin005_decision_consistency() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM disputes d WHERE d.id=NEW.dispute_id AND d.order_id=NEW.order_id AND d.status='resolved' AND d.resolution_action=NEW.action AND d.responsible_party=NEW.responsible_party AND d.claim_amount=NEW.claim_amount AND d.resolution_date=NEW.decided_at) THEN RAISE EXCEPTION 'FIN005 decision projection required'; END IF;
 IF NEW.action<>'release_to_seller' AND (SELECT COALESCE(SUM(amount_toman),0) FROM order_refund_obligations WHERE purpose='dispute' AND business_id=NEW.dispute_id AND order_id=NEW.order_id)<>NEW.claim_amount THEN RAISE EXCEPTION 'FIN005 decision obligations required'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER fin005_decision_consistency AFTER INSERT ON dispute_financial_decisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fin005_decision_consistency();

-- The same exact per-return predicate serves the bounded due scan and the
-- locked canonical release guard. NUMERIC multiplication avoids bigint wrap.
CREATE FUNCTION fin005_return_entitlement_proven(wanted_order TEXT,wanted_return TEXT)
RETURNS BOOLEAN LANGUAGE SQL STABLE AS $$
WITH ret AS (
 SELECT cr.id,o.id order_id,o.customer_user_id customer_id,o.store_id,s.owner_type
 FROM customer_returns cr JOIN orders o ON o.id=cr.order_id LEFT JOIN stores s ON s.id=o.store_id
 WHERE cr.id::text=wanted_return AND o.id=wanted_order AND cr.status='refunded'
 AND cr.customer_id=o.customer_user_id AND s.owner_type IN ('central','dropshipper')
), items AS (
 SELECT ri.id,ri.qty,oi.id order_item_id,oi.quantity,oi.final_unit_price,oi.wholesale_cost,oi.seller_margin,
 (SELECT MIN(x.supplier_id) FROM order_item_origins x WHERE x.order_item_id::text=oi.id::text) supplier_id,
 (SELECT COUNT(*)>0 AND COUNT(DISTINCT x.supplier_id)=1 AND BOOL_AND(x.order_id=oi.order_id AND x.quantity>0 AND COALESCE(x.supplier_id,'')<>'') AND SUM(x.quantity)=oi.quantity FROM order_item_origins x WHERE x.order_item_id::text=oi.id::text) origin_proven
 FROM return_items ri JOIN order_items oi ON oi.id::text=ri.order_item_id JOIN ret r ON oi.order_id=r.order_id
 WHERE ri.return_id::text=wanted_return AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok','defective')
), expected AS (
 SELECT i.id,'product_cost' component,'supplier' party,i.supplier_id account,
 i.qty::numeric*i.wholesale_cost amount,i.supplier_id supplier_id,NULL::text store_id
 FROM items i WHERE i.wholesale_cost>0
 UNION ALL
 SELECT i.id,'seller_profit','seller',r.store_id,i.qty::numeric*i.seller_margin,NULL::text,r.store_id
 FROM items i CROSS JOIN ret r WHERE r.owner_type='dropshipper' AND i.seller_margin>0
), obligations AS (
 SELECT f.*,COALESCE((SELECT e.status FROM order_refund_events e WHERE e.obligation_id=f.id ORDER BY event_no DESC LIMIT 1),f.status) current_state
 FROM order_refund_obligations f WHERE f.purpose='customer_return' AND f.business_id=wanted_return
)
SELECT COALESCE(EXISTS(SELECT 1 FROM ret) AND EXISTS(SELECT 1 FROM items)
 AND NOT EXISTS(SELECT 1 FROM return_items ri LEFT JOIN order_items oi ON oi.id::text=ri.order_item_id WHERE ri.return_id::text=wanted_return AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok','defective') AND (oi.id IS NULL OR oi.order_id<>wanted_order OR ri.qty>oi.quantity))
 AND NOT EXISTS(SELECT 1 FROM items WHERE qty<=0 OR final_unit_price<0 OR wholesale_cost<0 OR seller_margin<0 OR (wholesale_cost>0 AND origin_proven IS DISTINCT FROM TRUE))
 AND (SELECT SUM(qty::numeric*final_unit_price) FROM items) BETWEEN 1 AND 9007199254740991
 AND (SELECT SUM(amount_toman::numeric) FROM obligations)=(SELECT SUM(qty::numeric*final_unit_price) FROM items)
 AND NOT EXISTS(SELECT 1 FROM obligations f CROSS JOIN ret r WHERE f.order_id<>r.order_id OR f.customer_id IS DISTINCT FROM r.customer_id OR f.destination<>'wallet' OR f.policy_version<>'return_wallet_v1' OR f.current_state<>'succeeded')
 AND (SELECT COUNT(*) FROM customer_return_financial_events WHERE return_id::text=wanted_return)=(SELECT COUNT(*) FROM expected)
 AND NOT EXISTS(SELECT 1 FROM expected x WHERE x.amount NOT BETWEEN 1 AND 9007199254740991 OR
 (SELECT COUNT(*) FROM customer_return_financial_events e JOIN ledger_entries l ON l.id=e.ledger_entry_id
 WHERE e.return_id::text=wanted_return AND e.return_item_id=x.id AND e.order_id=wanted_order
 AND e.component=x.component AND e.operation='debit' AND e.amount::numeric=x.amount
 AND e.supplier_id IS NOT DISTINCT FROM x.supplier_id AND e.store_id IS NOT DISTINCT FROM x.store_id
 AND l.type='debit' AND l.target_type=x.party AND l.target_account=x.account AND l.amount=e.amount AND l.related_order_or_dispute=wanted_return)<>1),FALSE)
$$;

CREATE INDEX fin005_due_release ON orders(return_window_ends_at,id) WHERE NOT profit_released AND status='delivered_confirmed';
