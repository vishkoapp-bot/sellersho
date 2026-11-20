#!/bin/bash
# backup-entrypoint.sh — BAK-01 consolidated backup entry point.
#
# ONE documented entry point for every backup the platform takes: the
# nightly cron (04:41 Tehran) AND both deploy paths. It replaces the
# ambiguous pair backup-daily.sh / backup-auto.sh (both are now thin
# wrappers that exec this script) and is what the deploy scripts call
# instead of their own inline pg_dump.
#
#   backup-entrypoint.sh run <nightly|deploy> [--commit <sha>] [--include <path> ...] [--dest <dir>]
#   backup-entrypoint.sh verify <backup-dir>
#
# Guarantees (BAK-01 G1/G3/G4/G6/G9/G10):
#   set -euo pipefail throughout — a failing pg_dump (or any step)
#   aborts with a NONZERO exit and NO success marker (G1; the old
#   backup-daily.sh pattern silently wrote BACKUP_OK on failure).
#   umask 077 — every file 0600, every dir 0700.
#   Every backup carries MANIFEST.sha256 (sha256 of every plaintext
#   artifact) + manifest.json (kind, UTC timestamp, commit, pg version,
#   migration count, artifact list with sizes/hashes, missing items)
#   + MANIFEST.enc.sha256 (sha256 of the encrypted files) (G4).
#   Nightly captures DB (-Fc dump AND plain gz dump), uploads,
#   runtime/edge config (.env, all compose files, nginx.conf,
#   Caddyfile, release record) and the Caddy ACME volumes (G6).
#   Redis data is deliberately NOT backed up: holds are rebuildable
#   from PG (redis-recover rebuild), OTPs live 120 s and are not
#   recoverable by design — see docs/operations/redis-recovery.md.
#   Encryption is REQUIRED (G3): every artifact is encrypted with
#   openssl enc AES-256-CBC + PBKDF2 + random salt, using the key file
#   (default /opt/drup/.backup-encryption-key, root-only 0600,
#   generated server-side at deploy time — value never printed or
#   stored by agents, same handling as the Grafana admin password).
#   A missing/unreadable key FAILS LOUDLY (exit 1, no backup).
#   There is NO unencrypted fallback and NO --no-encrypt flag.
#   Why openssl and not age: age is not installed on the host and
#   installing it needs a deploy-time package decision; openssl is
#   already present on the host and on this VM. openssl enc has no
#   authentication, so integrity is provided by the sha256 manifest,
#   verified after decryption (decrypt -> sha256sum -c) at backup time,
#   at deploy-verify time and at restore-verification time.
#   Off-site copy (G2) is CONFIGURABLE via BAK_OFFSITE_DEST
#   (rsync target, e.g. user@host:/path); default OFF — activation
#   and destination choice are owner-blocked, pending the owner.
#   Retention (G10): nightly 7 days local; deploy backups newest 5.
#
# Exit codes:
#   0  backup complete, verified, encrypted, success signal published
#      (and off-site copy verified, if configured)
#   1  backup FAILED (any step before the atomic publish, or the
#      verify subcommand found a bad backup) — no success marker,
#      failure notice attempted (in-app admin inbox + marker file)
#   2  usage error
#   3  backup files are GOOD but the success signal could not be
#      published (publisher missing/failed) or the off-site copy
#      failed — distinct state, NOT a backup failure; a marker file
#      (.signal-failed / .offsite-failed) is left in the backup dir
#      and a failure notice is attempted. The 26 h staleness alert
#      stays as the backstop for this state. Callers: the nightly
#      cron surfaces exit 3 as a failure (it is one — the signal
#      did not publish); the deploy scripts treat it as a loud
#      WARNING and continue, because the backup itself is verified
#      good and aborting a deploy over a monitoring-signal failure
#      would be wrong.
#
# Success is signalled by the backup_signals row (via
# scripts/publish-backup-success.sh) and by the stdout line
# "BACKUP_OK <kind> <dest>" — there is NO marker file; a failed run
# leaves no backup dir behind (work happens in a .tmp dir, published
# by atomic mv only after verification).
#
# Env overrides (defaults are the prod values; the rehearsal and the
# test harness override the DB/docker/key/root paths):
#   BAK_ROOT (default /opt/drup) — host root: DB dump source, uploads,
#     config files, .deployed-commit live here.
#   BAK_BACKUP_BASE (default $BAK_ROOT/backups)
#   BAK_KEY_FILE (default $BAK_ROOT/.backup-encryption-key)
#   BAK_PG_DUMP_CMD (default: docker exec drup_postgres pg_dump -U drup_admin -d drup_db)
#   BAK_PSQL_CMD (default: docker exec drup_postgres psql -U drup_admin -d drup_db -v ON_ERROR_STOP=1 -At)
#   BAK_PG_RESTORE_LIST_CMD (default: docker exec -i drup_postgres pg_restore --list; reads the dump on stdin)
#   BAK_DOCKER_BIN (default docker) — used for Caddy volume inspection
#   BAK_OFFSITE_DEST (default empty = off-site copy DISABLED)
#   BAK_PUBLISHER (default <this script>/../publish-backup-success.sh)
#   BAK_FAILURE_PUBLISHER (default <this script>/publish-backup-failure.sh)
#   BAK_NIGHTLY_RETENTION_DAYS (default 7)
#   BAK_DEPLOY_RETENTION_COUNT (default 5)
#   BAK_CADDY_VOLUMES (default "caddy_data caddy_config")
#
# Never prints secrets. Never sends OTP. No prod contact is made by
# this script itself — it runs ON the host it backs up.
set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BAK_ROOT="${BAK_ROOT:-/opt/drup}"
BAK_BACKUP_BASE="${BAK_BACKUP_BASE:-$BAK_ROOT/backups}"
BAK_KEY_FILE="${BAK_KEY_FILE:-$BAK_ROOT/.backup-encryption-key}"
BAK_PG_DUMP_CMD="${BAK_PG_DUMP_CMD:-docker exec drup_postgres pg_dump -U drup_admin -d drup_db}"
BAK_PSQL_CMD="${BAK_PSQL_CMD:-docker exec drup_postgres psql -U drup_admin -d drup_db -v ON_ERROR_STOP=1 -At}"
BAK_PG_RESTORE_LIST_CMD="${BAK_PG_RESTORE_LIST_CMD:-docker exec -i drup_postgres pg_restore --list}"
BAK_DOCKER_BIN="${BAK_DOCKER_BIN:-docker}"
BAK_OFFSITE_DEST="${BAK_OFFSITE_DEST:-}"
BAK_PUBLISHER="${BAK_PUBLISHER:-$SCRIPT_DIR/../publish-backup-success.sh}"
BAK_FAILURE_PUBLISHER="${BAK_FAILURE_PUBLISHER:-$SCRIPT_DIR/publish-backup-failure.sh}"
BAK_NIGHTLY_RETENTION_DAYS="${BAK_NIGHTLY_RETENTION_DAYS:-7}"
BAK_DEPLOY_RETENTION_COUNT="${BAK_DEPLOY_RETENTION_COUNT:-5}"
BAK_CADDY_VOLUMES="${BAK_CADDY_VOLUMES:-caddy_data caddy_config}"

# Config files captured into config.tar.gz (relative to BAK_ROOT).
# Missing files are warned about and recorded in manifest.json —
# they do not fail the backup (a host without SSL has no
# docker-compose.ssl.yml; .deployed-release.json appears only after
# a REL-01 release deploy). Secrets note: .env IS in this list on
# purpose — a backup without it cannot rebuild the host — and it is
# encrypted like every other artifact before it leaves the work dir.
CONFIG_FILES=".env docker-compose.yml docker-compose.prod.yml docker-compose.ssl.yml docker-compose.observability.yml docker-compose.metrics-ports.yml nginx.conf Caddyfile .deployed-commit .deployed-release.json"

KIND=""
COMMIT=""
DEST_OVERRIDE=""
INCLUDE_FILES=()
SEALED=0
WORK=""
FAIL_REASON=""

log() {
  local line="[$(date -u +%FT%TZ)] backup-entrypoint: $*"
  echo "$line" >&2
  mkdir -p "$BAK_BACKUP_BASE" 2>/dev/null || true
  echo "$line" >> "$BAK_BACKUP_BASE/backup-entrypoint.log" 2>/dev/null || true
}

# Failure path: remove the unpublished work dir, leave a durable
# failure marker + log line, attempt the immediate failure notice.
# Best-effort only — it must never mask the original exit code.
on_failure() {
  local rc="${1:-1}"
  set +e
  [ -n "$WORK" ] && [ -d "$WORK" ] && rm -rf "$WORK"
  local ts
  ts="$(date -u +%Y%m%dT%H%M%SZ)"
  local marker="$BAK_BACKUP_BASE/.backup-failure-${KIND:-unknown}-$ts.marker"
  {
    echo "kind=${KIND:-unknown}"
    echo "timestamp_utc=$ts"
    echo "reason=${FAIL_REASON:-exit $rc}"
    echo "commit=${COMMIT:-unknown}"
  } > "$marker" 2>/dev/null
  chmod 600 "$marker" 2>/dev/null || true
  log "FAILURE kind=${KIND:-unknown} rc=$rc reason=${FAIL_REASON:-unspecified} marker=$marker"
  if [ -x "$BAK_FAILURE_PUBLISHER" ]; then
    "$BAK_FAILURE_PUBLISHER" "${KIND:-unknown}" "${FAIL_REASON:-backup failed (exit $rc)}" >> "$BAK_BACKUP_BASE/backup-entrypoint.log" 2>&1 || true
  else
    log "WARNING: failure publisher missing: $BAK_FAILURE_PUBLISHER — marker file + backup-failures.log are the only record"
  fi
  # No success marker is written on any failure path. Ever.
}

trap 'rc=$?; if [ "$rc" -ne 0 ] && [ "$SEALED" != 1 ]; then on_failure "$rc"; fi' EXIT

fail() {
  FAIL_REASON="$*"
  log "FAIL: $*"
  exit 1
}

require_key() {
  if [ ! -f "$BAK_KEY_FILE" ]; then
    fail "backup encryption key missing: $BAK_KEY_FILE (generate it server-side at deploy time; see docs/operations/backup-runbook.md — no unencrypted backup is ever written)"
  fi
  if [ ! -r "$BAK_KEY_FILE" ]; then
    fail "backup encryption key unreadable: $BAK_KEY_FILE"
  fi
  local mode
  mode="$(stat -c %a "$BAK_KEY_FILE" 2>/dev/null || echo "?")"
  if [ "$mode" != "600" ] && [ "$mode" != "400" ]; then
    log "WARNING: key file $BAK_KEY_FILE has mode $mode (expected 600); refusing to rely on loose permissions silently — tighten it, continuing this run"
  fi
}

detect_commit() {
  if [ -n "$COMMIT" ]; then return; fi
  if [ -f "$BAK_ROOT/.deployed-commit" ]; then
    COMMIT="$(tr -d '[:space:]' < "$BAK_ROOT/.deployed-commit")"
  elif [ -f "$BAK_ROOT/.deployed-release.json" ]; then
    COMMIT="$(python3 -c "import json;print(json.load(open('$BAK_ROOT/.deployed-release.json'))['commit'])" 2>/dev/null || echo unknown)"
  else
    COMMIT="unknown"
  fi
}

# --- manifest helpers -------------------------------------------------
# Artifacts are listed relative to the work dir; MANIFEST.sha256 is
# written inside the work dir so `sha256sum -c` runs from there.
write_manifests() {
  local work="$1"
  (cd "$work" && sha256sum "${ARTIFACTS[@]}" > MANIFEST.sha256)
  local missing_json="[]"
  if [ "${#MISSING_ITEMS[@]}" -gt 0 ]; then
    missing_json="$(python3 -c "import json,sys;print(json.dumps(sys.argv[1:]))" "${MISSING_ITEMS[@]}")"
  fi
  local artifacts_json
  artifacts_json="$(python3 -c "import json,sys;print(json.dumps(sys.argv[1:]))" "${ARTIFACTS[@]}")"
  MANIFEST_WORK="$work" MANIFEST_KIND="$KIND" MANIFEST_CREATED="$CREATED_UTC" \
  MANIFEST_COMMIT="$COMMIT" MANIFEST_PG="$PG_VERSION" MANIFEST_MIG="$MIGRATION_COUNT_JSON" \
  MANIFEST_ARTIFACTS="$artifacts_json" MANIFEST_MISSING="$missing_json" \
  MANIFEST_OFFSITE="$OFFSITE_JSON" \
  python3 - "$work/manifest.json" <<'PYEOF'
import json, os, sys
work = os.environ["MANIFEST_WORK"]
manifest = {
    "backup_kind": os.environ["MANIFEST_KIND"],
    "created_utc": os.environ["MANIFEST_CREATED"],
    "commit": os.environ["MANIFEST_COMMIT"],
    "pg_version": os.environ["MANIFEST_PG"],
    "schema_migrations_applied": json.loads(os.environ["MANIFEST_MIG"]),
    "artifacts": json.loads(os.environ["MANIFEST_ARTIFACTS"]),
    "artifact_sha256": {},
    "artifact_bytes": {},
    "missing": json.loads(os.environ["MANIFEST_MISSING"]),
    "encryption": "openssl-enc-aes-256-cbc-pbkdf2-sha256-salt",
    "offsite_destination": json.loads(os.environ["MANIFEST_OFFSITE"]),
    "entry_point": "scripts/backup/backup-entrypoint.sh",
    "rpo_target": "RPO <= 24h (interim, BAK-01 D3)",
    "rto_target": "RTO <= 4h (interim, BAK-01 D3)",
}
for line in open(os.path.join(work, "MANIFEST.sha256")):
    h, _, name = line.rstrip("\n").partition("  ")
    manifest["artifact_sha256"][name] = h
    manifest["artifact_bytes"][name] = os.path.getsize(os.path.join(work, name))
with open(sys.argv[1], "w") as f:
    json.dump(manifest, f, indent=2, sort_keys=True)
    f.write("\n")
PYEOF
}

encrypt_artifacts() {
  local work="$1"
  for f in "${ARTIFACTS[@]}"; do
    openssl enc -aes-256-cbc -pbkdf2 -md sha256 -salt \
      -pass "file:$BAK_KEY_FILE" -in "$work/$f" -out "$work/$f.enc" \
      || fail "encryption failed for $f"
    rm -f "$work/$f"
  done
  (cd "$work" && sha256sum "${ARTIFACTS[@]/%/.enc}" > MANIFEST.enc.sha256)
}

# Decrypt every *.enc artifact under $1 (recursively — --include
# files may live in subdirectories such as extra/) into $2,
# preserving relative paths.
decrypt_set() {
  local dir="$1" tmp="$2"
  mkdir -p "$tmp"
  while IFS= read -r enc; do
    [ -n "$enc" ] || continue
    local base="${enc%.enc}"
    mkdir -p "$tmp/$(dirname "$base")"
    openssl enc -d -aes-256-cbc -pbkdf2 -md sha256 -salt \
      -pass "file:$BAK_KEY_FILE" -in "$dir/$enc" -out "$tmp/$base" \
      || fail "decryption failed for $dir/$enc (wrong key or corrupt ciphertext)"
  done < <(cd "$dir" && find . -name '*.enc' -type f -printf '%P\n' | sort)
}

# Decrypt the sealed set into $1 and verify it end to end:
# encrypted hashes, plaintext hashes, gzip integrity, pg_restore
# catalogue. This is the check the deploy path (G5) and the nightly
# run both depend on — strictly stronger than pg_restore --list
# alone (the audit proved --list passes 4/6 single-byte data
# corruptions that a real restore rejects; only the manifest
# catches the remaining silent ones).
verify_decrypted_set() {
  local dir="$1" tmp="$2"
  if [ -f "$dir/MANIFEST.enc.sha256" ]; then
    (cd "$dir" && sha256sum -c MANIFEST.enc.sha256) >/dev/null \
      || fail "encrypted manifest verification FAILED for $dir"
    mkdir -p "$tmp"
    cp "$dir/MANIFEST.sha256" "$tmp/MANIFEST.sha256"
    decrypt_set "$dir" "$tmp"
    (cd "$tmp" && sha256sum -c MANIFEST.sha256) >/dev/null \
      || fail "plaintext manifest verification FAILED for $dir (decrypted content does not match the recorded hashes)"
  elif [ -f "$dir/MANIFEST.sha256" ]; then
    log "WARNING: $dir has no encrypted set — verifying the legacy unencrypted layout"
    mkdir -p "$tmp"
    cp "$dir"/* "$tmp/" 2>/dev/null || true
    (cd "$tmp" && sha256sum -c MANIFEST.sha256) >/dev/null \
      || fail "manifest verification FAILED for $dir"
  else
    fail "no manifest in $dir — not a verifiable backup (predates BAK-01 or incomplete)"
  fi
  for gz in "$tmp"/*.gz; do
    [ -f "$gz" ] || continue
    gzip -t "$gz" || fail "gzip integrity check FAILED for $gz"
  done
  if [ -f "$tmp/db.dump" ]; then
    eval "$BAK_PG_RESTORE_LIST_CMD" < "$tmp/db.dump" >/dev/null \
      || fail "pg_restore --list FAILED for $tmp/db.dump"
  else
    fail "db.dump missing from $dir after decryption"
  fi
}

# --- subcommand: verify ----------------------------------------------
cmd_verify() {
  # SEALED=1 first: verify is a read-only check, not a backup run —
  # a failed verification or a usage error must NOT write a
  # backup-failure marker.
  SEALED=1
  local dir="${1:-}"
  [ -n "$dir" ] && [ -d "$dir" ] || { echo "usage: $0 verify <backup-dir>" >&2; exit 2; }
  require_key
  VERIFY_TMP="$(mktemp -d)"
  trap 'rm -rf "$VERIFY_TMP"' EXIT
  verify_decrypted_set "$dir" "$VERIFY_TMP"
  echo "VERIFY_OK $dir (manifest + gzip + pg_restore --list)"
}

# --- subcommand: run --------------------------------------------------
cmd_run() {
  KIND="${1:-}"
  shift || true
  [ "$KIND" = nightly ] || [ "$KIND" = deploy ] || { echo "usage: $0 run <nightly|deploy> [--commit <sha>] [--include <path> ...] [--dest <dir>]" >&2; exit 2; }
  while [ $# -gt 0 ]; do
    case "$1" in
      --commit) COMMIT="$2"; shift 2;;
      --include) INCLUDE_FILES+=("$2"); shift 2;;
      --dest) DEST_OVERRIDE="$2"; shift 2;;
      *) echo "unknown argument: $1" >&2; exit 2;;
    esac
  done
  command -v python3 >/dev/null 2>&1 || fail "python3 is required (manifest writer)"
  command -v openssl >/dev/null 2>&1 || fail "openssl is required (backup encryption)"
  require_key
  detect_commit

  CREATED_UTC="$(date -u +%FT%TZ)"
  local date_day ts
  date_day="$(date +%F)"
  ts="$(date -u +%Y%m%dT%H%M%SZ)"
  local dest
  if [ -n "$DEST_OVERRIDE" ]; then
    dest="$DEST_OVERRIDE"
  elif [ "$KIND" = nightly ]; then
    dest="$BAK_BACKUP_BASE/auto/$date_day"
  else
    dest="$BAK_BACKUP_BASE/deploy-${COMMIT:0:8}-$ts"
  fi
  [ -e "$dest" ] && fail "destination already exists: $dest (refusing to overwrite a previous backup)"
  mkdir -p "$BAK_BACKUP_BASE"
  WORK="$(mktemp -d "$BAK_BACKUP_BASE/.tmp-backup-XXXXXX")"
  log "starting $KIND backup (commit ${COMMIT}) -> $dest"

  ARTIFACTS=()
  MISSING_ITEMS=()
  PG_VERSION="unknown"
  MIGRATION_COUNT_JSON="null"

  # 1) Database: custom-format dump AND plain gz dump. A failure in
  # either pipeline aborts here (pipefail) with no marker.
  log "pg_dump -Fc"
  eval "$BAK_PG_DUMP_CMD -Fc" > "$WORK/db.dump" || fail "pg_dump -Fc failed"
  test -s "$WORK/db.dump" || fail "pg_dump -Fc produced an empty dump"
  log "pg_dump | gzip"
  eval "$BAK_PG_DUMP_CMD" | gzip > "$WORK/db.sql.gz" || fail "pg_dump (plain) failed"
  test -s "$WORK/db.sql.gz" || fail "pg_dump (plain) produced an empty dump"
  ARTIFACTS+=(db.dump db.sql.gz)
  PG_VERSION="$(eval "$BAK_PSQL_CMD -c 'SELECT version()'" 2>/dev/null | head -n 1 || true)"
  [ -z "$PG_VERSION" ] && PG_VERSION="unknown"
  local mig_count
  if mig_count="$(eval "$BAK_PSQL_CMD -c 'SELECT count(*) FROM schema_migrations'" 2>/dev/null | tr -d '[:space:]')"; then
    case "$mig_count" in ''|*[!0-9]*) log "WARNING: unexpected schema_migrations count: $mig_count";; *) MIGRATION_COUNT_JSON="$mig_count";; esac
  else
    log "WARNING: could not read schema_migrations count (recorded as null)"
  fi

  # 2) Uploads.
  [ -d "$BAK_ROOT/static/uploads" ] || fail "uploads dir missing: $BAK_ROOT/static/uploads"
  tar -czf "$WORK/uploads.tar.gz" -C "$BAK_ROOT" static/uploads \
    || fail "uploads tar failed"
  ARTIFACTS+=(uploads.tar.gz)

  # 3) Runtime / edge config (G6). Only files that exist are
  # archived; the absent ones are recorded, not fatal.
  local present=()
  for f in $CONFIG_FILES; do
    if [ -e "$BAK_ROOT/$f" ]; then present+=("$f"); else MISSING_ITEMS+=("config:$f"); fi
  done
  if [ "${#present[@]}" -gt 0 ]; then
    tar -czf "$WORK/config.tar.gz" -C "$BAK_ROOT" "${present[@]}" \
      || fail "config tar failed"
    ARTIFACTS+=(config.tar.gz)
  else
    MISSING_ITEMS+=("config:ALL_MISSING")
    fail "no config files found under $BAK_ROOT (nothing to archive)"
  fi

  # 4) Caddy ACME volumes (G6). Best effort with a recorded miss:
  # on a host without SSL the volumes do not exist; on a host with
  # SSL their absence is a real gap the manifest must show.
  for vol in $BAK_CADDY_VOLUMES; do
    local mp=""
    mp="$("$BAK_DOCKER_BIN" volume inspect -f '{{.Mountpoint}}' "$vol" 2>/dev/null || true)"
    if [ -z "$mp" ] && [ -d "/var/lib/docker/volumes/$vol/_data" ]; then
      mp="/var/lib/docker/volumes/$vol/_data"
    fi
    if [ -n "$mp" ] && [ -d "$mp" ]; then
      tar -czf "$WORK/caddy-$vol.tar.gz" -C "$mp" . \
        || fail "caddy volume tar failed for $vol"
      ARTIFACTS+=("caddy-$vol.tar.gz")
    else
      log "WARNING: Caddy volume $vol not found (no docker volume, no /var/lib/docker mountpoint) — recorded as missing"
      MISSING_ITEMS+=("caddy_volume:$vol")
    fi
  done

  # 5) Caller-supplied extra files (--include). A path under
  # BAK_ROOT keeps its relative path inside the backup (deploy
  # rollback reads backup/<relative>); anything else lands under
  # extra/<basename>. A basename collision fails loudly.
  for src in "${INCLUDE_FILES[@]}"; do
    [ -f "$src" ] || fail "--include file not found: $src"
    local rel=""
    case "$src" in
      "$BAK_ROOT"/*) rel="${src#"$BAK_ROOT"/}";;
      *) rel="extra/$(basename "$src")";;
    esac
    if [ -e "$WORK/$rel" ]; then fail "--include collision: $rel already in the backup set"; fi
    mkdir -p "$WORK/$(dirname "$rel")"
    cp -p "$src" "$WORK/$rel" || fail "--include copy failed: $src"
    ARTIFACTS+=("$rel")
  done

  # 6) Manifests (G4), then encrypt (G3), then verify the sealed
  # set by decrypting it — before anything is published.
  local offsite_json="null"
  [ -n "$BAK_OFFSITE_DEST" ] && offsite_json="$(python3 -c "import json,sys;print(json.dumps(sys.argv[1]))" "$BAK_OFFSITE_DEST")"
  OFFSITE_JSON="$offsite_json"
  write_manifests "$WORK"
  encrypt_artifacts "$WORK"
  local vtmp
  vtmp="$(mktemp -d)"
  verify_decrypted_set "$WORK" "$vtmp"
  rm -rf "$vtmp"
  log "sealed set verified (manifest + decrypt + gzip + pg_restore --list)"

  # 7) Atomic publish.
  mkdir -p "$(dirname "$dest")"
  mv "$WORK" "$dest"
  WORK=""
  SEALED=1
  log "backup published: $dest"

  # 8) Retention (G10).
  if [ "$KIND" = nightly ]; then
    if [ -d "$BAK_BACKUP_BASE/auto" ]; then
      find "$BAK_BACKUP_BASE/auto" -mindepth 1 -maxdepth 1 -type d -mtime +"$BAK_NIGHTLY_RETENTION_DAYS" -exec rm -rf {} + 2>/dev/null || true
    fi
  else
    ls -dt "$BAK_BACKUP_BASE"/deploy-* 2>/dev/null | tail -n +"$((BAK_DEPLOY_RETENTION_COUNT + 1))" | xargs -r rm -rf 2>/dev/null || true
  fi

  # 9) Off-site copy (G2 — configurable, default OFF).
  local exit_code=0
  if [ -n "$BAK_OFFSITE_DEST" ]; then
    if ! offsite_copy "$dest"; then
      exit_code=3
      touch "$dest/.offsite-failed"
      log "WARNING: off-site copy failed (exit 3 — local backup is valid; marker $dest/.offsite-failed)"
      [ -x "$BAK_FAILURE_PUBLISHER" ] && "$BAK_FAILURE_PUBLISHER" "$KIND" "off-site copy failed for $dest (destination $BAK_OFFSITE_DEST)" >> "$BAK_BACKUP_BASE/backup-entrypoint.log" 2>&1 || true
    fi
  else
    log "off-site copy disabled (BAK_OFFSITE_DEST unset — G2 owner-blocked, no destination configured)"
  fi

  # 10) Publish the verified-success signal (G8 primary route).
  # Publisher failure is a DISTINCT state (exit 3): the files on
  # disk are verified good; the staleness alert is the backstop.
  local signal_kind="$KIND"
  if [ -x "$BAK_PUBLISHER" ]; then
    if BAK_KEY_FILE="$BAK_KEY_FILE" BAK_PSQL_CMD="$BAK_PSQL_CMD" "$BAK_PUBLISHER" "$dest" "$signal_kind"; then
      log "success signal published ($signal_kind)"
    else
      exit_code=3
      touch "$dest/.signal-failed"
      log "WARNING: success-signal publish failed (exit 3 — backup files verified good; marker $dest/.signal-failed)"
      [ -x "$BAK_FAILURE_PUBLISHER" ] && "$BAK_FAILURE_PUBLISHER" "$KIND" "backup-success signal publish failed for $dest" >> "$BAK_BACKUP_BASE/backup-entrypoint.log" 2>&1 || true
    fi
  else
    exit_code=3
    touch "$dest/.signal-failed"
    log "WARNING: publisher missing: $BAK_PUBLISHER (exit 3 — backup files verified good; marker $dest/.signal-failed)"
    [ -x "$BAK_FAILURE_PUBLISHER" ] && "$BAK_FAILURE_PUBLISHER" "$KIND" "backup-success publisher missing ($BAK_PUBLISHER) for $dest" >> "$BAK_BACKUP_BASE/backup-entrypoint.log" 2>&1 || true
  fi

  if [ "$exit_code" -eq 0 ]; then
    echo "BACKUP_OK $KIND $dest"
  else
    echo "BACKUP_OK_WITH_WARNINGS $KIND $dest (exit $exit_code — see log)" >&2
  fi
  exit "$exit_code"
}

offsite_copy() {
  local dest="$1"
  local remote_dir="$BAK_OFFSITE_DEST/$(basename "$dest")"
  command -v rsync >/dev/null 2>&1 || { log "off-site copy: rsync not found"; return 1; }
  case "$BAK_OFFSITE_DEST" in
    *:*)
      # Remote target user@host:/path — create the parent first
      # (rsync creates only the last component, not missing parents).
      local host="${BAK_OFFSITE_DEST%%:*}"
      local path="${BAK_OFFSITE_DEST#*:}"
      ssh -o BatchMode=yes -o ConnectTimeout=20 "$host" "mkdir -p '$path'" \
        || { log "off-site copy: cannot create remote parent $path"; return 1; }
      ;;
    *)
      mkdir -p "$BAK_OFFSITE_DEST" || { log "off-site copy: cannot create $BAK_OFFSITE_DEST"; return 1; }
      ;;
  esac
  rsync -a --checksum "$dest/" "$remote_dir/" || { log "off-site copy: rsync failed"; return 1; }
  case "$BAK_OFFSITE_DEST" in
    *:*)
      # Remote target user@host:/path — verify hashes over ssh.
      local host="${BAK_OFFSITE_DEST%%:*}"
      local path="${BAK_OFFSITE_DEST#*:}"
      ssh -o BatchMode=yes -o ConnectTimeout=20 "$host" "cd '$path/$(basename "$dest")' && sha256sum -c MANIFEST.enc.sha256" >/dev/null \
        || { log "off-site copy: remote checksum verification failed"; return 1; }
      ;;
    *)
      (cd "$remote_dir" && sha256sum -c MANIFEST.enc.sha256) >/dev/null \
        || { log "off-site copy: local checksum verification failed"; return 1; }
      ;;
  esac
  log "off-site copy verified: $remote_dir"
  return 0
}

# --- main --------------------------------------------------------------
SUB="${1:-}"
case "$SUB" in
  run) shift; cmd_run "$@";;
  verify) shift; cmd_verify "$@";;
  *) echo "usage: $0 run <nightly|deploy> [--commit <sha>] [--include <path> ...] [--dest <dir>] | $0 verify <backup-dir>" >&2; exit 2;;
esac
