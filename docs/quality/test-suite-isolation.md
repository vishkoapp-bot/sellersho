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
