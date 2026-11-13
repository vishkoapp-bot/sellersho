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

Integration runner provisioning, canonical migration bootstrap and failure-cleanup
rehearsal remain pending TEST-01 acceptance; this helper is not a capacity harness.
