-- Customer dispute reason (2026-11-01, Andrew's explicit order: create a
-- column for it). POST /orders/{id}/dispute previously accepted {reason}
-- but only echoed it: orders had no dispute-reason column, and the
-- finance disputes table's type vocabulary (return_7day /
-- delivery_undelivered) does not cover a general customer dispute, so
-- writing there would mislabel the row for the finance queue. This
-- column lives on the order itself, next to disputed_at.
--
-- NULL = no reason given (the dist always sends a reason — it defaults
-- client-side to "buyer says the parcel has not arrived yet" — but an
-- empty body must not invent one server-side).
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS dispute_reason TEXT;
