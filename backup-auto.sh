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

ls -lh "$DEST"
echo "BACKUP_AUTO_OK $DATE"
