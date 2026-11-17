-- FIN007: drain the complete source snapshot before installing the writer fence.
-- NOWAIT aborts this whole canonical migrator transaction; retry after draining.
LOCK TABLE orders,checkout_attempts,payment_transactions,order_payment_verifications,
 order_payment_verification_events,order_payment_receipts,seller_discount_codes,supplier_shipments
 IN ACCESS EXCLUSIVE MODE NOWAIT;
ALTER TABLE seller_discount_codes ADD COLUMN revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0);
ALTER TABLE orders ADD COLUMN coupon_discount_amount BIGINT CHECK(coupon_discount_amount>=0 AND coupon_discount_amount<=9007199254740991 AND coupon_discount_amount<=seller_discount_amount);
CREATE TABLE order_coupon_reservations(
 order_id TEXT PRIMARY KEY REFERENCES orders(id) ON DELETE RESTRICT,
 coupon_id TEXT NOT NULL REFERENCES seller_discount_codes(id) ON DELETE RESTRICT,
 store_id TEXT NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
 code TEXT NOT NULL CHECK(code<>'' AND code=upper(btrim(code))),
 seller_discount_amount BIGINT NOT NULL,
 coupon_discount_amount BIGINT,
 coupon_revision BIGINT,
 state TEXT NOT NULL CHECK(state IN ('reserved','consumed','released','review_hold')),
 evidence_kind TEXT NOT NULL CHECK(evidence_kind IN ('admission','legacy_snapshot')),
 reason TEXT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK(evidence_kind<>'admission' OR (coupon_revision IS NOT NULL AND coupon_revision>0 AND coupon_discount_amount IS NOT NULL AND coupon_discount_amount>0 AND coupon_discount_amount<=seller_discount_amount AND seller_discount_amount<=9007199254740991))
);
CREATE INDEX order_coupon_occupied ON order_coupon_reservations(coupon_id) WHERE state<>'released';
CREATE TABLE order_coupon_events(
 order_id TEXT NOT NULL REFERENCES order_coupon_reservations(order_id),
 event_kind TEXT NOT NULL CHECK(event_kind IN ('legacy_snapshot','reserved','consumed','released','review_hold')),prior_state TEXT CHECK(prior_state IN ('reserved','consumed','released','review_hold')),new_state TEXT NOT NULL CHECK(new_state IN ('reserved','consumed','released','review_hold')),
 reason TEXT NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(order_id,event_kind)
);
CREATE TABLE coupon_reconciliation_issues(
 id BIGSERIAL PRIMARY KEY,order_id TEXT REFERENCES orders(id),coupon_id TEXT REFERENCES seller_discount_codes(id),
 store_id TEXT NOT NULL,code TEXT NOT NULL,old_count BIGINT,rebuilt_count BIGINT,
 reason TEXT NOT NULL,source_snapshot JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
-- Resolve normalized identity within the original store only. No price recomputation.
CREATE TEMP TABLE fin007_history ON COMMIT DROP AS
 SELECT o.*,upper(btrim(o.discount_code)) normalized_code,
 (SELECT count(*) FROM seller_discount_codes c WHERE c.store_id=o.store_id AND upper(btrim(c.code))=upper(btrim(o.discount_code))) matches,
 (SELECT min(c.id) FROM seller_discount_codes c WHERE c.store_id=o.store_id AND upper(btrim(c.code))=upper(btrim(o.discount_code))) coupon_id,
 EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.order_id=o.id) receipt,
 EXISTS(SELECT 1 FROM payment_transactions p WHERE p.order_id=o.id AND p.status IN ('paid','refunded')) booked,
 EXISTS(SELECT 1 FROM payment_transactions p WHERE p.order_id=o.id AND p.status NOT IN ('pending','paid','refunded')) ambiguous_payment,
 EXISTS(SELECT 1 FROM order_payment_verifications v WHERE v.order_id=o.id AND NOT EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.verification_id=v.id)) inquiry
 FROM orders o WHERE btrim(COALESCE(o.discount_code,''))<>'';
INSERT INTO coupon_reconciliation_issues(order_id,coupon_id,store_id,code,reason,source_snapshot)
 SELECT id,CASE WHEN matches=1 THEN coupon_id END,store_id,normalized_code,
 CASE WHEN matches=0 THEN 'missing_identity' ELSE 'ambiguous_identity' END,jsonb_build_object('order_id',h.id,'store_id',h.store_id,'code',h.normalized_code,'seller_discount_amount',h.seller_discount_amount,'total_amount',h.total_amount,'payable_amount',h.payable_amount,'status',h.status,'payment_status',h.payment_status,'inventory_captured',h.inventory_captured,'receipt',h.receipt,'booked',h.booked,'inquiry',h.inquiry,'ambiguous_payment',h.ambiguous_payment,'payment_expires_at',h.payment_expires_at)
 FROM fin007_history h WHERE matches<>1;
INSERT INTO order_coupon_reservations(order_id,coupon_id,store_id,code,seller_discount_amount,coupon_discount_amount,coupon_revision,state,evidence_kind,reason)
 SELECT id,coupon_id,store_id,normalized_code,seller_discount_amount,NULL,NULL,
 CASE WHEN seller_discount_amount<=0 OR seller_discount_amount>9007199254740991 OR payable_amount<0 OR total_amount<0 THEN 'review_hold'
 WHEN receipt OR payment_status IN ('paid','refunded') OR inventory_captured OR booked THEN 'consumed'
 WHEN inquiry OR ambiguous_payment THEN 'review_hold'
 WHEN status='cancelled' AND payment_status='pending' THEN 'released'
 WHEN status IN ('pending','processing') AND payment_status='pending' AND payment_expires_at IS NOT NULL THEN 'reserved'
 ELSE 'review_hold' END,'legacy_snapshot','historical_snapshot'
 FROM fin007_history WHERE matches=1;
INSERT INTO coupon_reconciliation_issues(order_id,coupon_id,store_id,code,reason,source_snapshot)
 SELECT h.id,h.coupon_id,h.store_id,h.normalized_code,
 CASE WHEN h.seller_discount_amount<=0 THEN 'zero_or_invalid_aggregate' WHEN inquiry THEN 'unresolved_inquiry' ELSE 'uncertain_history' END,jsonb_build_object('order_id',h.id,'store_id',h.store_id,'code',h.normalized_code,'seller_discount_amount',h.seller_discount_amount,'total_amount',h.total_amount,'payable_amount',h.payable_amount,'status',h.status,'payment_status',h.payment_status,'inventory_captured',h.inventory_captured,'receipt',h.receipt,'booked',h.booked,'inquiry',h.inquiry,'ambiguous_payment',h.ambiguous_payment,'payment_expires_at',h.payment_expires_at)
 FROM fin007_history h JOIN order_coupon_reservations r ON r.order_id=h.id WHERE r.state='review_hold';
INSERT INTO order_coupon_events(order_id,event_kind,new_state,reason)
 SELECT order_id,'legacy_snapshot',state,reason FROM order_coupon_reservations;
CREATE TEMP TABLE fin007_counts ON COMMIT DROP AS
 SELECT c.id,c.store_id,c.code,c.used_count old_count,c.max_uses,count(r.order_id) FILTER(WHERE r.state<>'released') rebuilt_count
 FROM seller_discount_codes c LEFT JOIN order_coupon_reservations r ON r.coupon_id=c.id GROUP BY c.id;
INSERT INTO coupon_reconciliation_issues(coupon_id,store_id,code,old_count,rebuilt_count,reason,source_snapshot)
 SELECT id,store_id,upper(btrim(code)),old_count,rebuilt_count,
 CASE WHEN max_uses IS NOT NULL AND rebuilt_count>max_uses THEN 'historical_oversubscription' ELSE 'counter_discrepancy' END,to_jsonb(c)
 FROM fin007_counts c WHERE old_count<>rebuilt_count OR (max_uses IS NOT NULL AND rebuilt_count>max_uses);
-- PostgreSQL checked integer conversion aborts migration on overflow.
UPDATE seller_discount_codes c SET used_count=x.rebuilt_count::integer FROM fin007_counts x WHERE c.id=x.id;

CREATE FUNCTION fin007_coupon_terms() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 IF (OLD.id,OLD.store_id,OLD.code) IS DISTINCT FROM (NEW.id,NEW.store_id,NEW.code)
 AND EXISTS(SELECT 1 FROM order_coupon_reservations WHERE coupon_id=OLD.id) THEN RAISE EXCEPTION 'FIN007 immutable coupon identity' USING ERRCODE='55000'; END IF;
 IF OLD.used_count IS DISTINCT FROM NEW.used_count AND current_setting('sellersho.fin007_writer',true) IS DISTINCT FROM 'v1' THEN RAISE EXCEPTION 'FIN007-writer-required' USING ERRCODE='55000'; END IF;
 NEW.revision:=OLD.revision;
 IF (OLD.mode,OLD.value,OLD.starts_at,OLD.ends_at,OLD.is_active,OLD.max_uses) IS DISTINCT FROM (NEW.mode,NEW.value,NEW.starts_at,NEW.ends_at,NEW.is_active,NEW.max_uses) THEN NEW.revision:=OLD.revision+1; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fin007_coupon_terms BEFORE UPDATE ON seller_discount_codes FOR EACH ROW EXECUTE FUNCTION fin007_coupon_terms();
CREATE FUNCTION fin007_order_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE backed boolean;
BEGIN
 backed:=btrim(COALESCE(CASE WHEN TG_OP='DELETE' THEN OLD.discount_code ELSE NEW.discount_code END,''))<>'';
 IF TG_OP='UPDATE' THEN
  backed:=backed OR btrim(COALESCE(OLD.discount_code,''))<>'';
  IF backed AND ((NOT OLD.inventory_captured AND NEW.inventory_captured) OR (NEW.status IN ('shipped','delivered') AND OLD.status NOT IN ('shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed'))) AND NOT EXISTS(SELECT 1 FROM order_coupon_reservations WHERE order_id=OLD.id) THEN RAISE EXCEPTION 'FIN007 unresolved coupon cannot admit fulfillment' USING ERRCODE='55000'; END IF;
  IF backed AND (OLD.store_id,OLD.discount_code,OLD.seller_discount_amount,OLD.coupon_discount_amount,OLD.total_amount,OLD.discount_amount,OLD.payable_amount) IS DISTINCT FROM (NEW.store_id,NEW.discount_code,NEW.seller_discount_amount,NEW.coupon_discount_amount,NEW.total_amount,NEW.discount_amount,NEW.payable_amount) THEN RAISE EXCEPTION 'FIN007 immutable order coupon snapshot' USING ERRCODE='55000'; END IF;
  IF NOT ((OLD.status='cancelled') IS DISTINCT FROM (NEW.status='cancelled') OR OLD.payment_status IS DISTINCT FROM NEW.payment_status OR OLD.inventory_captured IS DISTINCT FROM NEW.inventory_captured) THEN RETURN NEW; END IF;
 END IF;
 IF backed AND current_setting('sellersho.fin007_writer',true) IS DISTINCT FROM 'v1' THEN RAISE EXCEPTION 'FIN007-writer-required' USING ERRCODE='55000'; END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
CREATE TRIGGER fin007_order_fence BEFORE INSERT OR UPDATE OR DELETE ON orders FOR EACH ROW EXECUTE FUNCTION fin007_order_fence();
-- A direct leg can dispatch without changing the aggregate order (mixed
-- fulfillment). Fence that physical edge without taking parent row locks here.
CREATE FUNCTION fin007_shipment_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND OLD.order_id=NEW.order_id AND (OLD.shipped_at IS NOT NULL OR OLD.delivered_at IS NOT NULL OR OLD.status IN ('shipped','in_transit','delivered')) THEN RETURN NEW; END IF;
 IF NOT (NEW.shipped_at IS NOT NULL OR NEW.delivered_at IS NOT NULL OR NEW.status IN ('shipped','in_transit','delivered')) THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM orders WHERE id=NEW.order_id AND btrim(COALESCE(discount_code,''))<>'') THEN
  IF current_setting('sellersho.fin007_writer',true) IS DISTINCT FROM 'v1' THEN RAISE EXCEPTION 'FIN007-writer-required' USING ERRCODE='55000'; END IF;
  IF NOT EXISTS(SELECT 1 FROM orders o JOIN order_coupon_reservations r ON r.order_id=o.id WHERE o.id=NEW.order_id AND o.payment_status='paid' AND o.status<>'cancelled' AND r.state IN ('consumed','review_hold')) THEN RAISE EXCEPTION 'FIN007 coupon cannot admit new shipment' USING ERRCODE='55000'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fin007_shipment_fence BEFORE INSERT OR UPDATE ON supplier_shipments FOR EACH ROW EXECUTE FUNCTION fin007_shipment_fence();
-- Child fences read identity without acquiring parent locks.
CREATE FUNCTION fin007_payment_fence() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE oid text;
BEGIN
 IF TG_TABLE_NAME='order_payment_verification_events' THEN
  SELECT order_id INTO oid FROM order_payment_verifications WHERE id=NEW.verification_id;
 ELSE oid:=NEW.order_id; END IF;
 IF TG_OP='UPDATE' AND TG_TABLE_NAME='payment_transactions' AND (to_jsonb(OLD)-ARRAY['updated_at','card_mask','rrn'])=(to_jsonb(NEW)-ARRAY['updated_at','card_mask','rrn']) THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM orders WHERE id=oid AND btrim(COALESCE(discount_code,''))<>'') AND current_setting('sellersho.fin007_writer',true) IS DISTINCT FROM 'v1' THEN RAISE EXCEPTION 'FIN007-writer-required' USING ERRCODE='55000'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fin007_payment_fence BEFORE INSERT OR UPDATE ON payment_transactions FOR EACH ROW EXECUTE FUNCTION fin007_payment_fence();
CREATE TRIGGER fin007_inquiry_fence BEFORE INSERT ON order_payment_verifications FOR EACH ROW EXECUTE FUNCTION fin007_payment_fence();
CREATE TRIGGER fin007_receipt_fence BEFORE INSERT ON order_payment_receipts FOR EACH ROW EXECUTE FUNCTION fin007_payment_fence();
CREATE TRIGGER fin007_verification_event_fence BEFORE INSERT ON order_payment_verification_events FOR EACH ROW EXECUTE FUNCTION fin007_payment_fence();
CREATE FUNCTION fin007_ledger_fence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='coupon_reconciliation_issues' THEN RAISE EXCEPTION 'FIN007 immutable migration-origin issue' USING ERRCODE='55000'; END IF;
 IF current_setting('sellersho.fin007_writer',true) IS DISTINCT FROM 'v1' THEN RAISE EXCEPTION 'FIN007-writer-required' USING ERRCODE='55000'; END IF;
 IF TG_OP<>'INSERT' AND TG_TABLE_NAME<>'order_coupon_reservations' THEN RAISE EXCEPTION 'FIN007 immutable evidence' USING ERRCODE='55000'; END IF;
 -- Backfill completed before this fence was installed. Runtime writers cannot
 -- claim migration origin to bypass admission capacity, terms or receipt proof.
 IF TG_TABLE_NAME='order_coupon_reservations' AND TG_OP='INSERT' AND ((to_jsonb(NEW)->>'evidence_kind')<>'admission' OR (to_jsonb(NEW)->>'state')<>'reserved') THEN RAISE EXCEPTION 'FIN007 invalid runtime reservation origin/state' USING ERRCODE='23514'; END IF;
 -- This is the admission's exact owner guard at rank350, before child INSERT
 -- and products. Normal creators already hold it, so this is reentrant. An
 -- unmarked/origin-forged writer was rejected above without taking this lock.
 -- Stamp only AFTER the actual owner wait: insert-first/counter-later SQL must
 -- not retain a pre-wait instant. Deferred checks retain this fixed instant.
 IF TG_TABLE_NAME='order_coupon_reservations' AND TG_OP='INSERT' THEN
  PERFORM 1 FROM seller_discount_codes WHERE id=NEW.coupon_id FOR UPDATE;
  NEW.created_at:=clock_timestamp();
  IF NOT EXISTS(SELECT 1 FROM seller_discount_codes WHERE id=NEW.coupon_id AND starts_at<=NEW.created_at AND ends_at>NEW.created_at) THEN RAISE EXCEPTION 'FIN007 invalid admission window' USING ERRCODE='23514'; END IF;
 END IF;
 IF TG_TABLE_NAME='order_coupon_reservations' AND TG_OP='DELETE' THEN RAISE EXCEPTION 'FIN007 immutable reservation' USING ERRCODE='55000'; END IF;
 IF TG_TABLE_NAME='order_coupon_reservations' AND TG_OP='UPDATE' THEN
  IF (to_jsonb(OLD)-ARRAY['state','reason','updated_at']) IS DISTINCT FROM (to_jsonb(NEW)-ARRAY['state','reason','updated_at']) OR (OLD.state IN ('consumed','released') AND NEW.state<>OLD.state) OR (OLD.state='review_hold' AND NEW.state='reserved') THEN RAISE EXCEPTION 'FIN007 immutable reservation' USING ERRCODE='55000'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER fin007_reservation_fence BEFORE INSERT OR UPDATE OR DELETE ON order_coupon_reservations FOR EACH ROW EXECUTE FUNCTION fin007_ledger_fence();
CREATE TRIGGER fin007_event_fence BEFORE INSERT OR UPDATE OR DELETE ON order_coupon_events FOR EACH ROW EXECUTE FUNCTION fin007_ledger_fence();
CREATE TRIGGER fin007_issue_fence BEFORE INSERT OR UPDATE OR DELETE ON coupon_reconciliation_issues FOR EACH ROW EXECUTE FUNCTION fin007_ledger_fence();
CREATE FUNCTION fin007_consistency() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE oid text; cid text; r order_coupon_reservations; o orders;
BEGIN
 IF TG_TABLE_NAME='seller_discount_codes' THEN cid:=NEW.id;
 ELSIF TG_TABLE_NAME='orders' THEN oid:=NEW.id;
 ELSE oid:=NEW.order_id;IF TG_TABLE_NAME='order_coupon_reservations' THEN cid:=NEW.coupon_id; END IF; END IF;
 IF oid IS NOT NULL THEN
  SELECT * INTO o FROM orders WHERE id=oid;
  SELECT * INTO r FROM order_coupon_reservations WHERE order_id=oid;
  IF btrim(COALESCE(o.discount_code,''))<>'' AND r.order_id IS NULL AND NOT EXISTS(SELECT 1 FROM coupon_reconciliation_issues WHERE order_id=oid AND reason IN ('missing_identity','ambiguous_identity')) THEN RAISE EXCEPTION 'FIN007 missing reservation' USING ERRCODE='23514'; END IF;
  IF r.order_id IS NOT NULL THEN
   cid:=r.coupon_id;
   IF (r.store_id,r.code,r.seller_discount_amount,r.coupon_discount_amount) IS DISTINCT FROM (o.store_id,upper(btrim(o.discount_code)),o.seller_discount_amount,o.coupon_discount_amount) OR NOT EXISTS(SELECT 1 FROM seller_discount_codes WHERE id=r.coupon_id AND store_id=r.store_id AND upper(btrim(code))=r.code) THEN RAISE EXCEPTION 'FIN007 coupon evidence mismatch' USING ERRCODE='23514'; END IF;
   IF r.state='reserved' AND (o.status='cancelled' OR o.inventory_captured OR o.payment_status IN ('paid','refunded')) THEN RAISE EXCEPTION 'FIN007 reserved lifecycle mismatch' USING ERRCODE='23514'; END IF;
   IF r.state='released' AND (o.status<>'cancelled' OR o.inventory_captured) THEN RAISE EXCEPTION 'FIN007 released order cannot admit fulfillment' USING ERRCODE='23514'; END IF;
   IF (r.evidence_kind='admission' OR TG_TABLE_NAME='order_payment_receipts') AND r.state NOT IN ('consumed','released') AND EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=oid) THEN RAISE EXCEPTION 'FIN007 receipt did not consume coupon' USING ERRCODE='23514'; END IF;
   IF r.state='released' AND (o.status IS DISTINCT FROM 'cancelled' OR o.inventory_captured IS DISTINCT FROM FALSE OR o.payment_status IS DISTINCT FROM 'pending' OR EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=oid) OR EXISTS(SELECT 1 FROM order_payment_verifications WHERE order_id=oid) OR EXISTS(SELECT 1 FROM payment_transactions WHERE order_id=oid AND status IS DISTINCT FROM 'pending')) THEN
    -- A later certified receipt on an already released reservation must persist.
    IF TG_TABLE_NAME='order_coupon_reservations' AND (TG_OP='INSERT' OR (to_jsonb(OLD)->>'state')<>'released') THEN RAISE EXCEPTION 'FIN007 release lacks unpaid proof' USING ERRCODE='23514'; END IF;
   END IF;
   IF r.evidence_kind='admission' AND r.state='consumed' AND NOT EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=oid) THEN RAISE EXCEPTION 'FIN007 consume lacks receipt' USING ERRCODE='23514'; END IF;
   IF TG_TABLE_NAME='order_coupon_reservations' AND TG_OP='INSERT' AND r.evidence_kind='admission' THEN
    IF NOT EXISTS(SELECT 1 FROM seller_discount_codes WHERE id=cid AND revision=r.coupon_revision AND is_active AND starts_at<=r.created_at AND ends_at>r.created_at AND (max_uses IS NULL OR used_count<=max_uses)) OR EXISTS(SELECT 1 FROM coupon_reconciliation_issues WHERE store_id=r.store_id AND code=r.code AND reason IN ('missing_identity','ambiguous_identity')) THEN RAISE EXCEPTION 'FIN007 invalid admission' USING ERRCODE='23514'; END IF;
   END IF;
   IF NOT EXISTS(SELECT 1 FROM order_coupon_events WHERE order_id=oid AND new_state=r.state) THEN RAISE EXCEPTION 'FIN007 missing transition event' USING ERRCODE='23514'; END IF;
  END IF;
 END IF;
 IF cid IS NOT NULL AND EXISTS(SELECT 1 FROM seller_discount_codes c WHERE c.id=cid AND c.used_count<>(SELECT count(*) FROM order_coupon_reservations x WHERE x.coupon_id=cid AND x.state<>'released')) THEN RAISE EXCEPTION 'FIN007 coupon count mismatch' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER fin007_order_consistency AFTER INSERT OR UPDATE ON orders DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fin007_consistency();
CREATE CONSTRAINT TRIGGER fin007_reservation_consistency AFTER INSERT OR UPDATE ON order_coupon_reservations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fin007_consistency();
CREATE CONSTRAINT TRIGGER fin007_count_consistency AFTER UPDATE ON seller_discount_codes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fin007_consistency();

CREATE CONSTRAINT TRIGGER fin007_receipt_consistency AFTER INSERT ON order_payment_receipts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fin007_consistency();
