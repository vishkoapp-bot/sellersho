# RPO/RTO step 2 — design only, NOT implemented (BAK-01 G7)

Status: DESIGN. Nothing in this document is built, scheduled, or
claimed. It is the follow-on to the BAK-01 interim targets
(RPO ≤ 24 h / RTO ≤ 4 h, daily verified dumps) and the bridge to the
spec target (RPO ≤ 15 min / RTO ≤ 60 min, which needs WAL/PITR or a
replica — see §4).

## 1. Target

- **RPO ≤ 1 h**: at most one hour of orders, payments and ledger
  entries lost on host loss.
- **RTO ≤ 2 h**: a documented restore into a prepared environment
  completes within two hours, measured end to end (not just
  `pg_restore`).

## 2. Proposed mechanism: hourly dumps

- Hourly `pg_dump` (custom format) via the consolidated entry
  point (`backup-entrypoint.sh run hourly` — a new kind, same
  guarantees: pipefail, manifest, encryption, verification,
  success signal). The nightly run stays the daily full set
  (uploads + config + Caddy volumes); the hourly run is DB-only
  plus the manifest, to keep the hourly load small.
- Retention: hourly dumps 48 h local (48 files), nightly 7 days
  local, deploy newest 5 — plus the off-site copy of the nightly
  set (and, once the destination is approved, of the hourly set
  with a shorter off-site retention).
- The hourly cron is a new schedule and needs owner approval
  naming it, like every other cron.

## 3. What must be measured BEFORE committing to it

1. **Hourly dump cost on prod PG**: wall time + CPU/IO impact of
   `pg_dump -Fc` at prod volume, observed over a week (the
   rehearsal set is kilobytes; prod volume is unmeasured). If a
   dump takes > 10 min or visibly disturbs peak traffic, hourly
   dumps are the wrong mechanism — go to §4 instead.
2. **WAL generation rate** (for the §4 decision): `pg_stat_archiver`
   / WAL bytes per hour over a week, to size WAL storage.
3. **Restore time at prod volume**: the weekly restore
   verification (§6 of the runbook) must run against a
   prod-scale restore at least once before the step-2 target is
   declared met — the 38 s rehearsal restore is a lower bound.
4. **Disk**: 48 hourly dumps + 7 nightly sets + 5 deploy backups
   on the same host; confirm headroom (the host was at 79–95%
   disk in Nov 2026; the deploy-backup prune to 5 exists for
   exactly this reason).

## 4. Why this is only step 2 (and what the spec target needs)

Hourly dumps cap the loss at one hour — they cannot reach
RPO ≤ 15 min. The spec target needs continuous WAL archiving
(`archive_mode=on` + `archive_command` shipping WAL segments to
durable off-host storage, or `pg_receivewal` streaming) or a
streaming replica / standby host, plus a tested PITR procedure
(base backup + WAL replay to a point in time) and a failover
runbook. That is infrastructure work (PG config change, WAL
storage, standby host cost + ops, quarterly rehearsal), needs
owner approval + a destination decision, and is NOT in this
document's scope. The interim (24 h / 4 h) and step-2 (1 h / 2 h)
targets are the honest rungs on the way there.

## 5. Acceptance (when step 2 is built)

- An order written 59 minutes before a host loss is recoverable;
  one written 61 minutes after the previous hourly dump is not —
  the boundary is demonstrated, not asserted.
- A full restore from the latest hourly dump into an isolated env
  validates (§6 of the runbook) and completes within 2 h measured
  end to end at prod-scale volume.
- The hourly success signal publishes (`backup_signals`
  kind=`hourly`) and the staleness alert covers it; a missed hourly
  run is visible within the hour, not the next day.
