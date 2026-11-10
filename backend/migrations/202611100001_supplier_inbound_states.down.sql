-- Refuse rollback while scheduled/cancelled rows exist; never rewrite or
-- delete an inbound to force an older constraint. This ALTER is atomic.
ALTER TABLE inbound_shipments
  DROP CONSTRAINT IF EXISTS inbound_shipments_status_check,
  ADD CONSTRAINT inbound_shipments_status_check CHECK (status IN (
    'in_transit','pending','receiving','received',
    'putaway_completed','qc_rejected','completed'
  ));
