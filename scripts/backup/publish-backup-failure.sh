#!/bin/bash
# publish-backup-failure.sh — BAK-01 G8 immediate backup-failure notice.
#
# Usage: scripts/backup/publish-backup-failure.sh <kind> [detail...]
#   kind:   nightly | deploy (which backup failed)
#   detail: free text describing the failure (no secrets — the entry
#           point passes only a short reason, never a key or env dump)
#
# Two delivery attempts, in order:
#   1. The in-app admin inbox, via a direct INSERT into
#      notification_outbox — the exact table/shape the observability
#      alert poller uses (backend/internal/observability/alerts.go)
#      for operations_alert notifications, so the notice lands in
#      the same admin inbox as every other operations alert, with
#      alertCode=backup_failure. This is the IMMEDIATE route: it
#      fires at failure time instead of waiting for the 26 h
#      staleness notice (which stays as the backstop).
#   2. Always (even when the DB insert fails — e.g. the DB itself is
#      down, in which case no DB-mediated route can work): a durable
#      marker file + a line in backup-failures.log under
#      $BAK_BACKUP_BASE. scripts/backup/check-backup-health.sh
#      retries pending markers (see the runbook).
#
# Exit 0 when the inbox insert succeeded, 1 otherwise (the marker
# is written either way). Callers must call this best-effort
# (|| true) — it must never mask the original backup exit code.
#
# Env overrides (defaults are the prod values):
#   BAK_ROOT (default /opt/drup)
#   BAK_BACKUP_BASE (default $BAK_ROOT/backups)
#   BAK_KEY_FILE is NOT needed here (no decryption).
#   BAK_PSQL_CMD — if set, the psql command is used verbatim (the
#     rehearsal/test harness sets it to a local psql). Otherwise the
#     docker-exec form is built from BACKUP_PG_CONTAINER (default
#     drup_postgres), BACKUP_PG_USER (drup_admin), BACKUP_PG_DB
#     (drup_db). No credentials in this file: DB access is via
#     `docker exec` local-socket auth, same as backup-auto.sh.
set -euo pipefail
umask 077

KIND="${1:-unknown}"
shift || true
DETAIL="${*:-backup failed (no detail recorded)}"
BAK_ROOT="${BAK_ROOT:-/opt/drup}"
BAK_BACKUP_BASE="${BAK_BACKUP_BASE:-$BAK_ROOT/backups}"
mkdir -p "$BAK_BACKUP_BASE"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
TS_HUMAN="$(date -u +%FT%TZ)"

# 1) Durable on-disk record — always written, DB or no DB,
# unless this is a retry (BAK_FAILURE_NO_MARKER=1): a retry that
# wrote a fresh marker would leave a new pending marker behind
# after every run.
if [ "${BAK_FAILURE_NO_MARKER:-0}" != "1" ]; then
  MARKER="$BAK_BACKUP_BASE/.backup-failure-${KIND}-$TS.marker"
  {
    echo "kind=$KIND"
    echo "timestamp_utc=$TS_HUMAN"
    echo "detail=$DETAIL"
  } > "$MARKER"
  chmod 600 "$MARKER"
else
  MARKER="(retry — no new marker)"
fi
echo "[$TS_HUMAN] BACKUP_FAILURE kind=$KIND detail=$DETAIL marker=$MARKER" >> "$BAK_BACKUP_BASE/backup-failures.log"

# 2) Immediate in-app admin inbox notice.
if [ -n "${BAK_PSQL_CMD:-}" ]; then
  PSQL_CMD="$BAK_PSQL_CMD"
else
  CONTAINER="${BACKUP_PG_CONTAINER:-drup_postgres}"
  PGUSER="${BACKUP_PG_USER:-drup_admin}"
  PGDB="${BACKUP_PG_DB:-drup_db}"
  PSQL_CMD="docker exec $CONTAINER psql -U $PGUSER -d $PGDB -v ON_ERROR_STOP=1"
fi

# Single-quote-escape the detail for the SQL literal (defence in
# depth — the detail comes from our own scripts, never from user
# input — but a stray quote must not break the insert).
ESCAPED_DETAIL="${DETAIL//\'/\'\'}"
BODY="نسخه پشتیبان ($KIND) در $TS_HUMAN ناموفق بود: $ESCAPED_DETAIL — داشبورد ظرفیت و وضعیت سرویس را بررسی کنید."
SQL="INSERT INTO notification_outbox(recipient_user_id,kind,title,body,payload) SELECT u.id,'operations_alert','خطا یا تأخیر نسخه پشتیبان','$BODY',jsonb_build_object('alertCode','backup_failure','tab','jobs','backupKind','$KIND') FROM users u WHERE u.is_active AND (u.role='admin' OR EXISTS(SELECT 1 FROM user_capabilities c WHERE c.user_id=u.id AND c.capability='super_admin' AND c.status='active'));"

if eval "$PSQL_CMD -c \"$SQL\"" >/dev/null 2>&1; then
  echo "BACKUP_FAILURE_NOTICE_OK $KIND $MARKER"
  exit 0
else
  echo "BACKUP_FAILURE_NOTICE_FAILED $KIND (DB insert failed — marker file $MARKER + backup-failures.log are the record; check-backup-health.sh will retry)" >&2
  exit 1
fi
