ALTER TABLE supplier_shipments ADD COLUMN IF NOT EXISTS dispatch_due_at timestamptz;

CREATE OR REPLACE FUNCTION set_supplier_dispatch_commitment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.dispatch_due_at IS NULL THEN
        NEW.dispatch_due_at := NEW.created_at + make_interval(days => (
            SELECT GREATEST(1, LEAST(30, COALESCE(MAX(p.preparation_days), 1)))
            FROM order_items oi JOIN products p ON p.id = oi.product_id
            WHERE oi.order_id = NEW.order_id AND p.supplier_id = NEW.supplier_id
        ));
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS supplier_dispatch_commitment_insert ON supplier_shipments;
CREATE TRIGGER supplier_dispatch_commitment_insert BEFORE INSERT ON supplier_shipments
FOR EACH ROW EXECUTE FUNCTION set_supplier_dispatch_commitment();
