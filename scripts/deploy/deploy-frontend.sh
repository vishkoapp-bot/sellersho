#!/usr/bin/env bash
# REL-01 (G4/G6/G8) — scripted frontend deploy from a staged release.
#
# Canonical home (C6): scripts/deploy/deploy-frontend.sh in the repo
# (single source of truth — the unversioned ~/workspace copy was lost
# in a VM replacement, G4). ~/workspace/deploy-frontend.sh is a
# symlink to this file; the remote path ships the script via scp
# "$0", and scp follows the symlink, so the shipped copy is this
# file's content.
#
# Reconstitutes ~/workspace/deploy-frontend.sh (lost in a VM
# replacement) on the REL-01 canonical path (D2): the release is built
# by scripts/release/make_release.sh from git archive, and this script
# only ever deploys a staged, validated release.
#
#   deploy-frontend.sh <release-dir> [--production] [--skip-health] [--rehearse]
#   deploy-frontend.sh <release-dir> --local-root <dir> [--skip-health] [--rehearse]
#
# Default (no --local-root): the release is shipped to the production
# host (ssh parspack-sellersho, /opt/drup) and this script re-runs
# itself there with --local-root /opt/drup. With --local-root the
# deploy runs against that directory directly — the rehearsal path
# REL-01 validates with (no prod contact, no SSH).
#
# N2 (fail-closed production gating): --production is FORCED unless
# this is an explicit rehearsal. Remote mode always targets /opt/drup
# so it always forces --production; local mode forces --production
# unless --rehearse is passed, and --rehearse is REFUSED outright
# when the target root is /opt/drup. A mock-mode staged release can
# therefore no longer be deployed to prod by forgetting the flag —
# validation with --production rejects it loudly. Rehearsals on a
# fake root must pass --rehearse explicitly.
#
# Protections ported from scripts/deploy_remote.py (G4):
#   file lock, manifest validation, live-mount check, backup
#   (cp -a, newest 5), old hashed-asset retention (G6 — mirrors
#   deploy_remote.py:503-509), health/root checks, scripted rollback.
#
# G8 (D6): NEVER mv/rename /opt/drup/dist (nginx bind-mount lesson —
# moving the dir leaves nginx serving the old inode). The swap is
# extract-over-then-prune-stale: new files are copied OVER the live
# dist first, then files present in live dist but in neither the new
# build nor the retained old assets are deleted last. There is no
# window in which dist does not exist.
#
# Never prints secrets. Never sends OTP. Exit non-zero on any failure.
set -euo pipefail

RELEASE="${1:-}"
shift || true
PRODUCTION=0
SKIP_HEALTH=0
REHEARSE=0
LOCAL_ROOT=""
SSH_HOST="parspack-sellersho"
TARGET="/opt/drup"

while [ $# -gt 0 ]; do
  case "$1" in
    --production) PRODUCTION=1; shift;;
    --skip-health) SKIP_HEALTH=1; shift;;
    --rehearse) REHEARSE=1; shift;;
    --local-root) LOCAL_ROOT="$2"; shift 2;;
    *) echo "unknown argument: $1" >&2; exit 2;;
  esac
done

[ -n "$RELEASE" ] || { echo "usage: deploy-frontend.sh <release-dir> [--production] [--skip-health] [--rehearse] [--local-root <dir>]" >&2; exit 2; }
RELEASE="$(cd "$RELEASE" && pwd)"
[ -f "$RELEASE/release.json" ] && [ -d "$RELEASE/dist" ] || { echo "FAIL: $RELEASE is not a staged release (need release.json + dist/)" >&2; exit 2; }
COMMIT="$(python3 -c "import json;print(json.load(open('$RELEASE/release.json'))['commit'])")"

# --- remote mode: ship self + release, re-run on the host --------------
# N2: remote mode targets /opt/drup — force --production. A mock-mode
# staged release is rejected by validation on the host, loudly.
if [ -z "$LOCAL_ROOT" ]; then
  PRODUCTION=1
  echo "remote mode targets $TARGET: forcing --production (N2)"
  echo "shipping release $COMMIT to $SSH_HOST:$TARGET (remote mode)"
  ssh -o BatchMode=yes -o ConnectTimeout=20 "$SSH_HOST" "mkdir -p '$TARGET/incoming' '$TARGET/releases'"
  tar -czf "/tmp/rel01-frontend-$COMMIT.tar.gz" -C "$(dirname "$RELEASE")" "$(basename "$RELEASE")"
  scp -o BatchMode=yes -o ConnectTimeout=20 "/tmp/rel01-frontend-$COMMIT.tar.gz" "$SSH_HOST:$TARGET/incoming/"
  scp -o BatchMode=yes -o ConnectTimeout=20 "$0" "$SSH_HOST:$TARGET/incoming/deploy-frontend.sh"
  REMOTE_ARGS="--local-root $TARGET"
  [ "$PRODUCTION" -eq 1 ] && REMOTE_ARGS="$REMOTE_ARGS --production"
  [ "$SKIP_HEALTH" -eq 1 ] && REMOTE_ARGS="$REMOTE_ARGS --skip-health"
  ssh -o BatchMode=yes -o ConnectTimeout=20 "$SSH_HOST" bash -se <<EOF
set -eu
mkdir -p '$TARGET/releases/$COMMIT'
tar -xzf '$TARGET/incoming/rel01-frontend-$COMMIT.tar.gz' -C '$TARGET/releases/$COMMIT' --strip-components=1
bash '$TARGET/incoming/deploy-frontend.sh' '$TARGET/releases/$COMMIT' $REMOTE_ARGS
EOF
  echo "FRONTEND DEPLOYED: $COMMIT"
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
echo "deploying frontend $COMMIT into $ROOT"
[ -d "$ROOT/dist" ] || { echo "FAIL: live dist missing: $ROOT/dist (unexpected mount; refusing)" >&2; exit 1; }

exec 9>"$ROOT/.deploy-frontend.lock"
if ! flock -n 9; then echo "FAIL: another frontend deploy holds the lock." >&2; exit 1; fi

VALIDATE_ARGS=(--stage "$RELEASE" --commit "$COMMIT")
[ "$PRODUCTION" -eq 1 ] && VALIDATE_ARGS+=(--production)
python3 "$RELEASE/scripts/release/validate_release.py" "${VALIDATE_ARGS[@]}"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP="$ROOT/backups/frontend-$COMMIT-$TS"
mkdir -p "$ROOT/backups" "$ROOT/releases/$COMMIT"
cp -rp --no-preserve=ownership "$ROOT/dist" "$BACKUP-dist"
cp "$RELEASE/release.json" "$BACKUP-release.json"
echo "backup: $BACKUP-dist"

# Stage the release record (D3: copy deployed alongside the release).
# In remote mode RELEASE is already $ROOT/releases/$COMMIT, so source
# and destination are the same file — skip the copy then (GNU cp
# refuses same-file copies and set -e would abort the deploy).
if [ ! "$RELEASE/release.json" -ef "$ROOT/releases/$COMMIT/release.json" ]; then
  cp "$RELEASE/release.json" "$ROOT/releases/$COMMIT/release.json"
fi
cp "$RELEASE/release.json" "$ROOT/.deployed-release.json"

# G6: retain old hashed assets for already-open client pages.
WORK="$(mktemp -d "$ROOT/.frontend-new-XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
cp -rp --no-preserve=ownership "$RELEASE/dist/." "$WORK/"
if [ -d "$ROOT/dist/assets" ]; then
  mkdir -p "$WORK/assets"
  for item in "$ROOT/dist/assets/"*; do
    [ -f "$item" ] || continue
    base="$(basename "$item")"
    if [ ! -e "$WORK/assets/$base" ]; then cp -rp --no-preserve=ownership "$item" "$WORK/assets/$base"; fi
  done
fi

rollback() {
  echo "ROLLBACK: restoring $BACKUP-dist" >&2
  # The deployed record is written before the swap/health check, so a
  # failed deploy must not leave a file claiming the release is
  # deployed (attempt 3 left /opt/drup/.deployed-release.json claiming
  # 755011b4 after rollback). Remove it on every rollback path.
  rm -f "$ROOT/.deployed-release.json"
  cp -rp --no-preserve=ownership "$BACKUP-dist/." "$ROOT/dist/"
  (cd "$BACKUP-dist" && find . -type f | sort > /tmp/rel01-rollback-keep.txt)
  (cd "$ROOT/dist" && find . -type f | sort > /tmp/rel01-rollback-live.txt)
  comm -23 /tmp/rel01-rollback-live.txt /tmp/rel01-rollback-keep.txt | while read -r f; do rm -f "$ROOT/dist/$f"; done
  rm -f /tmp/rel01-rollback-keep.txt /tmp/rel01-rollback-live.txt
  echo "rollback done" >&2
}

# G8: extract OVER the live dist first (no dir removal, no rename),
# then delete stale files last.
if ! cp -rp --no-preserve=ownership "$WORK/." "$ROOT/dist/"; then rollback; exit 1; fi
(cd "$WORK" && find . -type f | sort > /tmp/rel01-new-files.txt)
(cd "$ROOT/dist" && find . -type f | sort > /tmp/rel01-live-files.txt)
STALE="$(comm -23 /tmp/rel01-live-files.txt /tmp/rel01-new-files.txt || true)"
if [ -n "$STALE" ]; then
  echo "$STALE" | while read -r f; do rm -f "$ROOT/dist/$f"; done
  echo "pruned $(echo "$STALE" | wc -l) stale file(s) from live dist"
fi
rm -f /tmp/rel01-new-files.txt /tmp/rel01-live-files.txt

# Verify the new bundle is served: index.html exists and its hashed
# entrypoint file is present in the live dist.
[ -f "$ROOT/dist/index.html" ] || { echo "FAIL: live dist/index.html missing after swap" >&2; rollback; exit 1; }
ENTRY="$(grep -o 'assets/index-[A-Za-z0-9_-]*\.js' "$ROOT/dist/index.html" | head -1 || true)"
if [ -n "$ENTRY" ] && [ ! -f "$ROOT/dist/$ENTRY" ]; then
  echo "FAIL: index.html references missing bundle $ENTRY" >&2; rollback; exit 1
fi
echo "live bundle entrypoint: ${ENTRY:-unknown}"

# Health / root checks (skipped only in rehearsal, loudly).
if [ "$SKIP_HEALTH" -eq 1 ]; then
  echo "WARNING: --skip-health: no health/root check performed (rehearsal only)." >&2
else
  if ! curl -sf --max-time 10 "http://127.0.0.1:8080/api/ready" >/dev/null; then
    echo "FAIL: /api/ready did not answer after frontend swap" >&2; rollback; exit 1
  fi
  # Site-root check: the SPA is served by the frontend container's
  # nginx on its internal port 80 (root /usr/share/nginx/html, the
  # /opt/drup/dist bind mount). Port 80 is NOT published on the host,
  # so reach it via docker exec. Do NOT point this at 127.0.0.1:8080
  # (backend — 404s / by design) or 127.0.0.1:8081 (frontend
  # stub_status server — location / returns 404 by design; only
  # /status answers 200). Bounded retry: a single attempt is brittle
  # even on the right target.
  SITE_ROOT_OK=0
  for attempt in 1 2 3; do
    if docker exec drup_frontend curl -sf --max-time 10 "http://127.0.0.1:80/" -o /dev/null; then
      SITE_ROOT_OK=1
      break
    fi
    [ "$attempt" -lt 3 ] && sleep 2
  done
  if [ "$SITE_ROOT_OK" -eq 0 ]; then
    echo "FAIL: site root (frontend container http://127.0.0.1:80/) did not answer after frontend swap" >&2; rollback; exit 1
  fi
  echo "health: /api/ready + site root OK"
fi

# Retention: newest 5 frontend backups + newest 5 staged releases (D3).
ls -dt "$ROOT/backups"/frontend-* 2>/dev/null | tail -n +6 | xargs -r rm -rf
ls -dt "$ROOT/releases"/*/ 2>/dev/null | tail -n +6 | xargs -r rm -rf

echo "FRONTEND DEPLOYED: $COMMIT into $ROOT (backup $BACKUP-dist)"
