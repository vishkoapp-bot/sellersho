#!/bin/bash
# Selersho automated backup — BAK-01: thin wrapper, retained only so
# any existing invocation keeps working.
#
# The implementation lives in scripts/backup/backup-entrypoint.sh
# (the ONE consolidated backup entry point: pipefail, failure
# propagation, umask 077, checksums/manifest, encryption, retention).
# The 04:41 Tehran cron must run the entry point directly:
#   /opt/drup/scripts/backup/backup-entrypoint.sh run nightly
# (the deploy coordinator repoints the existing cron to it; the
# schedule does not change). This file must NOT grow a second
# implementation — keeping two nightly scripts is what made the
# cron's target ambiguous (BAK-01 G9).
#
# DB access is via `docker exec` local-socket auth (or the env
# overrides the entry point documents). No credentials in this file.
# Backup files are created mode 0600 (umask 077).
set -euo pipefail
umask 077
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
echo "backup-auto.sh is a BAK-01 compatibility wrapper; the entry point is scripts/backup/backup-entrypoint.sh" >&2
exec "$SCRIPT_DIR/scripts/backup/backup-entrypoint.sh" run nightly "$@"
