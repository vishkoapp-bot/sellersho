#!/bin/bash
# restore-verify.sh — BAK-01 G13 periodic restore verification job.
#
# Usage: scripts/backup/restore-verify.sh <backup-dir> [--keep-db] [--report <file>] [--target-db <name>]
#
# Restores a recent (encrypted) backup into a FRESH ISOLATED database
# — never prod, never the backup host's live DB — and validates it:
# schema/migration count vs the manifest, orders + statuses,
# order items, ledger rows/sums, inventory quantities, payment
# receipts, store domains, media references (DB image_url values vs
# files in uploads.tar.gz), and config presence (nginx.conf,
# Caddyfile). Measures the recovery time (restore start -> validation
# done) and the recoverable-data age (now - backup created_utc) and
# scores both against the INTERIM approved targets (D3):
# RPO <= 24 h, RTO <= 4 h. Writes a JSON report and prints a human
# summary. Exit 0 only when every check passes AND both targets hold.
#
# This job is the periodic verification (G13) — it is NOT a
# per-deploy check (too heavy): deploys use `backup-entrypoint.sh
# verify` (manifest + gzip + pg_restore --list, G5).
#
# PROPOSED schedule (NOT created — needs owner approval naming the
# schedule, D4): weekly, Sunday 05:30 Tehran, on an isolated host
# (never prod), plus after every deploy-backup change and after any
# restore-affecting change (PG upgrade, new migration type, new
# backup script version). The quarterly bare-host drill (fresh host,
# key fetched from the OFFLINE holder copy, no local state) is a
# manual runbook procedure — see docs/operations/backup-runbook.md.
#
# Env (defaults are the prod/operator values; the rehearsal and the
# test harness override them):
#   BAK_KEY_FILE (default /opt/drup/.backup-encryption-key)
#   BAK_ENTRYPOINT (default <this script>/backup-entrypoint.sh)
#   BAK_BACKUP_BASE (default /opt/drup/backups) — report dir default
#   BAK_ADMIN_PSQL — psql against the ISOLATED host's maintenance DB,
#     e.g. "psql -h 127.0.0.1 -p 5433 -U gap13 -d postgres -v
#     ON_ERROR_STOP=1 -At" (must be able to CREATE/DROP DATABASE).
#     No default that can touch prod: without it the job refuses.
#   BAK_PG_RESTORE_CMD — pg_restore for the isolated host, e.g.
#     "pg_restore -h 127.0.0.1 -p 5433 -U gap13". No default.
#   BAK_PUBLISH_RESTORE_SIGNAL (default 0) — if 1, upsert a
#     backup_signals row kind='restore_verification' into the PROD
#     db via BAK_PSQL_CMD (the poller only reads kind='nightly', so
#     this is a durable record, not a gauge input).
#   BAK_PSQL_CMD — only used for the optional restore signal above.
#
# Never prints secrets. Never sends OTP. All work is isolated; the
# disposable database is dropped afterwards unless --keep-db.
set -euo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BAK_KEY_FILE="${BAK_KEY_FILE:-/opt/drup/.backup-encryption-key}"
BAK_ENTRYPOINT="${BAK_ENTRYPOINT:-$SCRIPT_DIR/backup-entrypoint.sh}"
BAK_BACKUP_BASE="${BAK_BACKUP_BASE:-/opt/drup/backups}"

BACKUP_DIR="${1:-}"
shift || true
[ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || { echo "usage: $0 <backup-dir> [--keep-db] [--report <file>] [--target-db <name>]" >&2; exit 2; }
KEEP_DB=0
REPORT=""
TARGET_DB=""
while [ $# -gt 0 ]; do
  case "$1" in
    --keep-db) KEEP_DB=1; shift;;
    --report) REPORT="$2"; shift 2;;
    --target-db) TARGET_DB="$2"; shift 2;;
    *) echo "unknown argument: $1" >&2; exit 2;;
  esac
done

[ -n "${BAK_ADMIN_PSQL:-}" ] || { echo "FAIL: BAK_ADMIN_PSQL is required (psql against the ISOLATED host's maintenance DB — refusing to guess a target)" >&2; exit 2; }
[ -n "${BAK_PG_RESTORE_CMD:-}" ] || { echo "FAIL: BAK_PG_RESTORE_CMD is required (pg_restore for the isolated host)" >&2; exit 2; }
[ -f "$BAK_KEY_FILE" ] || { echo "FAIL: encryption key missing: $BAK_KEY_FILE" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "FAIL: python3 is required (report writer)" >&2; exit 1; }

TS="$(date -u +%Y%m%dT%H%M%SZ)"
# Disposable DB name: digits only. An unquoted CREATE DATABASE folds
# mixed-case identifiers to lowercase, but pg_restore -d does NOT
# fold its argument — a name with T/Z would be created lowercase
# and then not found at connect time.
TARGET_DB="${TARGET_DB:-bakverify_$(date -u +%Y%m%d%H%M%S)}"
REPORT="${REPORT:-$BAK_BACKUP_BASE/restore-verification/restore-verification-$TS.json}"
WORK="$(mktemp -d)"
# Backstop cleanup: the disposable DB is dropped on every exit
# (success or failure) unless --keep-db; the explicit drop in step
# 8 is the normal path, this trap covers the failure exits.
cleanup_target() {
  if [ "$KEEP_DB" -eq 0 ] && [ -n "${TARGET_DB:-}" ] && [ -n "${BAK_ADMIN_PSQL:-}" ]; then
    eval "$BAK_ADMIN_PSQL -c \"DROP DATABASE IF EXISTS $TARGET_DB\"" >/dev/null 2>&1 || true
  fi
  [ -n "${WORK:-}" ] && [ -d "$WORK" ] && rm -rf "$WORK"
}
trap cleanup_target EXIT

# Target psql: the admin command with the maintenance DB swapped
# for the disposable target DB.
if [[ "$BAK_ADMIN_PSQL" == *"-d postgres"* ]]; then
  TARGET_PSQL="${BAK_ADMIN_PSQL/-d postgres/-d $TARGET_DB}"
else
  TARGET_PSQL="$BAK_ADMIN_PSQL -d $TARGET_DB"
fi

JOB_START_S=$(date +%s)
FAILURES=()
note_fail() { FAILURES+=("$*"); echo "CHECK_FAIL: $*" >&2; }
note_ok() { echo "CHECK_OK: $*"; }

# 1) Fast gate: manifest + decrypt + gzip + pg_restore --list.
"$BAK_ENTRYPOINT" verify "$BACKUP_DIR" || { echo "FAIL: entry-point verification of $BACKUP_DIR failed" >&2; exit 1; }

# 2) Decrypt the set into the scratch dir (same shape as verify —
# recursive: --include files may live in subdirectories such as
# extra/; a top-level *.enc glob would silently skip them).
cp "$BACKUP_DIR/MANIFEST.sha256" "$WORK/MANIFEST.sha256"
while IFS= read -r enc; do
  [ -n "$enc" ] || continue
  base="${enc%.enc}"
  mkdir -p "$WORK/$(dirname "$base")"
  openssl enc -d -aes-256-cbc -pbkdf2 -md sha256 -salt \
    -pass "file:$BAK_KEY_FILE" -in "$BACKUP_DIR/$enc" -out "$WORK/$base" \
    || { echo "FAIL: decryption failed for $BACKUP_DIR/$enc" >&2; exit 1; }
done < <(cd "$BACKUP_DIR" && find . -name '*.enc' -type f -printf '%P\n' | sort)
(cd "$WORK" && sha256sum -c MANIFEST.sha256) >/dev/null || { echo "FAIL: plaintext manifest check failed" >&2; exit 1; }

MANIFEST_CREATED="$(python3 -c "import json;print(json.load(open('$BACKUP_DIR/manifest.json'))['created_utc'])" 2>/dev/null || echo "")"
MANIFEST_MIGRATIONS="$(python3 -c "import json;print(json.load(open('$BACKUP_DIR/manifest.json'))['schema_migrations_applied'])" 2>/dev/null || echo "null")"
MANIFEST_COMMIT="$(python3 -c "import json;print(json.load(open('$BACKUP_DIR/manifest.json'))['commit'])" 2>/dev/null || echo "unknown")"

# 3) Fresh isolated database + timed restore.
eval "$BAK_ADMIN_PSQL -c \"DROP DATABASE IF EXISTS $TARGET_DB\" -c \"CREATE DATABASE $TARGET_DB\"" >/dev/null
RESTORE_START_S=$(date +%s)
RESTORE_START_MS=$(date +%s%N)
eval "$BAK_PG_RESTORE_CMD -d $TARGET_DB \"$WORK/db.dump\"" > "$WORK/pg_restore.log" 2>&1 || {
  echo "FAIL: pg_restore into $TARGET_DB failed — see $WORK/pg_restore.log (kept in the report dir copy below)" >&2
  cp "$WORK/pg_restore.log" "$BAK_BACKUP_BASE/restore-verification/pg_restore-$TS.log" 2>/dev/null || true
  exit 1
}
RESTORE_END_MS=$(date +%s%N)
RESTORE_MS=$(( (RESTORE_END_MS - RESTORE_START_MS) / 1000000 ))
RESTORE_ERRORS="$(grep -ci "error" "$WORK/pg_restore.log" || true)"
echo "restore: ${RESTORE_MS}ms into $TARGET_DB (log error lines: $RESTORE_ERRORS)"

# 4) Extract uploads + config (+ caddy tars if present).
mkdir -p "$WORK/extracted-uploads" "$WORK/extracted-config" "$WORK/extracted-caddy"
tar -xzf "$WORK/uploads.tar.gz" -C "$WORK/extracted-uploads" --no-same-owner
tar -xzf "$WORK/config.tar.gz" -C "$WORK/extracted-config" --no-same-owner
for ct in "$WORK"/caddy-*.tar.gz; do
  [ -f "$ct" ] || continue
  mkdir -p "$WORK/extracted-caddy/$(basename "$ct" .tar.gz)"
  tar -xzf "$ct" -C "$WORK/extracted-caddy/$(basename "$ct" .tar.gz)" --no-same-owner || echo "WARNING: could not extract $ct" >&2
done

# 5) Validation queries against the restored DB.
q() { eval "$TARGET_PSQL -c \"$1\"" 2>/dev/null | tr -d '[:space:]'; }
MIGRATIONS="$(q 'SELECT count(*) FROM schema_migrations')"
STORES="$(q 'SELECT count(*) FROM stores')"
PRODUCTS="$(q 'SELECT count(*) FROM products')"
ORDERS="$(q 'SELECT count(*) FROM orders')"
ORDER_ITEMS="$(q 'SELECT count(*) FROM order_items')"
LEDGER_ROWS="$(q 'SELECT count(*) FROM ledger_entries')"
LEDGER_SUM="$(q 'SELECT COALESCE(sum(amount),0) FROM ledger_entries')"
INV_QTY="$(q 'SELECT COALESCE(sum(quantity),0) FROM inventory_balances')"
RECEIPTS="$(q 'SELECT count(*) FROM order_payment_receipts')"
DOMAINS="$(q 'SELECT count(*) FROM store_domains')"
ORDERS_BY_STATUS="$(eval "$TARGET_PSQL -c \"SELECT status||':'||count(*) FROM orders GROUP BY status ORDER BY status\"" 2>/dev/null | tr '\n' ',' | sed 's/,$//')"

if [ "$MIGRATIONS" = "$MANIFEST_MIGRATIONS" ]; then
  note_ok "schema_migrations $MIGRATIONS (matches manifest)"
else
  note_fail "schema_migrations $MIGRATIONS != manifest $MANIFEST_MIGRATIONS"
fi
for label in "stores:$STORES" "products:$PRODUCTS" "orders:$ORDERS" "order_items:$ORDER_ITEMS" "ledger_rows:$LEDGER_ROWS" "ledger_sum:$LEDGER_SUM" "inventory_qty:$INV_QTY" "payment_receipts:$RECEIPTS" "store_domains:$DOMAINS"; do
  echo "RESTORED ${label%%:*} ${label#*:}"
done
echo "RESTORED orders_by_status $ORDERS_BY_STATUS"

# Media references: every DB image_url under /static/uploads/ must
# exist as a file in the restored uploads tree.
MEDIA_REFS=0
MEDIA_MISSING=0
while IFS= read -r url; do
  [ -n "$url" ] || continue
  MEDIA_REFS=$((MEDIA_REFS + 1))
  rel="${url#/static/uploads/}"
  [ -f "$WORK/extracted-uploads/static/uploads/$rel" ] || { MEDIA_MISSING=$((MEDIA_MISSING + 1)); echo "MEDIA_MISSING $url" >&2; }
done < <(eval "$TARGET_PSQL -c \"SELECT image_url FROM products WHERE image_url LIKE '/static/uploads/%'\"" 2>/dev/null)
if [ "$MEDIA_MISSING" -eq 0 ]; then
  note_ok "media refs $MEDIA_REFS/$MEDIA_REFS present in uploads"
else
  note_fail "media refs missing $MEDIA_MISSING of $MEDIA_REFS"
fi

# Config presence (G6): the restore must contain the edge config.
for cfg in nginx.conf Caddyfile; do
  if [ -f "$WORK/extracted-config/$cfg" ]; then note_ok "config $cfg present"; else note_fail "config $cfg MISSING from restored config.tar.gz"; fi
done
if ls "$BACKUP_DIR"/caddy-*.tar.gz.enc >/dev/null 2>&1; then
  note_ok "caddy volume archive present in backup"
else
  echo "CHECK_WARN: no caddy volume archive in backup (recorded in manifest.json missing list if the volumes were absent)" >&2
fi

VALIDATE_END_S=$(date +%s)
RECOVERY_S=$((VALIDATE_END_S - RESTORE_START_S))
DATA_AGE_S=""
if [ -n "$MANIFEST_CREATED" ]; then
  CREATED_EPOCH="$(date -d "$MANIFEST_CREATED" +%s 2>/dev/null || echo 0)"
  if [ "$CREATED_EPOCH" -gt 0 ]; then DATA_AGE_S=$(( $(date +%s) - CREATED_EPOCH )); fi
fi
# Interim targets (D3): RPO <= 24h, RTO <= 4h.
RPO_OK="unknown"; RTO_OK="unknown"
if [ -n "$DATA_AGE_S" ]; then
  if [ "$DATA_AGE_S" -le 86400 ]; then RPO_OK="pass"; note_ok "data age ${DATA_AGE_S}s <= 24h (RPO interim target)"; else RPO_OK="fail"; note_fail "data age ${DATA_AGE_S}s > 24h (RPO interim target)"; fi
fi
if [ "$RECOVERY_S" -le 14400 ]; then RTO_OK="pass"; note_ok "recovery time ${RECOVERY_S}s <= 4h (RTO interim target)"; else RTO_OK="fail"; note_fail "recovery time ${RECOVERY_S}s > 4h (RTO interim target)"; fi
if [ "$RESTORE_ERRORS" != "0" ]; then note_fail "pg_restore log contains $RESTORE_ERRORS error lines"; fi

# 6) Report (single generation, written to $REPORT).
mkdir -p "$(dirname "$REPORT")"
FAILURES_JOINED=""
[ "${#FAILURES[@]}" -gt 0 ] && FAILURES_JOINED="$(printf '%s;;' "${FAILURES[@]}")"
REPORT_PATH="$REPORT" REPORT_TS="$(date -u +%FT%TZ)" REPORT_BACKUP="$BACKUP_DIR" REPORT_TARGET="$TARGET_DB" \
REPORT_MIGRATIONS="$MIGRATIONS" REPORT_STORES="$STORES" REPORT_PRODUCTS="$PRODUCTS" \
REPORT_ORDERS="$ORDERS" REPORT_ORDER_ITEMS="$ORDER_ITEMS" REPORT_LEDGER_ROWS="$LEDGER_ROWS" \
REPORT_LEDGER_SUM="$LEDGER_SUM" REPORT_INV_QTY="$INV_QTY" REPORT_RECEIPTS="$RECEIPTS" \
REPORT_DOMAINS="$DOMAINS" REPORT_ORDERS_BY_STATUS="$ORDERS_BY_STATUS" \
REPORT_MEDIA_REFS="$MEDIA_REFS" REPORT_MEDIA_MISSING="$MEDIA_MISSING" \
REPORT_RESTORE_MS="$RESTORE_MS" REPORT_RESTORE_ERRORS="$RESTORE_ERRORS" \
REPORT_RECOVERY_S="$RECOVERY_S" REPORT_DATA_AGE_S="${DATA_AGE_S:-null}" \
REPORT_RPO="$RPO_OK" REPORT_RTO="$RTO_OK" REPORT_COMMIT="$MANIFEST_COMMIT" \
REPORT_CREATED="$MANIFEST_CREATED" REPORT_FAILURES="$FAILURES_JOINED" \
python3 - <<'PYEOF'
import json, os
failures = [f for f in os.environ["REPORT_FAILURES"].split(";;") if f]
def num(v, default=None):
    try: return int(v)
    except (TypeError, ValueError): return default
report = {
    "job": "restore-verify",
    "timestamp_utc": os.environ["REPORT_TS"],
    "backup_dir": os.environ["REPORT_BACKUP"],
    "backup_commit": os.environ["REPORT_COMMIT"],
    "backup_created_utc": os.environ["REPORT_CREATED"],
    "target_db": os.environ["REPORT_TARGET"],
    "restore_ms": num(os.environ["REPORT_RESTORE_MS"]),
    "restore_log_error_lines": num(os.environ["REPORT_RESTORE_ERRORS"], 0),
    "recovery_seconds": num(os.environ["REPORT_RECOVERY_S"]),
    "data_age_seconds": num(os.environ["REPORT_DATA_AGE_S"]),
    "targets": {"rpo_le_24h": os.environ["REPORT_RPO"], "rto_le_4h": os.environ["REPORT_RTO"]},
    "validation": {
        "schema_migrations": num(os.environ["REPORT_MIGRATIONS"]),
        "stores": num(os.environ["REPORT_STORES"]),
        "products": num(os.environ["REPORT_PRODUCTS"]),
        "orders": num(os.environ["REPORT_ORDERS"]),
        "orders_by_status": os.environ["REPORT_ORDERS_BY_STATUS"],
        "order_items": num(os.environ["REPORT_ORDER_ITEMS"]),
        "ledger_rows": num(os.environ["REPORT_LEDGER_ROWS"]),
        "ledger_sum": num(os.environ["REPORT_LEDGER_SUM"]),
        "inventory_quantity": num(os.environ["REPORT_INV_QTY"]),
        "payment_receipts": num(os.environ["REPORT_RECEIPTS"]),
        "store_domains": num(os.environ["REPORT_DOMAINS"]),
        "media_refs": num(os.environ["REPORT_MEDIA_REFS"]),
        "media_missing": num(os.environ["REPORT_MEDIA_MISSING"]),
    },
    "failures": failures,
    "result": "PASS" if not failures and os.environ["REPORT_RPO"] == "pass" and os.environ["REPORT_RTO"] == "pass" else "FAIL",
}
with open(os.environ["REPORT_PATH"], "w") as f:
    json.dump(report, f, indent=2, sort_keys=True)
    f.write("\n")
PYEOF
cp "$WORK/pg_restore.log" "$(dirname "$REPORT")/pg_restore-$TS.log"
echo "REPORT $REPORT"
echo "RESULT: restore ${RESTORE_MS}ms, recovery ${RECOVERY_S}s, data age ${DATA_AGE_S:-unknown}s, failures: ${#FAILURES[@]}"

# 7) Optional durable record of the verification.
if [ "${BAK_PUBLISH_RESTORE_SIGNAL:-0}" = "1" ] && [ -n "${BAK_PSQL_CMD:-}" ]; then
  eval "$BAK_PSQL_CMD -c \"INSERT INTO backup_signals(kind,last_success_at,verified_artifacts,updated_at) VALUES('restore_verification',now(),'restore-verify $TARGET_DB',now()) ON CONFLICT (kind) DO UPDATE SET last_success_at=EXCLUDED.last_success_at, verified_artifacts=EXCLUDED.verified_artifacts, updated_at=now();\"" >/dev/null 2>&1 \
    && echo "restore_verification signal published" || echo "WARNING: restore_verification signal publish failed" >&2
fi

# 8) Drop the disposable DB unless asked to keep it.
if [ "$KEEP_DB" -eq 0 ]; then
  eval "$BAK_ADMIN_PSQL -c \"DROP DATABASE IF EXISTS $TARGET_DB\"" >/dev/null
  echo "disposable DB $TARGET_DB dropped"
else
  echo "disposable DB $TARGET_DB KEPT (--keep-db)"
fi

[ "${#FAILURES[@]}" -eq 0 ] && [ "$RPO_OK" = "pass" ] && [ "$RTO_OK" = "pass" ]
