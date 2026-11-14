# Canonical migrations

The CLI, HTTP startup and disposable test harness use `migrations.Run`. HTTP
startup waits on the same database advisory lock as the CLI and aborts before
serving if migration verification fails. A release should run the CLI before
starting replicas. All callers must use a direct PostgreSQL connection or session
pooling; transaction-pooling proxies cannot preserve a session advisory lock.

From `backend/`, with `DATABASE_URL` already supplied through the protected runtime
environment, run:

```
go run ./cmd/migrate -dir migrations
```

The full filename is the immutable version, ordered lexicographically. Some
historical migrations share their numeric prefix, so the prefix alone is not an
identity. Never rename, delete, edit or insert a file before the applied maximum;
append a new migration instead. SHA-256 covers the entire SQL file after CRLF-to-LF
normalization for Git's Windows line-ending conversion. Other changes, including
comments, constitute drift. Unknown applied files and changed checksums abort
before applying any pending SQL. Existing business tables without a migration
ledger require reconciliation; they cannot be implicitly treated as a fresh DB.

Each SQL file and its ledger insert commit in one transaction on the locked
connection. Failure leaves earlier successful files applied and rolls back the
failed file. Retry resumes at the first unapplied file. The command exits nonzero
and emits no success message on failure. Session death releases the advisory lock;
PostgreSQL rolls back an open transaction. Server-side statement, DDL lock and idle
transaction timeouts bound abandoned work even when a client is killed.

Defaults: overall CLI/startup deadline 10 minutes, singleton lock wait 30 seconds,
each migration/history transaction 2 minutes. The CLI exposes `-timeout`,
`-lock-timeout` and `-migration-timeout`. Each file logs start, elapsed duration and
success/failure without logging its SQL or connection URL.

## Legacy history adoption

Old server releases recorded only `version` and `applied_at`. Their SQL bytes
cannot be recovered from that table. Adoption is therefore an explicit release
operation, never an automatic HTTP-startup action:

1. Preserve the production backup and ledger. Verify its applied filenames against
   the deployed historical release and review any historical manual DDL. A checksum
   manifest proves the supplied files match reviewed source, not that a legacy
   database was never modified outside migrations.
2. Compare the deployment files with `historical-checksums.json`, pinned to the
   179 committed migrations at `1b988ac6ad2afc9e63b20b260639faf9c0ebdbfd`.
   The manifest is embedded in the runner; it is not regenerated at runtime.
3. Run `go run ./cmd/migrate -dir migrations -adopt-legacy` with the protected
   runtime environment. Under the same singleton lock, the runner verifies *all*
   recorded filenames, ordering and existing checksums; every checksumless row
   must exactly match the embedded historical manifest. Only then does one
   transaction populate missing checksums and enforce `NOT NULL`. Pending SQL
   executes normally afterward.
4. Re-run without `-adopt-legacy` and start replicas. Any missing/unknown/modified
   legacy file requires investigation and a reviewed reconciliation; do not edit
   the manifest merely to suppress a refusal.

## Transaction boundaries and nontransactional DDL

All 179 historical files support transactional execution. Top-level transaction
control is forbidden, including `COMMIT` embedded after other statements;
transaction words inside DO/function bodies, comments and strings are allowed.
The runner sets `standard_conforming_strings=on` to match its SQL boundary lexer.

Nontransactional DDL such as `CREATE INDEX CONCURRENTLY` is currently refused by
PostgreSQL inside the runner transaction. The explicit declaration
`-- migrate:transaction off` is also rejected during preflight. There is no automatic
retry outside a transaction. Before introducing such a migration, add a reviewed
nontransactional protocol with persistent dirty-state recording, interrupted-index
inspection and an explicit repair procedure; otherwise a crash could leave DDL
applied without an applied ledger row. No existing release requires this mode.

## Verification and lock review

From the repository root:

```
python scripts/test-isolated.py -- go test ./internal/migrations -count=1 -v
```

The harness boots all 179 files through the canonical CLI. Tests additionally
cover immutable history, a historical 178-to-179 legacy upgrade preserving a
business row, two simultaneous runners, failed SQL rollback/retry, timeouts,
missing directories/applied files, backdated files, untracked schemas, explicit
adoption with drift refusal, transaction escapes and actual process termination.

Fresh empty-database timings are not production lock estimates. Review pending
`ALTER TABLE`, ordinary `CREATE INDEX` and backfill `UPDATE` statements against
production row counts and active transactions before releasing. Historical
`000024_inventory_returns_carriers_dimensions`, `202610160006_sku_returns`,
`202610280009_tariff_return_pricing`, `202610310004_product_reviews_legacy_shape`,
`202611110002_product_model_v2` and `202611110003_sku_inventory` combine schema
changes/backfills and merit particular scrutiny when upgrading an older database.
Their normal indexes and constraint changes can block writers. Migrations that
are already recorded are verified but never rerun. A production-sized upgrade
rehearsal and lock observation remain a release gate, separate from empty bootstrap.
