-- Existing lines keep NULL; never invent historical product descriptions.
ALTER TABLE order_items ADD COLUMN product_title_snapshot TEXT;
CREATE FUNCTION guard_checkout_item_title() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.product_title_snapshot IS NOT NULL AND NEW.product_title_snapshot IS DISTINCT FROM OLD.product_title_snapshot THEN RAISE EXCEPTION 'checkout product title is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER checkout_item_title_guard BEFORE UPDATE OF product_title_snapshot ON order_items FOR EACH ROW EXECUTE FUNCTION guard_checkout_item_title();

-- New checkout identities only; historical orders and financial facts are untouched.
CREATE TABLE checkout_attempts (
 customer_user_id UUID NOT NULL REFERENCES users(id),
 attempt_id TEXT NOT NULL CHECK(length(attempt_id) BETWEEN 1 AND 128),
 payload_hash TEXT CHECK(payload_hash ~ '^[0-9a-f]{64}$'),
 state TEXT NOT NULL DEFAULT 'open' CHECK(state IN ('open','completed','abandoned')),
 order_id TEXT UNIQUE REFERENCES orders(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(customer_user_id,attempt_id),
 CHECK((state='completed')=(order_id IS NOT NULL)),
 CHECK(state='abandoned' OR payload_hash IS NOT NULL)
);
CREATE FUNCTION guard_checkout_attempt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'checkout attempt identity is permanent'; END IF;
 IF TG_OP='UPDATE' AND (NEW.customer_user_id,NEW.attempt_id,NEW.payload_hash,NEW.created_at) IS DISTINCT FROM (OLD.customer_user_id,OLD.attempt_id,OLD.payload_hash,OLD.created_at) THEN RAISE EXCEPTION 'checkout payload is immutable'; END IF;
 IF TG_OP='UPDATE' AND OLD.state<>'open' AND (NEW.state,NEW.order_id) IS DISTINCT FROM (OLD.state,OLD.order_id) THEN RAISE EXCEPTION 'resolved checkout is immutable'; END IF;
 IF NEW.order_id IS NOT NULL AND EXISTS(SELECT 1 FROM order_items WHERE order_id=NEW.order_id AND product_title_snapshot IS NULL) THEN RAISE EXCEPTION 'checkout requires complete line snapshots'; END IF;
 IF NEW.order_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM orders WHERE id=NEW.order_id AND customer_user_id=NEW.customer_user_id AND idempotency_key=NEW.attempt_id) THEN RAISE EXCEPTION 'checkout requires its exact owned order'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER checkout_attempt_guard BEFORE INSERT OR UPDATE OR DELETE ON checkout_attempts FOR EACH ROW EXECUTE FUNCTION guard_checkout_attempt();
