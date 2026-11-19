-- Retain retry protection for any request submitted on the new contract.
LOCK TABLE customer_withdrawal_requests IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM customer_withdrawal_requests WHERE request_key_sha256 IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove recorded customer withdrawal intent identities';
    END IF;
END $$;
ALTER TABLE customer_withdrawal_requests DROP COLUMN request_key_sha256;
