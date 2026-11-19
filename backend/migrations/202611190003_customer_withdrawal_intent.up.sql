-- Historical requests have no known client intent and must retain NULL.
ALTER TABLE customer_withdrawal_requests
    ADD COLUMN request_key_sha256 BYTEA CHECK (octet_length(request_key_sha256) = 32);

CREATE UNIQUE INDEX idx_customer_withdrawal_intent
    ON customer_withdrawal_requests (customer_id, request_key_sha256)
    WHERE request_key_sha256 IS NOT NULL;
