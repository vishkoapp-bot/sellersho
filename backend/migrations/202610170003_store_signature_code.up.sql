-- 202610170003_store_signature_code: server-side contract signature tracking code (L18).
--
-- The "SIG-DRP-XXXX" code shown during dropshipper onboarding was generated
-- client-side with Math.random() on every render, so it changed constantly
-- and was never persisted. It is now generated server-side (crypto/rand) at
-- store-registration/contract-signing time, stored on the store record, and
-- returned in the registration response; the frontend displays the persisted
-- value.
--
-- Partial unique index: pre-existing stores keep NULL and never collide.

ALTER TABLE stores ADD COLUMN IF NOT EXISTS contract_signature_code TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS stores_contract_signature_code_uidx
    ON stores (contract_signature_code)
    WHERE contract_signature_code IS NOT NULL;
