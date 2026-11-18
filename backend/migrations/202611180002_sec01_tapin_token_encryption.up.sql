-- 202611180002_sec01_tapin_token_encryption (SEC-01, gate G-10).
--
-- CORRECTION of the migration 202610280007 comment, which claimed that
-- platform/supplier Tapin tokens are stored "AES-256-GCM enc:v1: with
-- TAPIN_TOKEN_KEY" and read only via GetPlatformTapinCredentials.
-- That claim was FALSE for the code as shipped: no AES-GCM consumer
-- existed, TAPIN_TOKEN_KEY had no reader, GetPlatformTapinCredentials
-- did not exist, and suppliers.tapin_token was written in plaintext
-- by SupplierRepository.UpdateTapinToken.
--
-- The 202610280007 file itself is NOT edited: applied migrations are
-- checksum-pinned (internal/migrations), so the correction lives here
-- instead — as the authoritative record on the columns themselves
-- (COMMENT ON COLUMN, idempotent) plus the implementation that now
-- backs the claim:
--
--   * internal/tapin.TokenCipher — real AES-256-GCM, format
--     "enc:v1:<base64(nonce || ciphertext)>", key = SHA-256 of the
--     TAPIN_TOKEN_KEY material, optional previous-key dual-read.
--   * SupplierRepository.UpdateTapinToken — encrypt on write.
--   * TapinRepository.GetSupplierCredentials / SupplierRepository.
--     GetSupplierByID — decrypt on read (legacy plaintext passes
--     through until re-encrypted).
--   * TapinRepository.ReencryptSupplierTapinTokens — the in-place
--     migration, run at server startup after the SQL migrations.
--     Existing plaintext tokens are encrypted in place, never
--     dropped, never mass-revoked; every row is decrypt-checked
--     before and after its write.
--
-- platform_settings.tapin_platform_token still has no writer/reader
-- in Go: 0 rows hold a value live, and no code path can write one.
-- If a platform-token writer is ever built, it must use the same
-- TokenCipher; until then this column stays NULL (not configured).
COMMENT ON COLUMN platform_settings.tapin_platform_token IS
  'SEC-01 (202611180002): no writer/reader in code; NULL = not configured. If a writer is ever built it MUST store AES-256-GCM enc:v1: via internal/tapin.TokenCipher under TAPIN_TOKEN_KEY. The 202610280007 comment claiming such storage already existed was false; the real implementation landed with this migration.';
COMMENT ON COLUMN suppliers.tapin_token IS
  'SEC-01 (202611180002): stored AES-256-GCM enc:v1: via internal/tapin.TokenCipher under TAPIN_TOKEN_KEY (encrypt on write, decrypt on read). Legacy plaintext rows are re-encrypted in place at startup by TapinRepository.ReencryptSupplierTapinTokens; plaintext must never be written by new code. The 202610280007 comment claiming such storage already existed was false; the real implementation landed with this migration.';
