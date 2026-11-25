DROP TRIGGER IF EXISTS supplier_dispatch_commitment_insert ON supplier_shipments;
DROP FUNCTION IF EXISTS set_supplier_dispatch_commitment();
ALTER TABLE supplier_shipments DROP COLUMN IF EXISTS dispatch_due_at;
