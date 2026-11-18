# Credential lifecycle: SEC-01 source checkpoint

Production Compose now requires runtime injection for POSTGRES_PASSWORD,
REDIS_PASSWORD, DATABASE_URL, REDIS_URL, JWT_SECRET, TRACKING_WEBHOOK_SECRET
and TAPIN_TOKEN_KEY. PSP_TERMINAL_ID / PSP_API_KEY are OPTIONAL injection
(`${...:-}`): a partial pair fails startup, both absent boots with a
warning (SEC-01 re-scope, supervisor F-02). The dead TEST_OTP_*/LOCAL_OTP_MODE
lines were removed (no reader in any language). Values are deliberately omitted. Redis health checks use
REDISCLI_AUTH rather than password command arguments. Redis AOF is explicit.
No runtime credential was replaced and no production restart was performed.

The Go production configuration already rejected absent signing/database/Redis/
webhook settings. It now also rejects the known development signing/webhook
material and webhook secrets shorter than 32 characters. Development remains an
explicit opt-in configuration. These checks do not identify all historical
exposures: a random-looking committed literal may still have been used live.

Current/reachable-history production Compose exposure metadata is recorded in
credential-inventory.json. Only file, line, key name, revision, classification and
SHA256 fingerprint are emitted; source values, command text, runtime environments
and private files never leave memory. Environment-assignment fingerprints use
the value without surrounding quotes; Redis command/healthcheck records identify
source expressions, not individual credentials. Active use remains UNKNOWN.
Inventory excludes unrelated files; this is not a whole-history clean scan.

Before release, a named operator must compare source credential fingerprints to
actual restricted server configuration without displaying the values. Treat any
match as exposed. Preserve the current restricted runtime file and server-side
backup; do not put either in Git, logs, build arguments or frontend environment.
Keep production backup/config files mode 0600 and parent directories 0700, owned
by the deployment operator. Do not recursively change permissions on unrelated
services or move existing services. deploy.ps1 preserves runtime configuration.
The new required variable names must exist in that preserved runtime source
before deployment; do not silently derive or replace live passwords.

JWT rotation deliberately invalidates every old JWT. Prepare new signing
material in the restricted runtime source, schedule the coordinated switch of
all API replicas, verify old sessions fail and fresh authenticated sessions work,
and monitor login/OTP pressure. Do not restore an exposed signing key simply to
avoid reauthentication. The local drill uses fresh random keys, proves old-session
rejection and new-session acceptance, and prints neither keys nor tokens.

Database and Redis credentials require coordinated server-role/client changes,
connection drain, a tested new-auth health probe and a bounded compatibility
window. Do not delete the old role/password until every client has migrated.
Webhook rotation requires sender/provider support for overlapping verification
or an agreed cutover; replay/out-of-order callbacks must remain idempotent. These
provider and server operations have not been performed by this checkpoint.

Tapin token encryption (SEC-01, implemented; migration 202611180002
corrects the false 202610280007 comment that claimed AES-256-GCM storage
already existed). Supplier tokens are stored AES-256-GCM "enc:v1:" via
internal/tapin.TokenCipher, key = SHA-256 of the TAPIN_TOKEN_KEY material,
fresh 12-byte nonce per encryption. SupplierRepository.UpdateTapinToken
encrypts on write; TapinRepository.GetSupplierCredentials and
SupplierRepository.GetSupplierByID decrypt on read (legacy plaintext rows
pass through until re-encrypted). TapinRepository.
ReencryptSupplierTapinTokens encrypts legacy plaintext rows IN PLACE at
server startup (compare-and-swap write, decrypt-check before and after,
per-row failures left untouched and reported, never dropped). A TOTAL
failure (token rows exist and none could be re-encrypted) now FAILS the
boot (critic F-04 fix), so a wrong TAPIN_TOKEN_KEY rolls the deploy
back before any row changes. Rollback AFTER the first prod boot is
re-deploying the SEC-01 binary, never restoring the old binary (it
cannot read `enc:v1:` rows); the deploy backup's `postgres.dump` is
the pre-boot plaintext-token source — see rotation plan §8. The
202611180002 down file is NOT a rollback (it only clears comments).
platform_settings.tapin_platform_token column still has no writer/reader
(0 rows live); any future writer must use the same cipher.
Key rotation: deploy the new TAPIN_TOKEN_KEY with the old one as
TAPIN_TOKEN_PREVIOUS_KEY (decryption fallback, dual-read); the startup
pass re-encrypts every row to the new key; then drop
TAPIN_TOKEN_PREVIOUS_KEY. Dropping it has NO automatic mechanism
(SEC-01 fix, critic F-03): the deploy runner re-harvests the running
container's env into `/opt/drup/.env.deploy.runtime.json` on EVERY
deploy, so the previous key comes back unless the operator HAND-EDITS
that runtime JSON AND the container env (recreate the backend with
the key removed from its environment, or the equivalent operator
step). Note the asymmetry: TAPIN_TOKEN_KEY itself survives ONLY
through this harvest chain today (absent from prod `.env`). Rollback
is restoring the old key as TAPIN_TOKEN_KEY. Never rotate the live
key without the previous-key window: rows encrypted under a dropped
key become unreadable.
LoadConfig requires TAPIN_TOKEN_KEY in production (fail-closed, >=32
chars) and enforces the PSP pair rule when PAYMENT_GATEWAY_MODE is
real: a PARTIAL pair (exactly one of PSP_TERMINAL_ID / PSP_API_KEY
set) fails startup, while both absent boots with a prominent warning
that real-mode payments are unverifiable (VerifyTransaction still
fails closed at payment time; no PSP is contracted yet — FIN-010 not
landed, and no gate change is needed when it lands). PSP keys, when
present, must also be >=32 chars. A missing
ALLOWED_ORIGINS fails startup (the silent sellershow.ir fallback is
removed); an unset APP_ENV is production behaviour, never a silent dev
fallback. Development defaults are ephemeral random values, only when
APP_ENV is explicitly development/dev/test/local, and the retired dev
literals are rejected in production by SHA-256 fingerprint (the values
themselves are gone from config.go and the production binary).

Local evidence: configuration regression first reproduced acceptance of dev JWT,
dev webhook and short webhook in production; all now reject. Existing config
suite and JWT rotation drill pass. Four Python checks pass here:
required injection, production literal absence, no Redis healthcheck
password argument, and metadata-only inventory. The other two checks
— actual Redis unprivileged process UID, and merged Compose
missing-secret refusal / ephemeral injected-secret acceptance — are
Docker-dependent: they error with `FileNotFoundError: 'docker'` on a
host without Docker and are NOT claimed as passed here (SEC-01 fix,
supervisor F-03 / critic F-07). The 4 runnable checks are wired into
the deploy path as a gate (`scripts/run_secret_checks_nodocker.py`,
invoked by `deploy.ps1` before the build and by
`scripts/deploy_remote.py` before compose validation); the 2
Docker-dependent checks must be run on a Docker host. Docker
Compose runs config --quiet; captured stdout/stderr are never printed. This does
not start containers or certify production credentials, provider continuity,
encrypted history compatibility, artifact hygiene or live rotation.

Remaining SEC-01 gates: whole tracked-source/history/artifact exposure inventory,
restricted active-use comparison, actual coordinated rotation and rollback drill
for used credentials, ciphertext compatibility proof, production permissions,
frontend distributable scan and independently reviewed release evidence.
SEC-01 remains OPEN and is not a production readiness claim.

Independent review caught a Redis privilege regression: a shell command bypassed
the official image entrypoint's redis-server branch. The disposable local UID
regression first failed with root UID. Re-entering docker-entrypoint.sh from the
shell restores official data ownership and privilege dropping; the same test now
passes without network access or reused data. Container removal checks its exact
unique test name and sellersho.test-run label. Official entrypoint reference:
https://raw.githubusercontent.com/redis/docker-library-redis/master/7.4/alpine/docker-entrypoint.sh.

Startup diagnostic follow-up: malformed database/Redis URLs can contain secret
material in third-party parser errors. Constructors now return value-free
initialization diagnostics and close clients after failed startup probes. The
regression first reproduced both leaks, then passed without any database
connection. Independent root review validated the exact three-file change against
committed HEAD. PostgreSQL startup Ping still needs the OPS deadline work.
