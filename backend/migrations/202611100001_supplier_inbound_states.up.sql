-- Fresh installations still had the older seven-state CHECK, although the
-- supplier flow creates scheduled inbounds and can cancel them. Production
-- already permits these two states; persist that schema repair in the chain.
ALTER TABLE inbound_shipments
  DROP CONSTRAINT IF EXISTS inbound_shipments_status_check,
  ADD CONSTRAINT inbound_shipments_status_check CHECK (status IN (
    'scheduled','in_transit','pending','receiving','received',
    'putaway_completed','qc_rejected','completed','cancelled'
  ));
