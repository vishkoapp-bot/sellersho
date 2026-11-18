#!/bin/bash
# publish-backup-success.sh — OBS-01 D-02 backup-success signal publisher.
#
# Usage: scripts/publish-backup-success.sh <backup-dir> [kind]
#   backup-dir: directory holding db.sql.gz, uploads.tar.gz, config.tar.gz
#   kind:       signal kind, default "nightly"
#
# Verifies ALL THREE artifacts (present, non-empty, gzip -t clean) and
# ONLY THEN upserts the verified timestamp into backup_signals via the
# local Postgres container. A failed verification exits non-zero and
# publishes NOTHING — the poller treats unknown backup state as unknown,
# never as success. No credentials in this file: DB access is via
# `docker exec` local-socket auth, same as backup-auto.sh.
#
# Env overrides (defaults are the prod values):
#   BACKUP_PG_CONTAINER (drup_postgres), BACKUP_PG_USER (drup_admin),
#   BACKUP_PG_DB (drup_db)
set -euo pipefail

DIR="${1:-}"
KIND="${2:-nightly}"
if [ -z "$DIR" ]; then
  echo "usage: $0 <backup-dir> [kind]" >&2
  exit 2
fi
CONTAINER="${BACKUP_PG_CONTAINER:-drup_postgres}"
PGUSER="${BACKUP_PG_USER:-drup_admin}"
PGDB="${BACKUP_PG_DB:-drup_db}"

# 1) Verify every artifact. Any failure aborts before any publish.
for f in db.sql.gz uploads.tar.gz config.tar.gz; do
  path="$DIR/$f"
  test -s "$path" || { echo "publish-backup-success: missing or empty $path" >&2; exit 1; }
  gzip -t "$path" || { echo "publish-backup-success: corrupt gzip $path" >&2; exit 1; }
done

# 2) Publish the verified timestamp. The row is the single source the
# observability poller reads; last_success_at is server time (now()),
# recorded only after the verification above passed.
docker exec "$CONTAINER" psql -U "$PGUSER" -d "$PGDB" -v ON_ERROR_STOP=1 -c \
  "INSERT INTO backup_signals(kind,last_success_at,verified_artifacts,updated_at) VALUES('$KIND',now(),'db.sql.gz,uploads.tar.gz,config.tar.gz',now()) ON CONFLICT (kind) DO UPDATE SET last_success_at=EXCLUDED.last_success_at, verified_artifacts=EXCLUDED.verified_artifacts, updated_at=now();"

echo "BACKUP_SIGNAL_OK $KIND $DIR"
