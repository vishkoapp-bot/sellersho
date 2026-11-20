# Backup + restore runbook (BAK-01)

One entry point, one layout, one verification story. This runbook is
the manual companion to `scripts/backup/backup-entrypoint.sh`; the
periodic job is `scripts/backup/restore-verify.sh`.

**Rollback is a different runbook:** deploy failures are handled by
`docs/operations/rollback-runbook.md` (MIG-02 — schema-aware
rollback: compatible-binary restore, Mode A/B forward repair, and
the OWNER-PENDING DR write-freeze procedure). A deploy rollback
never restores the database; a database restore is DR-only and its
procedure lives in §5 below.

## 1. What exists

| Piece | Path (on the host) | Purpose |
|---|---|---|
| Entry point | `/opt/drup/scripts/backup/backup-entrypoint.sh` | The ONLY backup implementation. Nightly + deploy. |
| Success publisher | `/opt/drup/scripts/publish-backup-success.sh` | Verifies a backup set, then upserts `backup_signals`. |
| Failure publisher | `/opt/drup/scripts/backup/publish-backup-failure.sh` | Immediate failure notice into the admin inbox + marker file. |
| Health check | `/opt/drup/scripts/backup/check-backup-health.sh` | Fallback: retries pending failure markers, reports staleness. |
| Restore verification | `/opt/drup/scripts/backup/restore-verify.sh` | Weekly job: real restore into an isolated DB + validation. |
| Wrappers | `/opt/drup/backup-daily.sh`, `/opt/drup/backup-auto.sh` | Compatibility only — both exec the entry point. |
| Test harness | `scripts/backup/test-backup-entrypoint.sh` (repo) | Full suite: failure injection, corruption, encryption, retention, restore. |
| Backups | `/opt/drup/backups/auto/<YYYY-MM-DD>/` (nightly), `/opt/drup/backups/deploy-<commit>-<ts>/` (deploy) | Encrypted sets + manifests. |
| Encryption key | `/opt/drup/.backup-encryption-key` (root-only 0600) | Generated server-side at deploy; value never printed/stored by agents. |

## 2. The nightly backup

Cron (04:41 Tehran — the schedule does NOT change; the deploy
coordinator repoints the existing job to the entry point). The
`BAK_CADDY_VOLUMES` override is kept as belt-and-braces now that
the entry-point default matches it (MIG-02 step 7):

```
41 4 * * * BAK_CADDY_VOLUMES="drup_caddy_data drup_caddy_config" /opt/drup/scripts/backup/backup-entrypoint.sh run nightly >> /opt/drup/backups/backup-entrypoint.log 2>&1
```

What one run does, in order: `pg_dump -Fc` + plain `pg_dump | gzip`
(both must succeed — `pipefail`; a failure aborts with a nonzero
exit and NO success marker), uploads tar, config tar (`.env`, all
compose files, `nginx.conf`, `Caddyfile`, `.deployed-commit`,
`.deployed-release.json`), Caddy ACME volume tars
(`drup_caddy_data`, `drup_caddy_config` — MIG-02 step 7 made the
entry-point default match the live host, where the compose project
name `drup_` prefixes the volumes; the old default matched no
volume, so Caddy data was silently recorded missing. If the install
dir / project name ever changes, the default must move with it; a
missing volume is warned about and recorded in
`manifest.json`, not fatal), `MANIFEST.sha256` + `manifest.json`
(kind, UTC timestamp, commit, pg version, migration count,
artifact sizes/hashes, missing items), encryption of every artifact
(openssl AES-256-CBC + PBKDF2 + salt, key file), self-verification
by decrypting the sealed set (manifest + gzip + `pg_restore
--list`), atomic publish (`.tmp` dir + `mv`), retention (nightly
7 days; deploy newest 5), off-site copy if configured (default
OFF), success-signal publish.

Exit codes: `0` ok; `1` backup failed (no marker, failure notice
attempted); `2` usage; `3` backup files verified GOOD but the
success signal or the off-site copy failed — distinct state, marker
`.signal-failed` / `.offsite-failed` left in the backup dir, failure
notice attempted, 26 h staleness alert is the backstop. The nightly
cron surfaces exit 3 as a failure; the deploy scripts treat it as a
loud warning and continue (the backup itself is good).

Success is the `backup_signals` row (kind `nightly` / `deploy`) and
the stdout line `BACKUP_OK <kind> <dir>`. There is NO marker file
for success. The 26 h staleness notice (`backup_failure` in the
admin inbox, `SellershoBackupStale` rule) stays as the backstop, not
the primary route: a failure now also writes an IMMEDIATE notice
into the admin inbox (`notification_outbox`, kind
`operations_alert`, alertCode `backup_failure`) plus a durable
marker file `.backup-failure-<kind>-<ts>.marker` and a line in
`backup-failures.log`.

Redis data is deliberately NOT backed up: holds are rebuildable
from PG (`redis-recover rebuild`), OTPs live 120 s and are not
recoverable by design — see `docs/operations/redis-recovery.md`.

## 3. Encryption key — generation, storage, recovery

The key is a passphrase file, read by openssl (`-pass
file:<key>`). It is NOT in the repo, NOT in any backup, and its
value is never printed or stored by agents.

Generate (server-side, at deploy time, as root):

```
openssl rand -base64 48 > /opt/drup/.backup-encryption-key
chmod 600 /opt/drup/.backup-encryption-key
chown root:root /opt/drup/.backup-encryption-key
```

Storage (owner decision, BAK-01 G3): the host file PLUS an offline
copy held by the owner PLUS an offline copy held by a named deputy
(Haji-Bashi proposed). A single copy held only on the prod host is
NOT recovery. **Loss of the key = loss of every encrypted backup** —
state this to the owner when the scheme is approved.

Recovery test: at least once per quarter (the bare-host drill, §6)
the key is fetched from the OFFLINE holder copy — never from the
host — and used to decrypt. Rotation: on any holder change or
suspected exposure; keep the old key offline until every backup
encrypted under it has aged out of retention (7 days nightly / 5
deploy backups). A missing or unreadable key makes the entry point
FAIL LOUDLY (exit 1, no backup). There is no unencrypted fallback.

Why openssl and not age: age is not installed on the host and
installing it needs a deploy-time package decision; openssl is
already present. openssl enc has no authentication, so integrity is
provided by the sha256 manifest, verified after decryption at
backup time, at deploy-verify time, and at restore-verification
time.

## 4. Restore — the fast path (restore-verify.sh)

On an ISOLATED host (never prod, never the backup host's live DB):

```
export BAK_ADMIN_PSQL="psql -h <isolated-host> -U <user> -d postgres -v ON_ERROR_STOP=1 -At"
export BAK_PG_RESTORE_CMD="pg_restore -h <isolated-host> -U <user>"
export BAK_KEY_FILE=/path/to/key   # the offline holder copy for drills
scripts/backup/restore-verify.sh <backup-dir> --report <report.json>
```

The job: entry-point `verify` (manifest + decrypt + gzip +
`pg_restore --list`), real `pg_restore` into a fresh disposable DB,
extract uploads/config/Caddy tars, validation queries, timings,
JSON report. Exit 0 only when every check passes AND the interim
targets hold (RPO ≤ 24 h data age, RTO ≤ 4 h recovery time). The
disposable DB is dropped afterwards (unless `--keep-db`).

Measured on the synthetic rehearsal set (210 migrations, 2026-11-20):
`pg_restore` 38.1 s, recovery (restore start → validated) 39 s,
data age 51 s — a LOWER BOUND, not a prod RTO: prod volume and the
host-rebuild steps (provision, install, certs, DNS, smoke) were not
measured and dominate a real RTO.

## 5. Restore — manual steps (when the job cannot run)

```
# 1. Verify the set (needs the key):
scripts/backup/backup-entrypoint.sh verify <backup-dir>
# 2. Decrypt to a scratch dir (or let restore-verify.sh do it):
mkdir /tmp/restore && cd <backup-dir>
for enc in $(cd <backup-dir> && find . -name '*.enc' -type f); do
  mkdir -p /tmp/restore/$(dirname ${enc%.enc})
  openssl enc -d -aes-256-cbc -pbkdf2 -md sha256 -salt \
    -pass file:/path/to/key -in "$enc" -out "/tmp/restore/${enc%.enc}"
done
(cd /tmp/restore && sha256sum -c MANIFEST.sha256)
# 3. Restore the DB into a FRESH database:
createdb <target> && pg_restore -d <target> /tmp/restore/db.dump
# 4. Extract uploads + config (+ Caddy volumes if present):
tar -xzf /tmp/restore/uploads.tar.gz -C /tmp/restore/u --no-same-owner
tar -xzf /tmp/restore/config.tar.gz -C /tmp/restore/c --no-same-owner
# 5. Validate (§6 checklist) BEFORE pointing any service at it.
# 6. Boot the backend against the restored DB in isolation and run
#    the smoke suite (health, login, catalog, idempotent order
#    recovery). Only then promote the restored DB.
```

## 6. Validation checklist (every restore)

| Check | Source of truth |
|---|---|
| `schema_migrations` count == `manifest.json` value | manifest |
| Stores / products / orders (+ statuses) / order items | restored DB, compare with pre-loss counts if known |
| Ledger rows + sum | restored DB |
| Inventory quantity sum | restored DB |
| Payment receipts count (verified chain) | restored DB |
| Custom domains (active) | restored DB |
| Media refs: every `products.image_url` under `/static/uploads/` exists in the restored uploads tree | DB vs extracted `uploads.tar.gz` |
| Config present: `nginx.conf`, `Caddyfile` | extracted `config.tar.gz` |
| Caddy volume archive present (if the host used SSL) | backup dir |
| Data age (now − `manifest.json.created_utc`) ≤ 24 h | interim RPO target |
| Recovery time (restore start → validated) ≤ 4 h | interim RTO target |

## 7. Failure decision tree

| Symptom | Meaning | Action |
|---|---|---|
| Entry point exit 1, `pg_dump … failed` | DB unreachable / dump error | Check the DB + container; no backup dir was published; the previous backup is still the latest good one. Fix, re-run. |
| Entry point exit 1, `encryption key missing` | Key file absent/unreadable | Restore the key from the offline holder copy (never invent one — a new key cannot decrypt old backups). |
| `verify` fails: encrypted manifest | Ciphertext tampered / truncated | Do NOT use this backup; fall back to the previous verified backup; investigate disk. |
| `verify` fails: plaintext manifest | Decrypted content ≠ recorded hashes (silent data change) | Same as above — this is the check `pg_restore --list` cannot do. |
| `verify` fails: `gzip -t` | Truncated/corrupt gzip artifact | Same as above. |
| `verify` fails: `pg_restore --list` | Catalogue unreadable | Same as above. |
| `pg_restore` fails during a real restore | Data corruption the catalogue check passed | Try the previous verified backup; if several fail, suspect the disk/host, not the backups. |
| Validation mismatch after restore | Restored DB ≠ manifest (migrations, sums) | Do NOT promote; keep the restore isolated; compare against the manifest and the previous backup. |
| Entry point exit 3, `.signal-failed` | Backup good, signal not published | Check the publisher + `backup_signals` + DB health; re-run the publisher by hand; the 26 h staleness alert is the backstop. |
| Entry point exit 3, `.offsite-failed` | Backup good, off-site copy failed | Check the destination + network; re-copy with rsync and verify hashes; local backup is unaffected. |
| Failure marker files present | A run failed and the inbox insert may have failed too | Run `check-backup-health.sh --retry-markers` (re-attempts the inbox insert; acked markers are renamed `.acked`). |
| No verified backup within 26 h | Staleness alert fires | Treat as a backup outage: run the entry point by hand, watch the exit code, check the markers. |

## 8. Periodic verification + drills (proposed schedule — NOT created)

Creating the crons needs owner approval naming the schedule (D4).
Proposed:

- **Weekly restore verification, Sunday 05:30 Tehran** (after the
  nightly backup, before business hours), on an isolated host:
  `restore-verify.sh <latest backup dir>` with the report kept
  under `backups/restore-verification/`. Also after every
  deploy-backup change and after any restore-affecting change (PG
  upgrade, new migration type, new backup script version).
- **Quarterly bare-host drill (half-day):** start from a bare host
  with NO local state — fetch the latest encrypted backup set from
  the off-site copy, fetch the key from the OFFLINE holder copy
  (rotating which holder supplies it), restore, validate (§6),
  boot the backend in isolation, run the smoke suite. This is the
  acceptance drill the spec requires ("loss of primary host plus
  restore credentials, not only `pg_restore --list`").
- **Daily health check (proposed 05:05 Tehran):**
  `check-backup-health.sh --retry-markers`.

Alert on any verification failure or on staleness > 8 days (the
in-app inbox; Alertmanager/contact-point setup is an OBS-01
follow-up, not this task).

## 9. RPO / RTO — interim targets and what stays open

Built to the INTERIM targets (coordinator decision D3):
**RPO ≤ 24 h, RTO ≤ 4 h** — daily verified dumps + rehearsed
restore runbook. Worst case loses a day of data; recovery is a
documented half-day operation.

OPEN (owner-blocked, not claimed): the spec target **RPO ≤ 15 min /
RTO ≤ 60 min** needs WAL/PITR or a streaming replica plus a standby
host — daily dumps cannot meet it (proven: an order written ~100 ms
after a backup is correctly absent after restore). Step 2
(hourly dumps, RPO ≤ 1 h / RTO ≤ 2 h) is DESIGNED in
`docs/operations/backup-rpo-rto-step2.md`, not implemented.

## 10. Off-site copy (owner-blocked)

`BAK_OFFSITE_DEST` configures the destination (rsync target,
e.g. `user@host:/path`); default OFF. Encryption (G3) is a
prerequisite for activation. Candidates (audit §2): the Chehel
Kaman host (interim, same provider — survives single-host loss,
not provider-wide incidents), a second VPS / object storage from a
different provider (full independence), or explicit risk
acceptance of server-only backups (not recommended for a
money-path platform). Google Drive is EXCLUDED by explicit owner
decision (Nov 16, 2026) — do not propose it. Activation +
destination choice pending the owner (G2).

## 11. Deploy-time checklist (for the deploy coordinator)

1. Ship the scripts: `scripts/backup/*` + the updated
   `scripts/publish-backup-success.sh` + `backup-auto.sh` /
   `backup-daily.sh` wrappers (deploy_remote.py's ship loop does
   this; verify the sha256 match lines).
2. Repoint the 04:41 Tehran cron to the entry point (same
   schedule, same job, new script — D6).
3. Generate the prod key server-side (root-only 0600, §3); confirm
   the owner + deputy hold offline copies.
4. Verify the publisher is present at
   `/opt/drup/scripts/publish-backup-success.sh` (the entry point
   exits 3, not 0, if it is missing).
5. Run the entry point once by hand; confirm exit 0, `BACKUP_OK`,
   and a fresh `backup_signals` row (kind `nightly`).
6. Confirm free disk: the fuller backup set (DB ×2 formats +
   uploads + config + Caddy volumes, encrypted) uses more space
   than the old DB-only dumps; deploy backups are pruned to the
   newest 5.
7. Do NOT create the weekly verification cron or the daily health
   cron — those need owner approval naming the schedule.

## 12. What changed in this task (BAK-01)

- `scripts/backup/backup-entrypoint.sh` (new): the consolidated
  entry point — G1 pipefail/failure propagation, G4 checksums +
  manifest, G6 config + Caddy volumes, G3 openssl encryption
  (required, fails loudly without a key), G9 consolidation, G10
  retention, G5 `verify` (manifest + gzip + `pg_restore --list`),
  G2 configurable off-site copy (default off), G8 failure notice +
  marker fallback.
- `scripts/backup/publish-backup-failure.sh` (new): immediate
  inbox notice + durable marker.
- `scripts/backup/check-backup-health.sh` (new): fallback retry +
  staleness report.
- `scripts/backup/restore-verify.sh` (new): periodic restore
  verification job + JSON report.
- `scripts/backup/test-backup-entrypoint.sh` (new): 57-check
  harness (failure injection, corruption, encryption round-trip,
  retention, umask, wrappers, deploy rehearsal, full restore).
- `scripts/publish-backup-success.sh`: verifies the encrypted
  layout (ciphertext hashes → decrypt → plaintext hashes + gzip)
  as well as the legacy unencrypted layout.
- `backup-daily.sh`, `backup-auto.sh`: thin wrappers exec'ing the
  entry point (no second implementation).
- `scripts/deploy/deploy-backend.sh`: calls the entry point for
  the deploy backup (G5), `umask 077` (G11), exit-3 warn-and-
  continue.
- `scripts/deploy_remote.py`: calls the entry point for the deploy
  backup (G5), saved files become `--include` (rollback paths
  unchanged), ship loop ships all backup scripts, exit-3
  warn-and-continue.
