#!/bin/bash
# Selersho automated backup (installed 2026-10-21, overnight stability wave).
# Nightly: Postgres dump + uploads archive + config archive.
# Layout: /opt/drup/backups/auto/<YYYY-MM-DD>/  — keeps the last 7 days.
# DB access is via `docker exec` local-socket auth: no credentials in this file.
# Backup files are created mode 0600 (umask 077, SEC-01 gate G-13).
set -euo pipefail
umask 077
BASE=/opt/drup/backups/auto
DATE=$(date +%F)
DEST="$BASE/$DATE"
mkdir -p "$DEST"

# 1) Database dump (compressed)
docker exec drup_postgres pg_dump -U drup_admin -d drup_db | gzip > "$DEST/db.sql.gz.tmp"
mv "$DEST/db.sql.gz.tmp" "$DEST/db.sql.gz"
test -s "$DEST/db.sql.gz"

# 2) Uploaded files
tar -czf "$DEST/uploads.tar.gz.tmp" -C /opt/drup static/uploads
mv "$DEST/uploads.tar.gz.tmp" "$DEST/uploads.tar.gz"

# 3) Runtime config (env + compose files)
tar -czf "$DEST/config.tar.gz.tmp" -C /opt/drup .env docker-compose.yml docker-compose.prod.yml
mv "$DEST/config.tar.gz.tmp" "$DEST/config.tar.gz"

# 4) Rotation: remove dated backup dirs older than 7 days
find "$BASE" -mindepth 1 -maxdepth 1 -type d -mtime +7 -exec rm -rf {} +

# 5) OBS-01 D-02: publish the verified backup-success signal. The
# publisher re-verifies all three artifacts (non-empty + gzip -t) and
# only then upserts backup_signals; the observability poller reads that
# row into sellersho_backup_last_success_timestamp_seconds. Deploy note:
# scripts/publish-backup-success.sh must sit at
# /opt/drup/scripts/publish-backup-success.sh next to this file. A
# publish failure warns loudly but does not fail the backup itself —
# the files on disk are the source of truth, and the poller treats an
# unpublished signal as unknown, never as success.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -x "$SCRIPT_DIR/scripts/publish-backup-success.sh" ]; then
  "$SCRIPT_DIR/scripts/publish-backup-success.sh" "$DEST" || echo "WARNING: backup-success signal publish failed for $DEST" >&2
else
  echo "WARNING: publisher $SCRIPT_DIR/scripts/publish-backup-success.sh missing; backup-success signal NOT published" >&2
fi

ls -lh "$DEST"
echo "BACKUP_AUTO_OK $DATE"
