# Migration authoring rules (MIG-02)

Every `*.up.sql` file in this directory is an immutable, append-only
schema step. The full filename is the version, ordered
lexicographically; never rename, delete, edit or insert a file before
the applied maximum — append a new migration instead. SHA-256 covers
the entire SQL file after CRLF-to-LF normalization. `.down.sql` files
are never executed by any tooling; they exist only as documentation
for disposable databases.

## Classification (step 1)

Every file must have an entry in `classification.json` (the source of
truth; `MIGRATION_CLASSIFICATION.md` is generated from it — see
`scripts/release/generate_classification_md.py`). The class is one of
EXPAND (additive only), BACKFILL (data movement into new structures),
CONTRACT (tightens what old code may write) or DESTRUCTIVE (drops data
or objects). The verdict is one of `compatible` (the old binary is
unaffected), `tightening` (an old-binary write can fail only on a
violating value, fail-closed) or `breaking` (a normal old-binary write
path fails structurally — this moves the rollback floor
`lastCompatibleRevision`). `scripts/release/validate_release.py`
fails the release on any unclassified file, any sha256 mismatch, or
any drift between the JSON and the generated `.md`.

## Three-release staging (D-4 — process rule, no runner change)

Schema changes that remove or tighten anything an old binary may
still write go out in three releases, never one:

1. **Expand** — add the new column / index / constraint as nullable,
   `NOT VALID`, or unused (old and new binaries both work).
2. **Backfill** — populate the new structure (idempotent,
   re-runnable; see below) and let the old binary keep writing the
   old shape.
3. **Contract** — set `NOT NULL`, `VALIDATE CONSTRAINT`, drop the old
   column / constraint / trigger, only after every binary at or after
   the floor understands the new shape.

A file that combines expand + backfill + contract in one step is
rejected in review unless the table is provably tiny. The chunked
multi-transaction runner change was considered and NOT built (D-4);
staging across releases is the rule instead.

## Backfills

- A backfill must be **idempotent and re-runnable**: running it twice
  (or resuming it after a killed `migrate`) is a no-op for already
  moved rows. Prefer `WHERE <key> IS NULL` / `ON CONFLICT DO NOTHING`
  shapes over blind `INSERT ... SELECT`.
- A backfill expected to touch more than ~10k rows or run longer than
  ~30 s on production-shaped data must be split into its own file
  (the expand step) so it can be measured and, if needed, re-staged —
  never hide a large backfill inside a schema-change file.
- Every BACKFILL file carries **post-conditions** in
  `classification.json` (copied into the release manifest by
  `make_manifest.py`; the deploy enforces them for exactly the
  migrations it applies and fails the deploy on any mismatch).
  Typical shapes: `SELECT count(*) FROM t WHERE new_col IS NULL`
  must be 0; row counts before/after for `DELETE`s. The two
  data-`DELETE` files and the three `SET NOT NULL` files have
  post-conditions first — see their classification entries.

## Timeouts (D-5)

The runner splits the old single per-migration timeout into a
**lock timeout** (default 30 s: how long a migration waits for the
DDL/data locks it needs) and a **statement timeout** (default 10 min:
how long a statement may run). The overall 10-minute deadline is a
deploy-level budget, not a per-file one. A file that needs more than
the defaults declares it in `classification.json`
(`timeout_lock_seconds` / `timeout_statement_seconds`); the manifest
carries it in `migrations.timeouts` and `deploy_remote.py` passes the
manifest to `migrate` with `-manifest`. No file currently needs more
than the defaults (slowest measured file on disposable data:
`202611200002_perf02_bounding_indexes`, 562 ms; full set 26.3 s).
Do NOT declare timeouts in the `.up.sql` file itself (no file-level
annotations); overrides live in the manifest only.

## Lock hygiene

- `CREATE INDEX` on a large table takes a SHARE lock (writers block
  for the build). Assess the lock time against production row counts
  and active transactions before releasing; a production-sized
  upgrade rehearsal and lock observation remain a release gate.
  `CREATE INDEX CONCURRENTLY` cannot run inside the runner's
  transaction and is refused — introducing it needs a reviewed
  nontransactional protocol first (see
  `backend/internal/migrations/README.md`).
- `CHECK` additions on large tables must use `NOT VALID` +
  `VALIDATE CONSTRAINT` staging (two files: expand, then contract)
  so the validation scan does not hold the table lock for the whole
  check.
- `SET NOT NULL` / `ADD CONSTRAINT` / `DROP` on a hot table needs a
  data-impact note in the classification entry (which rows are
  affected, what an old-binary write does afterwards).

## Rollback floor

The release manifest stamps `migrations.lastCompatibleRevision`
(recomputed per release from the classification + git — currently
`ff57367`, 2026-11-11; see `scripts/release/make_manifest.py`).
A binary older than the floor must never be restored over the new
schema: `deploy_remote.py` refuses the blind restore and goes to
forward repair instead. The DR write-freeze/reconciliation procedure
is OWNER-PENDING (D-6) and no freeze automation exists — see
`docs/operations/rollback-runbook.md`.

## Verification

- `python3 scripts/release/check_classification.py` — standalone
  classification check (also enforced by `validate_release.py`).
- `python3 scripts/release/validate_release.py --stage <dir>
  --commit <sha>` — full release gate (manifest, contract,
  classification, floor ancestry).
- `python3 scripts/release/write_path_probe.py --db-url <url>` —
  write-path readiness probe (inventory upsert, review insert,
  withdrawal-intent insert) inside one rolled-back transaction.
- Disposable-DB tests: `~/workspace/.dev/test-isolated-native.sh --
  go test ./internal/migrations -count=1 -v`.
