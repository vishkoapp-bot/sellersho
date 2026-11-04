-- Down migration for 202610250301: drops only the tables this stream owns.
-- The shared customer_returns columns (return_code,
-- return_shipping_amount, return_shipping_bearer) are owned by the
-- money/warehouse streams and are deliberately left in place.
DROP TABLE IF EXISTS supplier_return_back_events;
DROP TABLE IF EXISTS supplier_return_back_requests;
