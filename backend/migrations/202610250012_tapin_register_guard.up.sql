-- Tapin ship-with-tapin idempotency guard (night-audit F2 sharp edge).
--
-- RegisterOrder is non-idempotent: if it succeeds at Tapin but the
-- response never arrives (slow-upstream timeout), a blind retry
-- registers a SECOND Tapin shipment for the same Selersho order.
-- This stamp records an attempt whose outcome is UNKNOWN so the retry
-- fails closed instead of double-registering. It is set only on
-- unknown-outcome failures and cleared on definite rejection, on
-- success, or by an admin after verifying in the Tapin panel.
ALTER TABLE supplier_shipments
    ADD COLUMN IF NOT EXISTS tapin_register_attempted_at TIMESTAMPTZ;
