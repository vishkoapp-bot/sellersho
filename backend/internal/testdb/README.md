# Disposable PostgreSQL tests

All database fixtures use `testdb.Open(t, dsn)` rather than application configuration.
Pure unit tests require no DSN. Existing suite-specific opt-in environment variables
remain available. Formerly implicit fixtures use `SELLERSHO_TEST_DATABASE_URL`.

An accepted URL must explicitly name the host and a database matching
`sellersho_test_[a-z0-9_]+`. Use a fresh random suffix for each run. Only `sslmode`,
`connect_timeout`, `search_path`, and `application_name` URL parameters are accepted.
Do not point any test at an application database, even if its name contains `test`.

The disposable-environment provisioner must create this marker in the fresh database,
before exposing its DSN to tests:

```sql
CREATE TABLE public.sellersho_test_guard (
    singleton BOOLEAN PRIMARY KEY CHECK (singleton),
    database_name TEXT NOT NULL
);
INSERT INTO public.sellersho_test_guard VALUES (TRUE, current_database());
```

Never add this marker to an existing application database. It is deliberately not
part of application migrations. Test roles need only access to their disposable
database. Retain no production data or credentials in fixtures. Keep application
DSNs absent from integration processes, and refuse unmarked targets before DDL,
seeding, truncation, or cleanup. Cleanup must verify the resolved target identity.

From the repository root, provision a disposable database and run a command:

```powershell
python scripts/test-isolated.py -- go test ./internal/repository -run '^TestSKUInventory' -count=1 -v
.\scripts\test-isolated.ps1 -- go test ./internal/testdb -count=1
```

The POSIX wrapper is `sh scripts/test-isolated.sh -- go test ./internal/testdb
-count=1`. Python 3, Docker and Go must be installed. The runner creates a random
container/database and a `NOSUPERUSER NOCREATEDB NOCREATEROLE` test role. PostgreSQL
binds only a random IPv4 loopback port and stores data in tmpfs. There are no host
mounts, external DSN inputs or production credentials. It runs the canonical
`go run ./cmd/migrate -dir migrations`, then runs the requested command from
`backend/`, supplying all existing suite-specific test URL variables through the
child environment. Credentials and complete URLs are redacted from child output.

The database lives for the child command's lifetime, which also supports custom
fixture probes. Bootstrap failures, test failures and handled interruption all
perform cleanup after checking the container's resolved ID, random owner label
and name. Cleanup failures remain failures. A host crash or SIGKILL cannot execute
cleanup; inspect the printed unique container name and its `sellersho.test-run`
label before removing that container. Never use broad Docker prune operations.

Seeder lifecycle fixtures need an empty copy of the current migrated schema:

```powershell
python scripts/test-isolated.py --schema-only -- go test ./internal/database/seeder -count=1 -v
```

Migration demo accounts conflict with the development seeder's historical fixed
IDs. `--schema-only` restores a schema-only dump into a second random disposable
database, preserving every trigger and setting `SELLERSHO_TEST_SCHEMA_ONLY=1` only
for that child. The seeder test verifies its non-superuser role, repeated seeding
and immutable-ledger refusal of `CleanDev`/`ResetDev`, with every table count
unchanged. Those operations include a forbidden ledger TRUNCATE; the previous
test's successful-cleanup expectation was stale. No production protection is
disabled. A configured integration run without the required empty-schema mode
fails with the invocation instruction. Whole-database disposal provides cleanup.

See [TEST-01 evidence](../../../docs/test-environment.md) for the exercised safety
checks and the five existing unit-test failures. This is not a capacity harness
or a claim that the full application test baseline is green.
