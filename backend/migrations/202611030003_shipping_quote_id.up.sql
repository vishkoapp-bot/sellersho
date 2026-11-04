-- FIX 5 (full live parity, Team 03 G1/G13): record the shipping quote
-- an order was placed with. The quote itself is computed (not stored
-- as rows): its deterministic quoteId is verified at order time by
-- recomputing it, and the id is recorded here alongside the quoted
-- shipping_cost so the order can always be traced back to its quote.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS shipping_quote_id TEXT;
