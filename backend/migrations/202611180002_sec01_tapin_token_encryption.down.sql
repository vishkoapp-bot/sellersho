-- 202611180002_sec01_tapin_token_encryption.down.sql
-- SEC-01: remove the corrective column comments.
--
-- THIS FILE IS NOT A ROLLBACK. It does NOT decrypt or alter any
-- stored token: the "enc:v1:" values stay encrypted, and only a
-- binary with the SEC-01 cipher can read them. The migration runner
-- has no down support (it globs *.up.sql only), so this file never
-- executes in a deploy at all.
--
-- Rollback after the first prod boot of the SEC-01 binary is
-- RE-DEPLOY THE SEC-01 BINARY — never restore the old binary (it
-- has no cipher and would hand "enc:v1:" ciphertext to Tapin as the
-- token). The deploy backup's postgres.dump is the pre-boot
-- plaintext-token source; per-row re-encryption failures stay
-- untouched for manual handling. See sec01-rotation-plan.md §4/§8.
COMMENT ON COLUMN platform_settings.tapin_platform_token IS NULL;
COMMENT ON COLUMN suppliers.tapin_token IS NULL;
