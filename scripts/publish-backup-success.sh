#!/bin/bash
# publish-backup-success.sh — OBS-01 D-02 backup-success signal publisher.
#
# Usage: scripts/publish-backup-success.sh <backup-dir> [kind]
#   backup-dir: directory holding the backup set
#   kind:       signal kind, default "nightly"
#
# BAK-01 layout (encrypted): the dir holds *.enc artifacts,
# MANIFEST.sha256 (plaintext hashes), MANIFEST.enc.sha256 (ciphertext
# hashes) and manifest.json. The publisher verifies the encrypted
# hashes, decrypts every artifact to a scratch dir, verifies the
# plaintext hashes + gzip integrity + pg_restore --list, and ONLY
# THEN upserts the verified timestamp into backup_signals via the
# local Postgres container. A failed verification exits non-zero and
# publishes NOTHING — the poller treats unknown backup state as
# unknown, never as success.
#
# Legacy layout (pre-BAK-01, unencrypted db.sql.gz / uploads.tar.gz
# / config.tar.gz): still supported — the publisher verifies all
# three artifacts (present, non-empty, gzip -t clean) as before.
#
# Env overrides (defaults are the prod values):
#   BAK_KEY_FILE (default /opt/drup/.backup-encryption-key) — needed
#     for the encrypted layout only.
#   BAK_PSQL_CMD — if set, the psql command is used verbatim (the
#     rehearsal/test harness sets it to a local psql). Otherwise the
#     docker-exec form is built from BACKUP_PG_CONTAINER
#     (drup_postgres), BACKUP_PG_USER (drup_admin), BACKUP_PG_DB
#     (drup_db).
# No credentials in this file: DB access is via `docker exec`
# local-socket auth, same as backup-auto.sh.
set -euo pipefail
umask 077

DIR="${1:-}"
KIND="${2:-nightly}"
if [ -z "$DIR" ]; then
  echo "usage: $0 <backup-dir> [kind]" >&2
  exit 2
fi
BAK_KEY_FILE="${BAK_KEY_FILE:-/opt/drup/.backup-encryption-key}"

if [ -n "${BAK_PSQL_CMD:-}" ]; then
  PSQL_CMD="$BAK_PSQL_CMD"
else
  CONTAINER="${BACKUP_PG_CONTAINER:-drup_postgres}"
  PGUSER="${BACKUP_PG_USER:-drup_admin}"
  PGDB="${BACKUP_PG_DB:-drup_db}"
  PSQL_CMD="docker exec $CONTAINER psql -U $PGUSER -d $PGDB -v ON_ERROR_STOP=1"
fi

publish() {
  local verified="$1"
  eval "$PSQL_CMD -c \"INSERT INTO backup_signals(kind,last_success_at,verified_artifacts,updated_at) VALUES('$KIND',now(),'$verified',now()) ON CONFLICT (kind) DO UPDATE SET last_success_at=EXCLUDED.last_success_at, verified_artifacts=EXCLUDED.verified_artifacts, updated_at=now();\""
}

if [ -f "$DIR/MANIFEST.enc.sha256" ]; then
  # --- BAK-01 encrypted layout ---
  [ -f "$BAK_KEY_FILE" ] || { echo "publish-backup-success: encryption key missing: $BAK_KEY_FILE" >&2; exit 1; }
  (cd "$DIR" && sha256sum -c MANIFEST.enc.sha256) >/dev/null \
    || { echo "publish-backup-success: encrypted manifest verification FAILED for $DIR" >&2; exit 1; }
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  cp "$DIR/MANIFEST.sha256" "$TMP/MANIFEST.sha256"
  # Decrypt every *.enc artifact, RECURSIVELY (--include files may
  # live in subdirectories such as extra/; a top-level *.enc glob
  # would silently skip them and the manifest check would then fail
  # on the missing plaintext).
  while IFS= read -r enc; do
    [ -n "$enc" ] || continue
    base="${enc%.enc}"
    mkdir -p "$TMP/$(dirname "$base")"
    openssl enc -d -aes-256-cbc -pbkdf2 -md sha256 -salt \
      -pass "file:$BAK_KEY_FILE" -in "$DIR/$enc" -out "$TMP/$base" \
      || { echo "publish-backup-success: decryption failed for $DIR/$enc" >&2; exit 1; }
  done < <(cd "$DIR" && find . -name '*.enc' -type f -printf '%P\n' | sort)
  (cd "$TMP" && sha256sum -c MANIFEST.sha256) >/dev/null \
    || { echo "publish-backup-success: plaintext manifest verification FAILED for $DIR" >&2; exit 1; }
  for f in db.sql.gz uploads.tar.gz config.tar.gz; do
    path="$TMP/$f"
    test -s "$path" || { echo "publish-backup-success: missing or empty $path" >&2; exit 1; }
    gzip -t "$path" || { echo "publish-backup-success: corrupt gzip $path" >&2; exit 1; }
  done
  VERIFIED="$(cd "$DIR" && find . -name '*.enc' -type f -printf '%P\n' | sort | tr '\n' ',' | sed 's/,$//')"
  publish "$VERIFIED"
  echo "BACKUP_SIGNAL_OK $KIND $DIR"
  exit 0
fi

# --- Legacy unencrypted layout (pre-BAK-01) ---
for f in db.sql.gz uploads.tar.gz config.tar.gz; do
  path="$DIR/$f"
  test -s "$path" || { echo "publish-backup-success: missing or empty $path" >&2; exit 1; }
  gzip -t "$path" || { echo "publish-backup-success: corrupt gzip $path" >&2; exit 1; }
done
publish 'db.sql.gz,uploads.tar.gz,config.tar.gz'
echo "BACKUP_SIGNAL_OK $KIND $DIR"
