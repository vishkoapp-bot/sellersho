-- 202610250010 rollback: re-insert the seed putaway task exactly as
-- 000008 created it (50 units, pending). Only meaningful on a DB that
-- never ran the QC flow that superseded it.
INSERT INTO putaway_tasks (id, product_id, inbound_id, bin_location, quantity, status)
VALUES ('PUT-INIT-001', 'prod-1', 'INB-TEST-PENDING-01', 'A-12-05', 50, 'pending')
ON CONFLICT (id) DO NOTHING;
