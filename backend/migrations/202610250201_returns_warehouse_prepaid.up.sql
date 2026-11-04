-- 202610250201: Returns-shipping policy (stream 1, returns-core).
--
-- Customer returns gain a destination central warehouse + prepaid shipment
-- tracking (user decision Oct 25, 2026):
--   warehouse_id     — the central warehouse the customer chose for the
--     return. NULL = not chosen (all pre-feature returns; also allowed
--     while prod has 0 warehouses — no city is ever hardcoded as a
--     default; the selector reads the warehouses table).
--   prepaid          — the customer prepaid the return courier (mandatory
--     before the parcel can be marked shipped; post-paid parcels are
--     refused at the warehouse and returned to the customer).
--   tracking_number  — the courier tracking number the customer reports
--     when marking the parcel shipped.
--   shipped_at       — when the customer marked the parcel shipped.
--
-- The QC cost split (defective → supplier pays, taste → customer pays)
-- and the supplier-returns panel are separate streams; this migration
-- only carries the fields this stream owns. All four columns use
-- IF NOT EXISTS / plain ADD so a parallel stream adding one of them
-- cannot break the apply.

ALTER TABLE customer_returns
  ADD COLUMN IF NOT EXISTS warehouse_id TEXT REFERENCES warehouses(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS prepaid BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS tracking_number TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS shipped_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS customer_returns_warehouse_idx ON customer_returns(warehouse_id);
