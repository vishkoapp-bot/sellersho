-- Ordinary checkout retains one pending/paid active attempt. An independently
-- verified or legacy-recorded late capture is compensation, never checkout.
ALTER TABLE payment_transactions ADD COLUMN compensation_capture BOOLEAN NOT NULL DEFAULT FALSE;
DROP INDEX payment_transactions_one_active_per_order;
CREATE UNIQUE INDEX payment_transactions_one_active_per_order ON payment_transactions(order_id) WHERE status IN ('pending','paid') AND NOT compensation_capture;
-- Add evidence only; never assume historical simulator rows are real cash.
-- Request snapshot commits before external verification; retry is an inquiry,
-- never a second charge. Simulator evidence never authorizes real money.
CREATE TABLE order_payment_verifications (
 id TEXT PRIMARY KEY,
 payment_id TEXT NOT NULL UNIQUE REFERENCES payment_transactions(id) ON DELETE RESTRICT,
 order_id TEXT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
 customer_id UUID REFERENCES users(id) ON DELETE RESTRICT,
 amount_toman BIGINT NOT NULL CHECK(amount_toman BETWEEN 1 AND 9007199254740991),
 currency TEXT NOT NULL CHECK(currency='TOMAN'),
 funding_kind TEXT NOT NULL CHECK(funding_kind IN ('gateway','simulated')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX order_payment_verifications_order ON order_payment_verifications(order_id,id);
CREATE TABLE order_payment_verification_events (
 id TEXT PRIMARY KEY,
 verification_id TEXT NOT NULL REFERENCES order_payment_verifications(id) ON DELETE RESTRICT,
 status TEXT NOT NULL CHECK(status IN ('verified','unknown','amount_mismatch')),
 observed_amount_toman BIGINT,
 provider_reference TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 CHECK(status='unknown' OR observed_amount_toman IS NOT NULL)
);
CREATE INDEX order_payment_verification_events_request ON order_payment_verification_events(verification_id,created_at,id);
CREATE TABLE order_payment_receipts (
 id TEXT PRIMARY KEY,
 verification_id TEXT NOT NULL UNIQUE REFERENCES order_payment_verifications(id) ON DELETE RESTRICT,
 funding_kind TEXT NOT NULL CHECK(funding_kind IN ('gateway','simulated')),
 payment_id TEXT NOT NULL UNIQUE REFERENCES payment_transactions(id) ON DELETE RESTRICT,
 order_id TEXT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
 customer_id UUID REFERENCES users(id) ON DELETE RESTRICT,
 amount_toman BIGINT NOT NULL CHECK(amount_toman BETWEEN 1 AND 9007199254740991),
 currency TEXT NOT NULL CHECK(currency='TOMAN'),
 provider_reference TEXT NOT NULL CHECK(length(btrim(provider_reference))>0),
 verified_at TIMESTAMPTZ NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX order_payment_receipt_reference ON order_payment_receipts(funding_kind,provider_reference);
CREATE INDEX order_payment_receipts_order ON order_payment_receipts(order_id,id);
CREATE TABLE order_refund_obligations (
 id TEXT PRIMARY KEY,
 order_id TEXT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
 source_payment_id TEXT NOT NULL REFERENCES payment_transactions(id) ON DELETE RESTRICT,
 customer_id UUID REFERENCES users(id) ON DELETE RESTRICT,
 source_amount_toman BIGINT NOT NULL CHECK(source_amount_toman BETWEEN 1 AND 9007199254740991),
 amount_toman BIGINT NOT NULL CHECK(amount_toman BETWEEN 1 AND 9007199254740991),
 currency TEXT NOT NULL CHECK(currency='TOMAN'),
 purpose TEXT NOT NULL CHECK(purpose IN ('cancellation','duplicate_payment','customer_return','admin_refund','dispute','legacy_reservation')),
 business_id TEXT NOT NULL,
 destination TEXT NOT NULL CHECK(destination IN ('original_payment','wallet','review')),
 policy_version TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','succeeded','failed','unknown')),
 state_reason TEXT NOT NULL DEFAULT '',
 actor_user_id TEXT NOT NULL CHECK(length(btrim(actor_user_id))>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(purpose,business_id,source_payment_id)
);
CREATE INDEX order_refund_obligations_source ON order_refund_obligations(source_payment_id,id) INCLUDE(amount_toman);
CREATE INDEX order_refund_obligations_order ON order_refund_obligations(order_id,id);
CREATE INDEX order_refund_obligations_work ON order_refund_obligations(status,created_at,id);
CREATE FUNCTION immutable_order_refund_evidence() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'immutable payment receipt and refund principal evidence';
END $$;
CREATE TRIGGER payment_verification_identity BEFORE UPDATE OR DELETE ON order_payment_verifications FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE TRIGGER payment_verification_event_identity BEFORE UPDATE OR DELETE ON order_payment_verification_events FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE FUNCTION guard_payment_verification_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p payment_transactions%ROWTYPE;owner UUID;
BEGIN
 PERFORM 1 FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO p FROM payment_transactions WHERE id=NEW.payment_id FOR UPDATE;
 SELECT customer_user_id INTO owner FROM orders WHERE id=NEW.order_id;
 IF NOT FOUND OR p.id IS NULL OR (p.order_id,p.amount,owner) IS DISTINCT FROM (NEW.order_id,NEW.amount_toman,NEW.customer_id) OR NEW.currency<>'TOMAN' THEN RAISE EXCEPTION 'verification request must bind exact source principal and customer'; END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER payment_verification_insert BEFORE INSERT ON order_payment_verifications FOR EACH ROW EXECUTE FUNCTION guard_payment_verification_insert();
CREATE TRIGGER payment_receipt_identity BEFORE UPDATE OR DELETE ON order_payment_receipts FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE TRIGGER refund_obligation_identity BEFORE UPDATE OR DELETE ON order_refund_obligations FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE FUNCTION guard_payment_receipt_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p payment_transactions%ROWTYPE; owner UUID;v order_payment_verifications%ROWTYPE;
BEGIN
 PERFORM 1 FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO p FROM payment_transactions WHERE id=NEW.payment_id FOR UPDATE;
 SELECT customer_user_id INTO owner FROM orders WHERE id=NEW.order_id;
 IF NOT FOUND OR p.id IS NULL OR (p.order_id,p.amount,owner) IS DISTINCT FROM (NEW.order_id,NEW.amount_toman,NEW.customer_id) OR NEW.currency<>'TOMAN' THEN RAISE EXCEPTION 'verified receipt must bind the exact payment, principal and customer'; END IF;
 SELECT * INTO v FROM order_payment_verifications WHERE id=NEW.verification_id;
 IF NOT FOUND OR (v.payment_id,v.order_id,v.customer_id,v.amount_toman,v.currency,v.funding_kind) IS DISTINCT FROM (NEW.payment_id,NEW.order_id,NEW.customer_id,NEW.amount_toman,NEW.currency,NEW.funding_kind) THEN RAISE EXCEPTION 'receipt must match committed verification request'; END IF;
 IF NEW.verified_at>clock_timestamp()+INTERVAL '5 seconds' THEN RAISE EXCEPTION 'payment verification cannot invent a future confirmation'; END IF;
 NEW.created_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER payment_receipt_insert BEFORE INSERT ON order_payment_receipts FOR EACH ROW EXECUTE FUNCTION guard_payment_receipt_insert();
CREATE FUNCTION guard_refund_obligation_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o orders%ROWTYPE;p payment_transactions%ROWTYPE;old_ledger ledger_entries%ROWTYPE;used NUMERIC;old_used NUMERIC;verified BOOLEAN;business_total NUMERIC;return_total NUMERIC;
BEGIN
 SELECT * INTO o FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO p FROM payment_transactions WHERE id=NEW.source_payment_id FOR UPDATE;
 IF o.id IS NULL OR p.id IS NULL OR p.order_id IS DISTINCT FROM o.id OR NEW.customer_id IS DISTINCT FROM o.customer_user_id OR NEW.currency<>'TOMAN' OR p.amount NOT BETWEEN 1 AND 9007199254740991 THEN RAISE EXCEPTION 'refund source, customer, currency or principal mismatch'; END IF;
 IF o.customer_user_id IS NOT NULL THEN PERFORM 1 FROM users WHERE id=o.customer_user_id FOR UPDATE; END IF;
 SELECT EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=p.id AND amount_toman=p.amount AND currency='TOMAN' AND funding_kind='gateway') INTO verified;
 IF p.status NOT IN ('paid','refunded') AND NOT EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=p.id) THEN RAISE EXCEPTION 'refund cannot reserve uncaptured principal'; END IF;
 SELECT COALESCE(SUM(amount_toman),0) INTO used FROM order_refund_obligations WHERE source_payment_id=p.id;
 IF used+NEW.amount_toman>p.amount THEN RAISE EXCEPTION 'refund reservations exceed captured source principal'; END IF;
 IF NEW.purpose='cancellation' THEN
  IF NEW.business_id<>o.id OR o.status<>'cancelled' OR NEW.destination<>'original_payment' OR NEW.policy_version<>'original_payment_v1' OR NEW.amount_toman::numeric<>p.amount::numeric-used THEN RAISE EXCEPTION 'supported full remaining cancellation source required'; END IF;
  NEW.status:=CASE WHEN verified THEN 'pending' ELSE 'unknown' END;
  NEW.state_reason:=CASE WHEN verified THEN 'awaiting_original_provider' WHEN EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=p.id AND funding_kind='simulated') THEN 'simulated_funding' ELSE 'funding_unverified' END;
 ELSIF NEW.purpose='duplicate_payment' THEN
  IF NEW.business_id<>p.id OR NOT p.compensation_capture OR NEW.destination<>'original_payment' OR NEW.policy_version<>'original_payment_v1' OR NEW.amount_toman::numeric<>p.amount::numeric-used OR NOT verified OR NOT EXISTS(SELECT 1 FROM order_payment_receipts a JOIN order_payment_receipts b ON b.order_id=a.order_id AND b.payment_id<>a.payment_id AND b.funding_kind='gateway' WHERE a.payment_id=p.id AND (b.created_at,b.id COLLATE "C")<(a.created_at,a.id COLLATE "C")) THEN RAISE EXCEPTION 'extra capture must reserve exact verified later source'; END IF;
  NEW.status:='pending';NEW.state_reason:='awaiting_original_provider';
 ELSIF NEW.purpose='legacy_reservation' THEN
  IF NEW.destination<>'review' OR NEW.policy_version<>'legacy_review_v1' THEN RAISE EXCEPTION 'legacy refund reservation cannot claim verified money moved'; END IF;
  IF NEW.business_id='refund-history:'||o.id THEN
   IF NOT EXISTS(SELECT 1 FROM refunds WHERE order_id=o.id) OR NEW.amount_toman::numeric<>p.amount::numeric-used THEN RAISE EXCEPTION 'retained old PSP claims can only hold remaining source for review'; END IF;
  ELSIF NEW.business_id='extra-history:'||p.id THEN
   IF NOT verified OR NOT p.compensation_capture OR NOT EXISTS(SELECT 1 FROM payment_transactions other WHERE other.order_id=o.id AND other.id<>p.id AND other.status='paid') AND NOT o.inventory_captured AND o.status NOT IN ('shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed') OR NEW.amount_toman::numeric<>p.amount::numeric-used THEN RAISE EXCEPTION 'unreconciled extra money requires fulfilled legacy funding slot'; END IF;
  ELSIF NEW.business_id='ambiguous-history:'||o.id THEN
   IF (SELECT COUNT(*) FROM payment_transactions pt WHERE pt.order_id=o.id AND (pt.status IN ('paid','refunded') OR EXISTS(SELECT 1 FROM order_payment_receipts r WHERE r.payment_id=pt.id)))<2 OR NOT EXISTS(SELECT 1 FROM ledger_entries l WHERE l.related_order_or_dispute=o.id AND l.target_account=o.id AND l.target_type='platform' AND l.type='credit' AND l.reason LIKE 'استرداد وجه سفارش:%') OR NEW.amount_toman::numeric<>p.amount::numeric-used THEN RAISE EXCEPTION 'ambiguous order-level history can only hold remaining multiple sources'; END IF;
  ELSIF left(NEW.business_id,7)='ledger:' THEN
   SELECT * INTO old_ledger FROM ledger_entries WHERE id=substring(NEW.business_id FROM 8) AND related_order_or_dispute=o.id AND target_account=o.id AND target_type='platform' AND type='credit' AND reason LIKE 'استرداد وجه سفارش:%';
   IF NOT FOUND OR old_ledger.amount NOT BETWEEN 1 AND 9007199254740991 THEN RAISE EXCEPTION 'documented legacy refund claim required'; END IF;
   SELECT COALESCE(SUM(amount_toman),0) INTO old_used FROM order_refund_obligations WHERE purpose='legacy_reservation' AND business_id=NEW.business_id;
   IF NEW.amount_toman::numeric<>LEAST(p.amount::numeric-used,old_ledger.amount::numeric-old_used) OR old_used+NEW.amount_toman>old_ledger.amount THEN RAISE EXCEPTION 'legacy claim allocation exceeds documented source or principal'; END IF;
  ELSIF NEW.business_id='return-history:'||o.id THEN
   IF NOT EXISTS(SELECT 1 FROM customer_returns r WHERE r.order_id=o.id AND r.status='refunded' AND NOT EXISTS(SELECT 1 FROM order_refund_obligations c WHERE c.purpose='customer_return' AND c.business_id=r.id::text)) OR NEW.amount_toman::numeric<>p.amount::numeric-used THEN RAISE EXCEPTION 'unreconciled historical return can only hold remaining principal for review'; END IF;
  ELSIF NEW.business_id='dispute-history:'||o.id THEN
   IF NOT EXISTS(SELECT 1 FROM disputes d WHERE d.order_id=o.id AND d.status='resolved' AND d.resolution_action LIKE 'refund%' AND d.claim_amount>0 AND NOT EXISTS(SELECT 1 FROM order_refund_obligations c WHERE c.purpose='dispute' AND c.business_id=d.id)) OR NEW.amount_toman::numeric<>p.amount::numeric-used THEN RAISE EXCEPTION 'unreconciled old dispute can only hold remaining source'; END IF;
  ELSIF NEW.business_id='payment-refunded:'||p.id THEN
   IF p.status<>'refunded' OR NEW.amount_toman::numeric<>p.amount::numeric-used THEN RAISE EXCEPTION 'legacy refunded marker can only hold its unresolved remaining source'; END IF;
  ELSE RAISE EXCEPTION 'supported historical refund evidence required';
  END IF;
  NEW.status:='unknown';NEW.state_reason:=CASE WHEN NEW.business_id='return-history:'||o.id THEN 'legacy_return_unreconciled' ELSE 'legacy_refund_unverified' END;
 ELSIF NEW.purpose IN ('customer_return','admin_refund','dispute') THEN
  IF NEW.policy_version<>(CASE WHEN NEW.purpose='customer_return' THEN 'return_wallet_v1' ELSE 'original_payment_v1' END) OR NEW.destination<>(CASE WHEN NEW.purpose='customer_return' THEN 'wallet' ELSE 'original_payment' END) THEN RAISE EXCEPTION 'unsupported refund destination policy'; END IF;
  IF NEW.destination='wallet' AND (NOT verified OR o.customer_user_id IS NULL) THEN RAISE EXCEPTION 'wallet refund requires verified real funding and exact customer'; END IF;
  SELECT COALESCE(SUM(amount_toman),0) INTO business_total FROM order_refund_obligations WHERE order_id=o.id AND purpose=NEW.purpose AND business_id=NEW.business_id;
  IF NEW.purpose='customer_return' THEN
   SELECT SUM(oi.final_unit_price::numeric*ri.qty) INTO return_total FROM customer_returns r JOIN return_items ri ON ri.return_id=r.id JOIN order_items oi ON oi.id::text=ri.order_item_id WHERE r.id::text=NEW.business_id AND r.order_id=o.id AND r.customer_id=o.customer_user_id AND r.status='received' AND ri.received_at IS NOT NULL AND ri.qc_status IN ('ok','defective');
   IF return_total IS NULL OR business_total+NEW.amount_toman>return_total THEN RAISE EXCEPTION 'refund exceeds received return principal'; END IF;
  ELSIF NEW.purpose='dispute' THEN
   SELECT claim_amount INTO return_total FROM disputes WHERE id=NEW.business_id AND order_id=o.id AND resolution_action LIKE 'refund%';
   IF return_total IS NULL OR business_total+NEW.amount_toman>return_total THEN RAISE EXCEPTION 'refund exceeds resolved dispute principal'; END IF;
  END IF;
  NEW.status:=CASE WHEN verified THEN 'pending' ELSE 'unknown' END;
  NEW.state_reason:=CASE WHEN verified THEN CASE WHEN NEW.destination='wallet' THEN 'awaiting_wallet_credit' ELSE 'awaiting_original_provider' END ELSE 'funding_unverified' END;
 ELSE RAISE EXCEPTION 'unsupported refund purpose';
 END IF;
 NEW.source_amount_toman:=p.amount;NEW.created_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER refund_obligation_insert BEFORE INSERT ON order_refund_obligations FOR EACH ROW EXECUTE FUNCTION guard_refund_obligation_insert();
CREATE FUNCTION guard_payment_refund_source_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.compensation_capture IS DISTINCT FROM OLD.compensation_capture THEN
  IF OLD.compensation_capture OR NOT NEW.compensation_capture OR NEW.status<>'paid' OR NOT (EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=OLD.id) OR (NEW.amount BETWEEN 1 AND 9007199254740991 AND length(btrim(COALESCE(NEW.rrn,'')))>0 AND EXISTS(SELECT 1 FROM orders WHERE id=OLD.order_id AND status='cancelled'))) OR NOT (EXISTS(SELECT 1 FROM orders WHERE id=OLD.order_id AND (status IN ('cancelled','shipped','delivered','delivered_unconfirmed','delivered_confirmed','delivery_disputed') OR inventory_captured)) OR EXISTS(SELECT 1 FROM payment_transactions p WHERE p.order_id=OLD.order_id AND p.id<>OLD.id AND p.status IN ('pending','paid') AND NOT p.compensation_capture)) THEN RAISE EXCEPTION 'compensation classification requires retained verified late or extra capture'; END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM order_payment_verifications WHERE payment_id=OLD.id) OR EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=OLD.id) OR EXISTS(SELECT 1 FROM order_refund_obligations WHERE source_payment_id=OLD.id) THEN
  IF (NEW.id,NEW.order_id,NEW.amount,NEW.gateway_token) IS DISTINCT FROM (OLD.id,OLD.order_id,OLD.amount,OLD.gateway_token) THEN RAISE EXCEPTION 'captured payment/refund source identity cannot change'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER payment_refund_source_identity BEFORE UPDATE ON payment_transactions FOR EACH ROW EXECUTE FUNCTION guard_payment_refund_source_identity();
CREATE FUNCTION guard_order_refund_customer_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.customer_user_id IS DISTINCT FROM OLD.customer_user_id AND (EXISTS(SELECT 1 FROM order_payment_verifications WHERE order_id=OLD.id) OR EXISTS(SELECT 1 FROM order_payment_receipts WHERE order_id=OLD.id) OR EXISTS(SELECT 1 FROM order_refund_obligations WHERE order_id=OLD.id)) THEN RAISE EXCEPTION 'funded order customer identity cannot change'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_refund_customer_identity BEFORE UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION guard_order_refund_customer_identity();

-- A journal insert is the wallet effect. Outcome evidence cannot assert credit
-- unless this exact journal exists, and a failed transaction rolls both back.
CREATE TABLE order_refund_wallet_credits (
 obligation_id TEXT PRIMARY KEY REFERENCES order_refund_obligations(id) ON DELETE RESTRICT,
 customer_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 amount_toman BIGINT NOT NULL CHECK(amount_toman BETWEEN 1 AND 9007199254740991),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE order_refund_attempts (
 id TEXT PRIMARY KEY,
 obligation_id TEXT NOT NULL UNIQUE REFERENCES order_refund_obligations(id) ON DELETE RESTRICT,
 source_payment_id TEXT NOT NULL REFERENCES payment_transactions(id) ON DELETE RESTRICT,
 amount_toman BIGINT NOT NULL CHECK(amount_toman BETWEEN 1 AND 9007199254740991),
 currency TEXT NOT NULL CHECK(currency='TOMAN'),
 destination TEXT NOT NULL CHECK(destination='original_payment'),
 operation_key TEXT NOT NULL UNIQUE,
 original_reference TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER refund_attempt_identity BEFORE UPDATE OR DELETE ON order_refund_attempts FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE FUNCTION guard_refund_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o order_refund_obligations%ROWTYPE;parent_id TEXT;
BEGIN
 SELECT order_id INTO parent_id FROM order_refund_obligations WHERE id=NEW.obligation_id;
 PERFORM 1 FROM orders WHERE id=parent_id FOR UPDATE;
 SELECT * INTO o FROM order_refund_obligations WHERE id=NEW.obligation_id;
 IF o.id IS NULL OR o.destination<>'original_payment' OR (NEW.source_payment_id,NEW.amount_toman,NEW.currency,NEW.destination,NEW.operation_key) IS DISTINCT FROM (o.source_payment_id,o.amount_toman,o.currency,o.destination,'refund:'||o.id) OR NOT EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=o.source_payment_id AND funding_kind='gateway' AND amount_toman=o.source_amount_toman AND provider_reference=NEW.original_reference) THEN RAISE EXCEPTION 'provider attempt requires exact verified original source and reserved operation'; END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER refund_attempt_insert BEFORE INSERT ON order_refund_attempts FOR EACH ROW EXECUTE FUNCTION guard_refund_attempt();
CREATE TABLE order_refund_events (
 id TEXT PRIMARY KEY,
 event_no BIGSERIAL NOT NULL UNIQUE,
 obligation_id TEXT NOT NULL REFERENCES order_refund_obligations(id) ON DELETE RESTRICT,
 source_payment_id TEXT NOT NULL REFERENCES payment_transactions(id) ON DELETE RESTRICT,
 amount_toman BIGINT NOT NULL CHECK(amount_toman BETWEEN 1 AND 9007199254740991),
 currency TEXT NOT NULL CHECK(currency='TOMAN'),
 destination TEXT NOT NULL CHECK(destination IN ('original_payment','wallet','review')),
 status TEXT NOT NULL CHECK(status IN ('pending','succeeded','failed','unknown')),
 operation_key TEXT NOT NULL,
 provider_reference TEXT NOT NULL DEFAULT '',
 reason TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX order_refund_events_claim ON order_refund_events(obligation_id,event_no DESC);
CREATE TRIGGER refund_wallet_credit_identity BEFORE UPDATE OR DELETE ON order_refund_wallet_credits FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE TRIGGER refund_event_identity BEFORE UPDATE OR DELETE ON order_refund_events FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE FUNCTION guard_refund_wallet_credit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o order_refund_obligations%ROWTYPE;parent_id TEXT;current_balance BIGINT;
BEGIN
 SELECT order_id INTO parent_id FROM order_refund_obligations WHERE id=NEW.obligation_id;
 PERFORM 1 FROM orders WHERE id=parent_id FOR UPDATE;
 SELECT * INTO o FROM order_refund_obligations WHERE id=NEW.obligation_id;
 PERFORM 1 FROM payment_transactions WHERE id=o.source_payment_id FOR UPDATE;
 PERFORM 1 FROM users WHERE id=o.customer_id FOR UPDATE;
 IF o.id IS NULL OR o.destination<>'wallet' OR (o.customer_id,o.amount_toman) IS DISTINCT FROM (NEW.customer_id,NEW.amount_toman) OR NOT EXISTS(SELECT 1 FROM order_payment_receipts WHERE payment_id=o.source_payment_id AND amount_toman=o.source_amount_toman AND funding_kind='gateway') THEN RAISE EXCEPTION 'wallet credit requires exact verified reserved principal'; END IF;
 IF EXISTS(SELECT 1 FROM order_refund_wallet_credits WHERE obligation_id=o.id) THEN RETURN NEW; END IF;
 SELECT COALESCE(balance,0) INTO current_balance FROM customer_wallets WHERE customer_id=o.customer_id;
 IF COALESCE(current_balance,0)::numeric+NEW.amount_toman>9007199254740991 THEN RAISE EXCEPTION 'wallet balance exceeds supported money'; END IF;
 INSERT INTO customer_wallets(customer_id,balance) VALUES(o.customer_id,NEW.amount_toman) ON CONFLICT(customer_id) DO UPDATE SET balance=customer_wallets.balance+EXCLUDED.balance,updated_at=NOW();
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER refund_wallet_credit BEFORE INSERT ON order_refund_wallet_credits FOR EACH ROW EXECUTE FUNCTION guard_refund_wallet_credit();
CREATE FUNCTION guard_refund_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o order_refund_obligations%ROWTYPE;parent_id TEXT;
BEGIN
 SELECT order_id INTO parent_id FROM order_refund_obligations WHERE id=NEW.obligation_id;
 PERFORM 1 FROM orders WHERE id=parent_id FOR UPDATE;
 SELECT * INTO o FROM order_refund_obligations WHERE id=NEW.obligation_id;
 IF o.id IS NULL OR (NEW.source_payment_id,NEW.amount_toman,NEW.currency,NEW.destination,NEW.operation_key) IS DISTINCT FROM (o.source_payment_id,o.amount_toman,o.currency,o.destination,'refund:'||o.id) THEN RAISE EXCEPTION 'refund outcome source, amount, currency, destination or operation changed'; END IF;
 IF EXISTS(SELECT 1 FROM order_refund_events WHERE obligation_id=o.id AND status='succeeded' AND id<>NEW.id) THEN RAISE EXCEPTION 'confirmed refund outcome is terminal'; END IF;
 IF NEW.status='succeeded' THEN
  IF o.destination='wallet' THEN
   IF NOT EXISTS(SELECT 1 FROM order_refund_wallet_credits WHERE obligation_id=o.id AND customer_id=o.customer_id AND amount_toman=o.amount_toman) THEN RAISE EXCEPTION 'wallet success requires atomic credit journal'; END IF;
  ELSIF o.destination='original_payment' THEN
   IF btrim(NEW.provider_reference)='' OR NOT EXISTS(SELECT 1 FROM order_refund_attempts WHERE obligation_id=o.id AND operation_key=NEW.operation_key AND source_payment_id=NEW.source_payment_id AND amount_toman=NEW.amount_toman AND currency=NEW.currency AND destination=NEW.destination) THEN RAISE EXCEPTION 'original success requires exact retained provider attempt'; END IF;
  ELSE RAISE EXCEPTION 'review hold cannot assert money moved'; END IF;
 END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER refund_event_insert BEFORE INSERT ON order_refund_events FOR EACH ROW EXECUTE FUNCTION guard_refund_event();

CREATE TABLE order_refund_request_keys (
 order_id TEXT NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
 client_key TEXT NOT NULL CHECK(length(client_key) BETWEEN 1 AND 128),
 fingerprint TEXT NOT NULL,
 result JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(order_id,client_key)
);
CREATE TRIGGER refund_request_key_identity BEFORE UPDATE OR DELETE ON order_refund_request_keys FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();

CREATE FUNCTION guard_payment_compensation_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.compensation_capture THEN RAISE EXCEPTION 'a new checkout cannot declare compensation before independent verification'; END IF;RETURN NEW;
END $$;
CREATE TRIGGER payment_compensation_insert BEFORE INSERT ON payment_transactions FOR EACH ROW EXECUTE FUNCTION guard_payment_compensation_insert();
CREATE UNIQUE INDEX order_refund_confirmed_provider_reference ON order_refund_events(provider_reference) WHERE status='succeeded' AND destination='original_payment';
CREATE TABLE order_refund_poll_state (
 obligation_id TEXT PRIMARY KEY REFERENCES order_refund_obligations(id) ON DELETE RESTRICT,
 next_reconcile_at TIMESTAMPTZ NOT NULL
);

-- All new refund writers use source-linked claims. Preserve old records for
-- inquiry; an obsolete writer cannot append an unreserved PSP operation.
CREATE TRIGGER legacy_refund_evidence BEFORE INSERT OR UPDATE OR DELETE ON refunds FOR EACH ROW EXECUTE FUNCTION immutable_order_refund_evidence();
CREATE TABLE order_expiry_review_state (
 order_id TEXT PRIMARY KEY REFERENCES orders(id) ON DELETE RESTRICT,
 next_retry_at TIMESTAMPTZ NOT NULL,
 reason TEXT NOT NULL CHECK(reason='candidate_rejected')
);

CREATE FUNCTION require_wallet_refund_outcome() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM order_refund_events e WHERE e.obligation_id=NEW.obligation_id AND e.status='succeeded' AND e.destination='wallet' AND e.amount_toman=NEW.amount_toman AND e.currency='TOMAN') THEN RAISE EXCEPTION 'wallet credit and its immutable success outcome must commit together'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER refund_wallet_requires_outcome AFTER INSERT ON order_refund_wallet_credits DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_wallet_refund_outcome();

-- Classification is only an index exception for a recorded compensation fact;
-- it is never funding verification. Every such capture must have its full
-- source principal reserved atomically, including legacy unknown funding.
CREATE FUNCTION require_compensation_principal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.compensation_capture AND NEW.status='paid' AND COALESCE((SELECT SUM(amount_toman) FROM order_refund_obligations WHERE source_payment_id=NEW.id),0)<>NEW.amount THEN RAISE EXCEPTION 'compensation capture must retain its full source principal before commit'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER compensation_requires_principal AFTER UPDATE ON payment_transactions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION require_compensation_principal();
