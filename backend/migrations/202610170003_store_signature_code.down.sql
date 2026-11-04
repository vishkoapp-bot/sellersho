-- Rollback for 202610170003_store_signature_code.
--
-- Drops the contract signature code column/index. Does NOT touch store rows
-- themselves. Note: after rollback the frontend must not rely on
-- contractSignatureCode in the registration response (it will be absent).

DROP INDEX IF EXISTS stores_contract_signature_code_uidx;
ALTER TABLE stores DROP COLUMN IF EXISTS contract_signature_code;
