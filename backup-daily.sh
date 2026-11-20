#!/bin/bash
# Selersho daily backup — BAK-01: thin wrapper, retained only so any
# existing cron/invocation keeps working.
#
# The implementation lives in scripts/backup/backup-entrypoint.sh
# (the ONE consolidated backup entry point: pipefail, failure
# propagation, umask 077, checksums/manifest, encryption, retention).
# The 04:41 Tehran cron must run the entry point directly:
#   /opt/drup/scripts/backup/backup-entrypoint.sh run nightly
# (the deploy coordinator repoints the existing cron to it; the
# schedule does not change). This file must NOT grow a second
# implementation — the old version silently wrote BACKUP_OK when
# pg_dump failed (missing pipefail, BAK-01 G1).
#
# DB access is via `docker exec` local-socket auth (or the env
# overrides the entry point documents). No credentials in this file.
# Backup files are created mode 0600 (umask 077); the parent dir
# must be mode 0700.
set -euo pipefail
umask 077
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "backup-daily.sh is a BAK-01 compatibility wrapper; the entry point is scripts/backup/backup-entrypoint.sh" >&2
exec "$SCRIPT_DIR/scripts/backup/backup-entrypoint.sh" run nightly "$@"
