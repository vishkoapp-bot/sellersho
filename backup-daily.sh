#!/bin/bash
# Selersho daily backup: Postgres dump + uploads archive. Keeps last 7 days.
set -e
BACKUP_DIR=/opt/drup/backups
DATE=$(date +%Y%m%d-%H%M%S)
mkdir -p "$BACKUP_DIR"
cd /opt/drup
PGPASS=$(grep -E '^\s*POSTGRES_PASSWORD:' docker-compose.prod.yml | head -1 | sed 's/.*: *//')
PGPASSWORD="$PGPASS" docker compose -f docker-compose.yml -f docker-compose.prod.yml exec -T postgres pg_dump -U drup_admin -d drup_db | gzip > "$BACKUP_DIR/db-$DATE.sql.gz"
tar -czf "$BACKUP_DIR/uploads-$DATE.tar.gz" -C /opt/drup static/uploads
find "$BACKUP_DIR" -name 'db-*.sql.gz' -mtime +7 -delete
find "$BACKUP_DIR" -name 'uploads-*.tar.gz' -mtime +7 -delete
ls -lh "$BACKUP_DIR" | grep "$DATE"
echo BACKUP_OK
