# Complete integration suite isolation

The shared-database command reproduced all 109 original top-level failures on
2026-11-14. Older fixtures execute destructive DDL in public and leave truncated
table definitions for later tests. Serial package execution (`-p 1`) cannot
isolate tests within a package.

Run `python scripts/test-suite-isolated.py` from the repository root. It compiles
each package once and discovers every top-level Test function, then runs each
against a separate database cloned from the canonical migrated template. Seeder
tests receive the existing schema-only template with triggers preserved. Each
child retains the NOCREATEDB/NOSUPERUSER test role, independently verified marker,
and scrubbed provider environment. Only the container-owning runner creates and
drops clones. Existing schema-scoped fixtures and subtests keep their normal
relationships inside that test's database. Binaries run in their package directory
so relative migration/testdata paths match `go test`.

Use `--run REGEX` and package paths for focused reproduction. Failure names and
the aggregate result are retained; failed tests remain failed. Default helper or
opt-in skips emitted by test binaries remain visible. A full passing result is
not claimed while the complete isolated run and domain triage are outstanding.

Initial verification: the destructive `TestFinanceAdjustmentConcurrentReplay`
and `TestLegacyAliasAdoptionPreservesLedgerRows` both passed against separate
clones with verified container cleanup. The complete migrated suite is running
separately; remaining failures will be fixed without removing their assertions.

## First complete isolated baseline

The completed run discovered 565 top-level tests and reported 72 failures. Of the
109 shared-database failures, 43 no longer failed and 66 still failed. Six further
independent failures became visible after removing order-dependent contamination.
This is a failing baseline, including visible helper/opt-in skips, not 493 passing
integration gates. Local complete output: `logs/test02-per-test-full.log`.

## Supplier registration/access fixtures

Registration has activated supplier accounts immediately since the existing
October policy. Fixtures incorrectly attempted a second pending-only approval;
the email round-trip test also expected `pending_approval`. Fixtures now assert
the returned approved supplier and active owner, use valid numeric mobile numbers
for team members, and consume the migrated schema without rerunning historical
DDL. Seven admin queue/access/ownership/QC metadata/tracking-validation/team tests
passed in separate clones; the registration email/null round-trip passed too.
Remaining publication and inbound lifecycle failures retain their assertions and
are tracked separately. Logs: `logs/test02-supplier-access-green.log` and
`logs/test02-registration-green.log`.
