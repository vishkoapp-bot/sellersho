#!/bin/bash
# check-backup-health.sh — BAK-01 G8 fallback health check.
#
# Usage: scripts/backup/check-backup-health.sh [--retry-markers] [--stale-hours N]
#
# Reads the durable failure markers left by backup-entrypoint.sh /
# publish-backup-failure.sh and the latest verified backup, and
# reports the backup health of the host. This is the FALLBACK for
# the case the primary failure notice could not reach the in-app
# inbox (e.g. the DB was down when the backup failed): the marker
# file survives on disk and this check re-attempts delivery once
# the DB is back, and surfaces staleness even when no failure was
# ever recorded.
#
#   --retry-markers   re-attempt the inbox insert for every pending
#                     .backup-failure-*.marker (via
#                     publish-backup-failure.sh); a marker whose
#                     insert succeeds is renamed to .acked.
#   --stale-hours N   staleness threshold in hours (default 26, the
#                     same threshold as the SellershoBackupStale
#                     rule / in-app backup_failure alert).
#
# Exit 0: no pending failure markers and a verified backup within
# the threshold. Exit 1: pending failures or stale backup (or both)
# — the details are printed on stdout for the cron mail/log.
#
# PROPOSED schedule (NOT created — needs owner approval naming the
# schedule, same rule as the restore-verification cron): daily
# 05:05 Tehran, right after the 04:41 nightly backup, e.g.
#   5 5 * * * /opt/drup/scripts/backup/check-backup-health.sh --retry-markers >> /opt/drup/backups/backup-health.log 2>&1
# See docs/operations/backup-runbook.md.
#
# Env overrides (defaults are the prod values):
#   BAK_ROOT (default /opt/drup), BAK_BACKUP_BASE (default $BAK_ROOT/backups)
#   BAK_PSQL_CMD / BACKUP_PG_* — same as publish-backup-failure.sh
#     (used only to read backup_signals for the staleness check).
set -u
umask 077

BAK_ROOT="${BAK_ROOT:-/opt/drup}"
BAK_BACKUP_BASE="${BAK_BACKUP_BASE:-$BAK_ROOT/backups}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RETRY=0
STALE_HOURS=26

while [ $# -gt 0 ]; do
  case "$1" in
    --retry-markers) RETRY=1; shift;;
    --stale-hours) STALE_HOURS="$2"; shift 2;;
    *) echo "usage: $0 [--retry-markers] [--stale-hours N]" >&2; exit 2;;
  esac
done

STATUS=0

# 1) Pending failure markers.
for marker in "$BAK_BACKUP_BASE"/.backup-failure-*.marker; do
  [ -f "$marker" ] || continue
  echo "PENDING_FAILURE_MARKER $marker"
  cat "$marker"
  if [ "$RETRY" -eq 0 ]; then
    STATUS=1
    continue
  fi
  kind="$(grep '^kind=' "$marker" | cut -d= -f2-)"
  detail="$(grep '^detail=' "$marker" | cut -d= -f2-)"
  if [ -x "$SCRIPT_DIR/publish-backup-failure.sh" ]; then
    # The retry re-attempts the inbox insert; the marker is
    # renamed only when the insert succeeds, so a still-dead DB
    # keeps the marker pending for the next run. The retry itself
    # must NOT write a fresh marker (BAK_FAILURE_NO_MARKER=1) —
    # otherwise every retry leaves a new pending marker behind.
    if BAK_ROOT="$BAK_ROOT" BAK_BACKUP_BASE="$BAK_BACKUP_BASE" BAK_FAILURE_NO_MARKER=1 \
         "$SCRIPT_DIR/publish-backup-failure.sh" "${kind:-unknown}" "retry of failed backup: ${detail:-unspecified}" >/dev/null 2>&1; then
      mv "$marker" "$marker.acked"
      echo "MARKER_ACKED $marker"
    else
      echo "MARKER_RETRY_FAILED $marker (DB still unreachable — left pending)"
      STATUS=1
    fi
  else
    echo "MARKER_RETRY_FAILED $marker (failure publisher missing)"
    STATUS=1
  fi
done

# 2) Staleness: newest verified backup. Prefer the backup_signals
# row (the same source the poller reads); fall back to the newest
# nightly backup dir's manifest timestamp when the DB is unreadable.
LAST_OK=""
if [ -n "${BAK_PSQL_CMD:-}" ]; then
  PSQL_CMD="$BAK_PSQL_CMD"
else
  CONTAINER="${BACKUP_PG_CONTAINER:-drup_postgres}"
  PGUSER="${BACKUP_PG_USER:-drup_admin}"
  PGDB="${BACKUP_PG_DB:-drup_db}"
  PSQL_CMD="docker exec $CONTAINER psql -U $PGUSER -d $PGDB -v ON_ERROR_STOP=1 -At"
fi
LAST_OK="$(eval "$PSQL_CMD -c \"SELECT EXTRACT(EPOCH FROM MAX(last_success_at))::bigint FROM backup_signals WHERE kind='nightly'\"" 2>/dev/null | tr -d '[:space:]' || true)"
if [ -n "$LAST_OK" ] && [ "$LAST_OK" != "" ]; then
  AGE_HOURS=$(( ($(date +%s) - LAST_OK) / 3600 ))
  echo "LAST_VERIFIED_BACKUP epoch=$LAST_OK age_hours=$AGE_HOURS (source: backup_signals)"
  if [ "$AGE_HOURS" -ge "$STALE_HOURS" ]; then
    echo "STALE_BACKUP: no verified nightly backup within ${STALE_HOURS}h"
    STATUS=1
  fi
else
  # DB unreadable — fall back to manifest timestamps on disk.
  NEWEST="$(ls -dt "$BAK_BACKUP_BASE"/auto/*/manifest.json 2>/dev/null | head -n 1 || true)"
  if [ -n "$NEWEST" ]; then
    CREATED="$(python3 -c "import json;print(json.load(open('$NEWEST'))['created_utc'])" 2>/dev/null || echo "")"
    echo "LAST_VERIFIED_BACKUP manifest=$NEWEST created_utc=$CREATED (source: manifest on disk; backup_signals unreadable — DB may be down)"
    if [ -n "$CREATED" ]; then
      AGE_S=$(( $(date +%s) - $(date -d "$CREATED" +%s 2>/dev/null || echo 0) ))
      AGE_HOURS=$(( AGE_S / 3600 ))
      if [ "$AGE_HOURS" -ge "$STALE_HOURS" ]; then
        echo "STALE_BACKUP: no verified nightly backup within ${STALE_HOURS}h"
        STATUS=1
      fi
    fi
  else
    echo "STALE_BACKUP: no nightly backup dir with a manifest found at all"
    STATUS=1
  fi
fi

if [ "$STATUS" -eq 0 ]; then
  echo "BACKUP_HEALTH_OK"
fi
exit "$STATUS"
