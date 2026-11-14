# Credential lifecycle: SEC-01 source checkpoint

Production Compose now requires runtime injection for POSTGRES_PASSWORD,
REDIS_PASSWORD, DATABASE_URL, REDIS_URL, JWT_SECRET, TRACKING_WEBHOOK_SECRET and
TAPIN_TOKEN_KEY. Values are deliberately omitted. Redis health checks use
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

TAPIN_TOKEN_KEY appears in production Compose but current Go/TypeScript sources
have no encryption/decryption consumer. This does not prove historical encrypted
rows are absent. Inventory ciphertext formats and their writer revisions, prove
old-key decryption in isolated sanitized fixtures, design versioned dual-read/
new-write key IDs, reencrypt in bounded audited batches, and verify rollback
compatibility before any key retirement. Never replace an encryption key blindly.

Local evidence: configuration regression first reproduced acceptance of dev JWT,
dev webhook and short webhook in production; all now reject. Existing config
suite and JWT rotation drill pass. Six Python checks pass: required injection, production literal absence,
actual Redis unprivileged process UID,
no Redis healthcheck password argument, metadata-only inventory, and merged
Compose missing-secret refusal / ephemeral injected-secret acceptance. Docker
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
