# Migration interface for MIG-02 (G12 — REL-01 defines, does NOT implement)

REL-01 deliberately does **not** implement migration forward repair.
This document is the interface MIG-02 consumes, so the boundary is
explicit and the REL-01 tooling already produces everything MIG-02
needs.

## What REL-01 provides

1. **Manifest migration set** — `release.json` → `migrations`:
   ```json
   {"files": [{"name": "<version>_<name>.up.sql", "sha256": "<hex>"}],
    "count": 210,
    "lastCompatibleRevision": null}
   ```
   The file list + per-file sha256 are recomputed and verified by
   `validate_release.py` against the staged `backend/migrations/`.
   The set is immutable per release: it is the exact set that was
   built and validated.

2. **Pending enumeration** — `validate_release.py --db-url <url>`
   (read-only) runs the same `classify_history` rules as
   `deploy_remote.py`: unknown applied history fails; unreviewed
   historical pending (before `202611090001`, outside the reviewed
   set `202611050001_customer_store_links.up.sql`) fails the release.
   The smoke suite (`smoke_release.py`) applies the full staged set
   to a disposable database with the staged `migrate-linux` and
   fails if any migration errors.

3. **Failure semantics preserved** — a failed migration aborts the
   release before any traffic switch; the database is deliberately
   NOT restored over new orders (the `deploy_remote.py` design
   choice). REL-01 adds no DB-restore step.

## What MIG-02 must define (not done here)

- **`lastCompatibleRevision`**: the last app revision whose binary
  is compatible with this release's schema. REL-01 records the
  field as `null`. MIG-02 fills it (per release) and the validator
  must then enforce it at rollback time.
- **Migration classification**: expand / backfill / contract stages
  for each migration, so a rollback can tell whether the previous
  binary is schema-compatible.
- **Failed-migration forward repair**: what happens to a partially
  applied migration file on rollback (today: it stays, and the
  restored binary may retry it). The procedure, the "last
  compatible revision" record, and the stranded-file handling
  belong to MIG-02.
- **Backfill resumability, lock/statement timeouts, data
  validation** for the staged migrations.

## Invariants MIG-02 must preserve

- The manifest migration set stays the single source of truth for
  what a release contains; MIG-02 must not introduce a second list.
- Validation stays fail-closed: any migration state the classifier
  cannot explain fails the release.
- Data accepted after a deployment is never silently erased by a
  rollback (no blind restore of an old DB backup over new orders).
