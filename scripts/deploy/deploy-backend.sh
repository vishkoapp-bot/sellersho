#!/usr/bin/env bash
# REL-01 (G4) — scripted backend swap from a staged release.
#
# Canonical home (C6): scripts/deploy/deploy-backend.sh in the repo
# (single source of truth — the unversioned ~/workspace copy was lost
# in a VM replacement, G4). ~/workspace/deploy-backend.sh is a
# symlink to this file; the remote path ships the script via scp
# "$0", and scp follows the symlink, so the shipped copy is this
# file's content.
#
#   deploy-backend.sh <release-dir> [--production] [--skip-health] [--db-url <url>]
#   deploy-backend.sh <release-dir> --local-root <dir> [--skip-health] [--rehearse] [--db-url <url>]
#
# Default (no --local-root): the release is shipped to the production
# host (ssh parspack-sellersho, /opt/drup) and this script re-runs
# itself there with --local-root /opt/drup. With --local-root the swap
# runs against that directory directly — the rehearsal path REL-01
# validates with (no prod contact, no SSH, no Docker: migration and
# pg_dump steps are skipped loudly in rehearsal).
#
# N2 (fail-closed production gating): --production is FORCED unless
# this is an explicit rehearsal. Remote mode always targets /opt/drup
# so it always forces --production; local mode forces --production
# unless --rehearse is passed, and --rehearse is REFUSED outright
# when the target root is /opt/drup. A mock-mode staged release can
# therefore no longer be deployed to prod by forgetting the flag.
#
# N5 (pre-swap migration classification): pass --db-url <prod-db-url>
# and this script runs validate --db-url <url> --db-required BEFORE
# the swap (read-only pending enumeration with the classify_history
# rules; unreviewed historical pending fails the deploy). WITHOUT
# --db-url NO classification runs at deploy time — the script says so
# loudly instead of implying otherwise (the old comment claimed it
# ran "inside validate --db-url on the operator side" while neither
# deploy script ever passed --db-url). The prod URL is supplied by
# the operator at deploy time from the host's live config and is
# never committed; automating its supply is MIG-02-owned (see
# scripts/release/MIGRATION_INTERFACE.md).
#
# Protections ported from scripts/deploy_remote.py (G4):
#   file lock, manifest validation, live-mount check, BAK-01
#   entry-point backup (manifest + gzip + pg_restore --list
#   verification, G5 — replaces the old inline pg_dump + --list-only
#   check, which the BAK-01 audit proved insufficient),
#   migration classification + staged migrate-linux execution
#   inside the backend container, checksum-adoption check, binary
#   swap via server-linux.new-<commit> + atomic mv (ETXTBSY-safe:
#   never scp over the running binary), health-gated switch on
#   /api/ready, scripted rollback.
#
# After the swap the frontend container is restarted too (nginx caches
# the backend's old container IP at startup). Both compose files are
# always used. The smoke suite is NOT re-run here: it runs in staging
# (smoke_release.py) before the release is shipped; this script gates
# on /api/ready + /api/health after the swap.
#
# Never prints secrets. Never sends OTP. Exit non-zero on any failure.
set -euo pipefail
umask 077  # BAK-01 G11: deploy backups hold secrets; files 0600.

RELEASE="${1:-}"
shift || true
PRODUCTION=0
SKIP_HEALTH=0
LOCAL_ROOT=""
REHEARSE=0
DB_URL=""
SSH_HOST="parspack-sellersho"
TARGET="/opt/drup"

while [ $# -gt 0 ]; do
  case "$1" in
    --production) PRODUCTION=1; shift;;
    --skip-health) SKIP_HEALTH=1; shift;;
    --local-root) LOCAL_ROOT="$2"; shift 2;;
    --rehearse) REHEARSE=1; shift;;
    --db-url) DB_URL="$2"; shift 2;;
    *) echo "unknown argument: $1" >&2; exit 2;;
  esac
done

[ -n "$RELEASE" ] || { echo "usage: deploy-backend.sh <release-dir> [--production] [--skip-health] [--db-url <url>] [--local-root <dir> [--rehearse]]" >&2; exit 2; }
RELEASE="$(cd "$RELEASE" && pwd)"
[ -f "$RELEASE/release.json" ] && [ -f "$RELEASE/backend/bin/server-linux" ] && [ -f "$RELEASE/backend/bin/migrate-linux" ] \
  || { echo "FAIL: $RELEASE is not a staged release (need release.json + backend/bin/*-linux)" >&2; exit 2; }
COMMIT="$(python3 -c "import json;print(json.load(open('$RELEASE/release.json'))['commit'])")"

# --- remote mode: ship self + release, re-run on the host --------------
# N2: remote mode targets /opt/drup — force --production. A mock-mode
# staged release is rejected by validation on the host, loudly.
# --db-url is NOT forwarded: the URL is host-local config, supplied
# by the operator on the host side (N5); forwarding it over scp/ssh
# args would put a credential in the process list.
if [ -z "$LOCAL_ROOT" ]; then
  PRODUCTION=1
  echo "remote mode targets $TARGET: forcing --production (N2)"
  echo "shipping release $COMMIT to $SSH_HOST:$TARGET (remote mode)"
  ssh -o BatchMode=yes -o ConnectTimeout=20 "$SSH_HOST" "mkdir -p '$TARGET/incoming' '$TARGET/releases'"
  tar -czf "/tmp/rel01-backend-$COMMIT.tar.gz" -C "$(dirname "$RELEASE")" "$(basename "$RELEASE")"
  scp -o BatchMode=yes -o ConnectTimeout=20 "/tmp/rel01-backend-$COMMIT.tar.gz" "$SSH_HOST:$TARGET/incoming/"
  scp -o BatchMode=yes -o ConnectTimeout=20 "$0" "$SSH_HOST:$TARGET/incoming/deploy-backend.sh"
  REMOTE_ARGS="--local-root $TARGET"
  [ "$PRODUCTION" -eq 1 ] && REMOTE_ARGS="$REMOTE_ARGS --production"
  [ "$SKIP_HEALTH" -eq 1 ] && REMOTE_ARGS="$REMOTE_ARGS --skip-health"
  ssh -o BatchMode=yes -o ConnectTimeout=20 "$SSH_HOST" bash -se <<EOF
set -eu
mkdir -p '$TARGET/releases/$COMMIT'
tar -xzf '$TARGET/incoming/rel01-backend-$COMMIT.tar.gz' -C '$TARGET/releases/$COMMIT' --strip-components=1
bash '$TARGET/incoming/deploy-backend.sh' '$TARGET/releases/$COMMIT' $REMOTE_ARGS
EOF
  echo "BACKEND DEPLOYED: $COMMIT"
  exit 0
fi

# --- local mode (runs on the target host, or a rehearsal root) ---------
ROOT="$LOCAL_ROOT"
# N2: force --production unless this is an explicit rehearsal on a
# non-production root. --rehearse is refused outright for /opt/drup.
if [ "$ROOT" = "/opt/drup" ]; then
  if [ "$REHEARSE" -eq 1 ]; then echo "FAIL: --rehearse refused for target root /opt/drup (N2)" >&2; exit 1; fi
  PRODUCTION=1
  echo "target root is /opt/drup: forcing --production (N2)"
elif [ "$REHEARSE" -eq 0 ]; then
  PRODUCTION=1
  echo "forcing --production: pass --rehearse explicitly for a non-production rehearsal root (N2)"
else
  echo "rehearsal mode (--rehearse): production gating NOT forced (N2)"
fi
echo "deploying backend $COMMIT into $ROOT"
[ -f "$ROOT/backend/bin/server-linux" ] || { echo "FAIL: live backend binary missing: $ROOT/backend/bin/server-linux (unexpected mount; refusing)" >&2; exit 1; }

exec 9>"$ROOT/.deploy-backend.lock"
if ! flock -n 9; then echo "FAIL: another backend deploy holds the lock." >&2; exit 1; fi

VALIDATE_ARGS=(--stage "$RELEASE" --commit "$COMMIT")
[ "$PRODUCTION" -eq 1 ] && VALIDATE_ARGS+=(--production)
python3 "$RELEASE/scripts/release/validate_release.py" "${VALIDATE_ARGS[@]}"

# N5: pre-swap migration classification (read-only). Runs ONLY when
# --db-url is supplied: validate enumerates pending migrations
# against that database with the classify_history rules and fails
# (--db-required) on unreviewed historical pending. WITHOUT --db-url
# no classification runs at deploy time — the in-container migrate
# below still applies pending migrations, but the unreviewed-
# historical check is skipped; that gap is stated here, loudly,
# instead of implied away. (The old comment claimed classification
# ran "inside validate --db-url on the operator side" while neither
# deploy script ever passed --db-url — C5.)
if [ -n "$DB_URL" ]; then
  echo "pre-swap migration classification against supplied --db-url (N5; URL not printed)"
  DB_VALIDATE_ARGS=(--stage "$RELEASE" --commit "$COMMIT")
  [ "$PRODUCTION" -eq 1 ] && DB_VALIDATE_ARGS+=(--production)
  python3 "$RELEASE/scripts/release/validate_release.py" "${DB_VALIDATE_ARGS[@]}" \
    --db-url "$DB_URL" --db-required
else
  echo "WARNING: no --db-url supplied: pre-swap migration classification SKIPPED (N5) — pending migrations will be applied in-container without the unreviewed-historical check. Supply --db-url at deploy time from the host's live config." >&2
fi

HAVE_DOCKER=0
command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && HAVE_DOCKER=1
if [ "$REHEARSE" -eq 1 ]; then HAVE_DOCKER=0; fi

TS="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP="$ROOT/backups/backend-$COMMIT-$TS"
mkdir -p "$ROOT/backups" "$ROOT/releases/$COMMIT"
cp -rp --no-preserve=ownership "$ROOT/backend/bin/server-linux" "$BACKUP-server-linux"
cp "$RELEASE/release.json" "$BACKUP-release.json"
echo "backup: $BACKUP-server-linux"

# Live-mount check (deploy_remote.py): /app/server must be the live binary.
if [ "$HAVE_DOCKER" -eq 1 ]; then
  MOUNT="$(docker inspect drup_backend --format '{{range .Mounts}}{{if eq .Destination "/app/server"}}{{.Source}}{{end}}{{end}}')"
  if [ "$MOUNT" != "$ROOT/backend/bin/server-linux" ]; then
    echo "FAIL: unexpected production mount for /app/server: $MOUNT" >&2; exit 1
  fi
  echo "live-mount check OK: /app/server -> $MOUNT"
fi

# BAK-01 entry-point backup (deploy kind) + verification (G5),
# prod path only. The entry point writes the encrypted, manifested
# backup set (DB -Fc + plain dumps, uploads, config, Caddy volumes)
# under $ROOT/backups/deploy-<commit>-<ts>/, verifies it (manifest
# + decrypt + gzip + pg_restore --list), prunes deploy backups to
# the newest 5 (G10), and publishes the verified-success signal.
# It FAILS LOUDLY (nonzero, no success marker, immediate failure
# notice) when pg_dump fails, when the encryption key is missing,
# or when verification fails — the old inline pg_dump + --list-only
# check could pass a corrupt backup (audit §4.3) and is gone.
# The $BACKUP-server-linux / $BACKUP-release.json copies below the
# entry-point call stay OUTSIDE the backup dir: rollback() reads
# them directly, and the entry point also folds copies into the
# backup set via --include.
if [ "$HAVE_DOCKER" -eq 1 ]; then
  ENTRYPOINT=""
  [ -x "$RELEASE/scripts/backup/backup-entrypoint.sh" ] && ENTRYPOINT="$RELEASE/scripts/backup/backup-entrypoint.sh"
  [ -z "$ENTRYPOINT" ] && [ -x "$ROOT/scripts/backup/backup-entrypoint.sh" ] && ENTRYPOINT="$ROOT/scripts/backup/backup-entrypoint.sh"
  [ -n "$ENTRYPOINT" ] || { echo "FAIL: backup entry point not found in the staged release or at $ROOT/scripts/backup/backup-entrypoint.sh (BAK-01: the deploy coordinator installs it; refusing to deploy without a verified backup)" >&2; exit 1; }
  # Exit 3 (backup files verified good, success signal / off-site
  # copy failed) is a loud WARNING, not an abort — see the entry
  # point's exit-code contract. Any other nonzero exit aborts the
  # deploy before any change.
  set +e
  BAK_ROOT="$ROOT" "$ENTRYPOINT" run deploy --commit "$COMMIT" --include "$BACKUP-server-linux" --include "$BACKUP-release.json"
  ENTRY_RC=$?
  set -e
  if [ "$ENTRY_RC" -eq 3 ]; then
    echo "WARNING: entry-point backup files verified good but the success signal / off-site copy failed (exit 3; marker left in the backup dir; continuing the deploy)" >&2
  elif [ "$ENTRY_RC" -ne 0 ]; then
    echo "FAIL: entry-point backup failed (exit $ENTRY_RC); deploy aborted before any change" >&2
    exit "$ENTRY_RC"
  fi
  echo "entry-point backup OK (deploy $COMMIT)"
else
  echo "WARNING: no Docker here (--rehearse or no daemon): entry-point backup SKIPPED — a real deploy must run on the host with Docker." >&2
fi

# Migration classification + execution inside the backend container
# (deploy_remote.py:448-458). In rehearsal there is no database, so
# the step is skipped loudly and nothing is migrated.
if [ "$HAVE_DOCKER" -eq 1 ]; then
  MIG_STAGE="/tmp/sellersho-migrate-$COMMIT"
  docker exec drup_backend mkdir -p "$MIG_STAGE/migrations"
  docker cp "$RELEASE/backend/bin/migrate-linux" "drup_backend:$MIG_STAGE/migrate"
  docker cp "$RELEASE/backend/migrations/." "drup_backend:$MIG_STAGE/migrations"
  # Migration classification (N5) already ran pre-swap, IF --db-url
  # was supplied (see above); the container run below is the
  # execution, not the classification.
  docker exec drup_backend "$MIG_STAGE/migrate" -dir "$MIG_STAGE/migrations" -adopt-legacy
  docker exec drup_backend "$MIG_STAGE/migrate" -dir "$MIG_STAGE/migrations"
  NULL_CHECKSUMS="$(docker exec drup_postgres psql -U drup_admin -d drup_db -Atc "SELECT count(*) FROM schema_migrations WHERE checksum IS NULL OR checksum=''" | tr -d '[:space:]')"
  if [ "$NULL_CHECKSUMS" != "0" ]; then
    echo "FAIL: $NULL_CHECKSUMS schema_migrations rows without a checksum after migration" >&2
    docker exec drup_backend rm -rf "$MIG_STAGE" || true
    exit 1
  fi
  docker exec drup_backend rm -rf "$MIG_STAGE"
  echo "migrations applied + checksum-adoption check OK"
else
  echo "WARNING: no Docker here: migration execution SKIPPED (rehearsal only — a real deploy classifies + applies pending migrations in-container)." >&2
fi

# Binary swap: upload as .new-<commit>, atomic mv into place
# (ETXTBSY-safe), keep a dated .bak of the previous binary.
cp -rp --no-preserve=ownership "$RELEASE/backend/bin/server-linux" "$ROOT/backend/bin/server-linux.new-$COMMIT"
chmod 755 "$ROOT/backend/bin/server-linux.new-$COMMIT"
cp -rp --no-preserve=ownership "$ROOT/backend/bin/server-linux" "$ROOT/backend/bin/server-linux.bak-$TS"
mv "$ROOT/backend/bin/server-linux.new-$COMMIT" "$ROOT/backend/bin/server-linux"
echo "binary swapped: $COMMIT (previous kept as server-linux.bak-$TS)"

# Copy new migration files into the live tree (never overwrite existing).
for f in "$RELEASE/backend/migrations/"*.up.sql "$RELEASE/backend/migrations/"*.down.sql; do
  [ -f "$f" ] || continue
  base="$(basename "$f")"
  [ -e "$ROOT/backend/migrations/$base" ] || cp "$f" "$ROOT/backend/migrations/$base"
done

# Stage the release record (D3).
cp "$RELEASE/release.json" "$ROOT/releases/$COMMIT/release.json"
cp "$RELEASE/release.json" "$ROOT/.deployed-release.json"

rollback() {
  echo "ROLLBACK: restoring $BACKUP-server-linux" >&2
  cp -rp --no-preserve=ownership "$BACKUP-server-linux" "$ROOT/backend/bin/server-linux.new-rollback"
  mv "$ROOT/backend/bin/server-linux.new-rollback" "$ROOT/backend/bin/server-linux"
  if [ "$HAVE_DOCKER" -eq 1 ]; then
    (cd "$ROOT" && docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --force-recreate backend) || true
    (cd "$ROOT" && docker compose -f docker-compose.yml -f docker-compose.prod.yml restart frontend) || true
  fi
  echo "rollback done (binary restored; DB intentionally NOT restored over new orders)" >&2
}

# Recreate backend + restart frontend (nginx caches the old backend IP).
if [ "$HAVE_DOCKER" -eq 1 ]; then
  (cd "$ROOT" && docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --force-recreate backend)
  (cd "$ROOT" && docker compose -f docker-compose.yml -f docker-compose.prod.yml restart frontend)
  NEW_SHA="$(docker exec drup_backend sha256sum /app/server | cut -d' ' -f1)"
  WANT_SHA="$(sha256sum "$ROOT/backend/bin/server-linux" | cut -d' ' -f1)"
  if [ "$NEW_SHA" != "$WANT_SHA" ]; then
    echo "FAIL: running container serves a different binary ($NEW_SHA != $WANT_SHA)" >&2
    rollback; exit 1
  fi
  echo "container binary verified: $NEW_SHA"
else
  echo "WARNING: no Docker here: container recreate SKIPPED (rehearsal only)." >&2
fi

# Health-gated switch: /api/ready (+ /api/health) must answer.
if [ "$SKIP_HEALTH" -eq 1 ]; then
  echo "WARNING: --skip-health: no readiness gate performed (rehearsal only)." >&2
else
  READY=0
  for _ in $(seq 1 45); do
    if curl -sf --max-time 3 "http://127.0.0.1:8080/api/ready" >/dev/null 2>&1; then READY=1; break; fi
    sleep 2
  done
  if [ "$READY" -ne 1 ]; then
    echo "FAIL: /api/ready did not answer after backend swap" >&2; rollback; exit 1
  fi
  curl -sf --max-time 10 "http://127.0.0.1:8080/api/health" >/dev/null || { echo "FAIL: /api/health failed after swap" >&2; rollback; exit 1; }
  echo "health: /api/ready + /api/health OK"
fi

# Retention: newest 5 backend backups + newest 5 staged releases (D3).
ls -dt "$ROOT/backups"/backend-* 2>/dev/null | tail -n +6 | xargs -r rm -rf
ls -dt "$ROOT/releases"/*/ 2>/dev/null | tail -n +6 | xargs -r rm -rf

echo "BACKEND DEPLOYED: $COMMIT into $ROOT (backup $BACKUP-server-linux)"
