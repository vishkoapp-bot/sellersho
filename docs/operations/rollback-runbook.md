# Rollback runbook (MIG-02)

A failed deploy never restores the database over newly received
orders. The database is append-only forward: a rollback restores
**code and configuration**; a forward repair lands a **newer
release** that understands the current schema; a database restore
is DR-only (see §5) and never happens silently.

The deploy script (`scripts/deploy_remote.py`) implements this
runbook. This document is the human companion: what the script
decides, why, and what to do when it says `ROLLBACK NEEDS
ATTENTION`.

## 0. The three outcomes

| Outcome | When | What happens to the data |
|---|---|---|
| Compatible-binary restore | The old binary is at/after `lastCompatibleRevision` and every applied migration resolves into its migration set | Nothing is erased. Additive migration data stays. |
| Mode A repair + restore | Same as above, except some migrations this deploy applied are not yet filed in the persistent migration dir | The stranded files are copied in (sha-verified) first; then the restore proceeds. Nothing is erased. |
| Mode B forward repair | The old binary predates `lastCompatibleRevision`, or an applied migration resolves to no file anywhere, or the old release dir is gone | The old binary is NOT restored. The new release stays live (recreated + verified). Nothing is erased. |
| DR restore | Host loss / unrecoverable DB (not a deploy failure) | See §5. OWNER-PENDING (D-6) — no automation exists. |

## 1. Decision tree (what `deploy_remote.py` does)

```
deploy fails
  │
  ├─ read backup/.deployed-commit            → old commit
  ├─ read schema_migrations (fresh)          → applied {version: checksum}
  ├─ read old release dir                    → old binary's migration set
  └─ rollback_precheck()
       │
       ├─ no old commit, or old release dir gone
       │     → verdict "unavailable" → Mode B (§4)
       │
       ├─ old binary predates lastCompatibleRevision
       │     (git merge-base --is-ancestor <floor> <old-commit> fails)
       │     → verdict "incompatible" → Mode B (§4)
       │     NOTE: when the failed deploy applied NO migrations, the
       │     live schema is the one the old binary was already running,
       │     so the floor check does not apply (restore proceeds).
       │
       ├─ an applied version resolves to no file in the persistent
       │  dir, the failed release dir, or the old release dir
       │     → verdict "incompatible" → Mode B (§4)
       │
       ├─ some applied versions are missing from the persistent dir
       │  but exist in a release dir with a matching checksum
       │     → verdict "mode-a" → Mode A (§3) then restore (§2)
       │
       └─ everything resolves and the old binary is at/after the floor
             → verdict "compatible" → restore (§2)
```

The pre-check result is printed (`Rollback pre-check: <verdict> —
<reason>`) and recorded in the deploy backup dir's
`rollback-point.json` and in `/opt/drup/.rollback-points.json`.
When the pre-check itself cannot run (e.g. the DB is unreadable)
the verdict is `incompatible` — a blind restore is never attempted.

## 2. Compatible-binary restore (the normal rollback)

Exact steps (in `deploy_remote.py`, except block):

1. Remove the container migration staging dir.
2. Stop the edge containers when the deploy had started SSL
   (`ssl and changed`).
3. Restore every file in `saved_files` from the deploy backup dir
   (`backend/bin/server-linux`, `nginx.conf`,
   `docker-compose.ssl.yml`, `Caddyfile`, `.deployed-commit`,
   backup scripts, `docker-compose.observability.yml`,
   `docker-compose.metrics-ports.yml`); files that did not exist
   before the deploy are unlinked.
4. Restore the previous collector config tree
   (`backup/ops-observability` → `/opt/drup/ops/observability`).
5. Restore the previous `dist` when it was swapped.
6. Restore the previous Xray config when it was moved.
7. Restore the previous runtime file (`.env.deploy.runtime.json`).
8. `docker compose up -d --no-deps --force-recreate backend
   frontend` (plus `edge` when it was running).
9. `health()` — `/api/health` must report `status: ok`,
   `postgres: up`, `redis: up` (45 × 2 s retries).
10. **Write-path probe** — `python3
    <release>/scripts/release/write_path_probe.py
    --docker-container drup_postgres --expect current`. The probe
    exercises the inventory-balance upsert, a review insert and a
    withdrawal-intent insert inside ONE transaction that is always
    rolled back, then verifies row counts are unchanged and prints
    `PROBE_OK`. `/api/health` passing does NOT prove the write paths
    work — the probe is the gate.
11. Record `rollback-point.json` (`status: rolled-back`,
    `restoredCommit`, `migrationsApplied`, `filesCopied`,
    `compatibility`) and append it to
    `/opt/drup/.rollback-points.json`.
12. Print `Previous code restored; additive migration data retained.
    Database backup was not restored.` — and still exit non-zero, so
    the original deploy failure is reviewed.

Expected output (deploy log tail):

```
Rollback pre-check: compatible — every applied version is present in the old binary migration set and the old binary is at/after lastCompatibleRevision
PROBE_OK expect=current (3/3 write-path checks passed, transaction rolled back, row counts unchanged)
Previous code restored; additive migration data retained. Database backup was not restored.
```

## 3. Mode A repair — applied-but-unfiled migrations

**Symptom:** the deploy applied a migration and then failed
*before* the persistent-dir copy step, so `schema_migrations` has
a row whose file is missing from `/opt/drup/backend/migrations`
(the persistent dir, bind-mounted as `/app/migrations` in the
backend container). The old binary would fail boot with:

```
applied migration <name> missing from required directory
```

(reproduced in rehearsal R-2; the runner's `prepareHistory`
refuses to boot when an applied version has no file in its
migration dir.)

**Repair (automatic, before the old binary is recreated):** for each
stranded file, copy it from the failed release dir (or the old
release dir) into the persistent dir, verifying sha256 against the
checksum recorded in `schema_migrations` (CRLF-normalized, as the
runner hashes it) — and fail the rollback loudly when no source
copy matches. The copied list is recorded in `rollback-point.json`
as `filesCopied` with `repair: mode-a`.

Manual equivalent (only if the scripted path cannot run):

```
# 1. Find the stranded versions (applied, no file in the dir):
docker exec drup_postgres psql -U drup_admin -d drup_db -Atc \
  "SELECT version FROM schema_migrations" | sort > /tmp/applied.txt
ls /opt/drup/backend/migrations/*.up.sql | xargs -n1 basename | sort > /tmp/filed.txt
comm -23 /tmp/applied.txt /tmp/filed.txt
# 2. For each stranded name, copy it from the failed release dir and verify:
cp /opt/drup/releases/<commit>/backend/migrations/<name> /opt/drup/backend/migrations/<name>
#    verify: sha256 of the copied file (CRLF-normalized) == the checksum in schema_migrations
# 3. Recreate the backend container and check health + probe (§2 steps 8–10).
```

## 4. Mode B forward repair — the old binary is refused

**Symptom:** the pre-check verdict is `incompatible` or
`unavailable`. The classic case is a breaking migration such as
`202611110003` (it drops the legacy unique constraint the
pre-`ff57367` binary's `ON CONFLICT (product_id, owner_type,
location_type, location_key, status)` upsert requires): the old
binary would boot and pass `/api/health` yet fail every inventory
write. The probe's `--expect legacy` negative control proves it
(see rehearsal R-3/R-4).

**Repair (automatic):** the old binary is NOT restored. Instead:

1. The persistent dir is re-synced from the new release's manifest
   (copy exactly the manifest files, sha256-verified).
2. The new binary, `dist`, `nginx.conf`, compose files and backup
   scripts are (re-)shipped from the failed release dir.
3. `docker compose up -d --no-deps --force-recreate backend
   frontend` (plus `edge` when it was running).
4. `health()` must pass, then the write-path probe
   (`--expect current`) must print `PROBE_OK`.
5. Record `rollback-point.json` (`status: forward-repaired`,
   `repair: mode-b`, `observabilityReverified: false`) and append it
   to `/opt/drup/.rollback-points.json`; write
   `.deployed-commit` with the new commit (the new release IS what
   is running — the stamp must reflect reality).
6. Print `FORWARD REPAIR SUCCEEDED ...` — and still exit non-zero,
   so the original failure is reviewed.

**Follow-up after any Mode B repair (manual):** verify the
observability stack separately (the repair does not re-verify it):
Prometheus targets up per job, alert rules loaded, `nginx_up == 1`,
a `sellersho_` series returning data, Grafana `/api/health`
(`database: ok`) — the same checks `deploy_observability()` runs.
Then diagnose the original failure from the deploy backup dir
(`rollback-point.json` names the reason and the migrations the
failed deploy applied).

**If the forward repair itself fails** (health or probe still red):
the script prints `ROLLBACK NEEDS ATTENTION` and exits non-zero
with the new release in place but unverified. Do NOT restore the
old binary by hand at this point — it was already proven
incompatible. Escalate to §5 (DR restore) only when the database
itself is unrecoverable; otherwise diagnose from the deploy backup
and repair forward (a minimal hotfix release on top of the failed
one).

## 5. DR restore — OWNER-PENDING (D-6), no automation

> **Status: the DR write-freeze / reconciliation procedure is
> OWNER-PENDING (decision D-6, Nov 20, 2026). No freeze automation
> exists and none may be built until the owner / management approve
> the procedure itself — who may freeze order acceptance, and how
> customers and stores are told. This section describes the
> procedure to be approved; it is NOT an implemented capability.**

A database restore (from a deploy or nightly backup) is DR-only:
host loss or an unrecoverable database. It is never a deploy
rollback (deploys never restore the DB — §0).

Planned procedure (to be approved, then rehearsed):

1. **Freeze order acceptance** — stop new orders/payments at the
   edge (procedure, owner and customer/store communication to be
   approved under D-6). No freeze automation exists today.
2. **Restore to a point** — from the latest verified backup set
   (`scripts/backup/backup-entrypoint.sh verify <backup-dir>`,
   then `scripts/backup/restore-verify.sh`; the manual steps are in
   `docs/operations/backup-runbook.md` §5). DR restore stays in
   the backup runbook; this runbook only decides when it applies.
3. **Reconcile orders / payments / ledger accepted after the
   backup point** — list every order, payment and ledger entry
   written after the backup's timestamp (from the live system
   before the freeze, and from gateway records); re-enter or
   refund each one explicitly; never silently drop accepted data.
   Reconciliation is a manual, audited step — there is no script
   for it until D-6 approves the procedure.
4. **Re-open traffic only after reconciliation** — health +
   write-path probe green, row counts and checksums of the
   reconciled orders verified before/after (§6).

## 6. Never silently erase accepted data

The rule, enforced by the deploy script and by every procedure
above: **a rollback or forward repair never deletes rows accepted
after the deployment.** The check, run before and after any
rollback / forward repair:

```
# Before (record counts + checksums of the accepted set):
docker exec drup_postgres psql -U drup_admin -d drup_db -Atc \
  "SELECT count(*), COALESCE(sum(total_amount),0) FROM orders"
docker exec drup_postgres psql -U drup_admin -d drup_db -Atc \
  "SELECT count(*), COALESCE(sum(amount),0) FROM ledger_entries"
# After the rollback / repair, run the same two queries: both pairs
# must be identical. (Rehearsal R-6 proves this end to end.)
```

The deploy backup dir also carries the evidence for any later
audit: `applied-before.json` (pre-deploy applied set),
`applied-after.json` (post-run applied set),
`migrations-applied.json` (exactly the rows this deploy added),
`rollback-point.json` (commit, floor, migrations applied, files
copied/removed, compatibility verdict, reason, status) and
`backend-environment.json` (the pre-deploy backend environment).
`/opt/drup/.rollback-points.json` is the append-only ledger of
every deploy / rollback / forward repair; pre-`ff57367` points and
any point that cannot be restored are recorded there as
`unavailable` with their reason (rehearsal R-5).

## 7. The rollback floor (`lastCompatibleRevision`)

Each release manifest stamps `migrations.lastCompatibleRevision`
(recomputed per release from `backend/migrations/classification.json`
+ git — currently `ff57367`, 2026-11-11; see
`scripts/release/make_manifest.py`). It is the newest commit that
introduced a migration the classification marks floor-moving
(`breaking` verdict). A binary older than the floor must never be
restored over the new schema. `scripts/release/validate_release.py`
fails a release whose floor is null or is not an ancestor of the
release commit.

## 8. Files

| File | Role |
|---|---|
| `scripts/deploy_remote.py` | The deploy + schema-aware rollback implementation (steps 2–4) |
| `scripts/rollback_points.py` | The append-only ledger (`record` / `list` / `record-unavailable`) |
| `scripts/release/write_path_probe.py` | The write-path readiness probe (deploy + rollback gate) |
| `scripts/release/make_manifest.py` | Builds `release.json` (migration set, floor, timeouts, post-conditions) |
| `scripts/release/validate_release.py` | Release gate (manifest, classification, floor ancestry) |
| `backend/migrations/classification.json` | Per-file class / verdict / post-conditions / timeouts |
| `backend/migrations/README.md` | Authoring rules (three-release staging, lock hygiene, timeouts) |
| `docs/operations/backup-runbook.md` | Backup + DR restore (the DR restore procedure lives there) |
| `~/workspace/mig02-evidence/` | Rehearsal R-1–R-6 evidence (outside the repo) |

## 9. Rehearsal evidence (step 6)

All six scenarios are rehearsed on disposable infrastructure
(fake `/opt/drup` root, fake docker, disposable PG + scratch
Redis, real `deploy_remote.py`); no prod deploy is required to
prove MIG-02. Evidence under `~/workspace/mig02-evidence/`:

- **R-1** fail migration before commit: deploy aborts, no binary
  swap, no persistent-dir change, no `schema_migrations` row for
  the failed file, backup dir with the pre-deploy snapshot.
- **R-2** fail migration after commit (Mode A): negative control
  (old binary fails boot without the fix) + repaired run (old
  binary boots, health + probe pass, repair recorded).
- **R-3** fail backend health after migration (Mode B): rollback
  refuses the pre-floor old binary, forward-repair release healthy
  without re-triggering the stranded file.
- **R-4** overlap-window compatibility matrix (binary × schema).
- **R-5** unavailable rollback points recorded in
  `.rollback-points.json`.
- **R-6** accepted data never erased (row counts + checksums
  before/after).
