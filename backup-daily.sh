#!/bin/bash
# Selersho daily backup: Postgres dump + uploads archive. Keeps last 7 days.
# SEC-01 (gate G-14): DB access is via `docker exec` local-socket auth —
# the same pattern as backup-auto.sh. The previous version grepped
# POSTGRES_PASSWORD out of docker-compose.prod.yml and exported it as
# PGPASSWORD; it only worked because the live file still held literals.
# No credentials in this file. Backup files are created mode 0600
# (umask 077, gate G-13); the parent dir must be mode 0700.
set -e
umask 077
BACKUP_DIR=/opt/drup/backups
DATE=$(date +%Y%m%d-%H%M%S)
mkdir -p "$BACKUP_DIR"
cd /opt/drup
docker exec drup_postgres pg_dump -U drup_admin -d drup_db | gzip > "$BACKUP_DIR/db-$DATE.sql.gz"
tar -czf "$BACKUP_DIR/uploads-$DATE.tar.gz" -C /opt/drup static/uploads
find "$BACKUP_DIR" -name 'db-*.sql.gz' -mtime +7 -delete
find "$BACKUP_DIR" -name 'uploads-*.tar.gz' -mtime +7 -delete
ls -lh "$BACKUP_DIR" | grep "$DATE"
echo BACKUP_OK
