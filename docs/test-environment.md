# TEST-01 disposable test environment evidence

Validated on Windows, 14 November 2026, with Docker Server 29.7.2 and
`postgres:16-alpine`. Fixtures contain generated/test data only. PostgreSQL uses a
unique container, a unique `sellersho_test_*` database, loopback-only random port,
tmpfs data, and a `NOSUPERUSER NOCREATEDB NOCREATEROLE` database owner. No existing
application database or credentials are used. All commands below run from the
repository root. Audit reproduction scripts remain unchanged.

## Checks and results

```powershell
python scripts/test_isolated_runner_test.py
```

PASS: nine regression tests cover replaced/mislabelled/missing container refusal,
setup and test failure cleanup, cleanup failure propagation, inherited external
database/provider credential removal, and explicit schema-only opt-in.

```powershell
python scripts/test_isolated_acceptance.py
```

PASS (runner exit 0): reachable sentinel supplied through `DATABASE_URL`, all
integration URL variables absent, then `go test ./... -count=1` made zero sentinel
database connections and left its schema/data unchanged. A correctly named but
unmarked disposable database was refused before fixture writes by
`TestConcurrentPaymentCallbacksConsumeInventoryOnce`. Actual bootstrap failure
and a nonzero child command both removed their verified disposable containers.
The expected unmarked-target rejection is printed as a failing Go test, and the
acceptance driver requires that failure.

The default whole Go suite itself exited 1 with these five existing failures;
this evidence does not claim a green application baseline:

- `TestProposeProductRejectsMissingMeasurements` (three missing-measurement cases)
- `TestCampaignDiscountOnlyFromMargin`
- `TestCampaignFixedAndFloor`
- `TestCampaignNeverRaisesPrice`
- `TestDiscountWithNonZeroPlatformPctFeeUntouched`

```powershell
python scripts/test-isolated.py -- go test ./internal/repository -run '^(TestAuditFinance|TestAuditSupply|TestAuditOrder|TestSKUInventory)' -count=1 -v
```

PASS: the canonical CLI applied all 179 current migrations; all ten matching
`TestSKUInventory*` regressions ran and passed, followed by verified container
cleanup. The `TestAudit*` alternatives matched no additional tests. Existing
notification warnings about minimal fixture columns appeared in the callback
test; they did not fail its assertions.

```powershell
.\scripts\test-isolated.ps1 -- go test ./internal/testdb -run '^TestDisposableIdentity$' -count=1
python scripts/test-isolated.py --schema-only -- go test ./internal/database/seeder -count=1 -v
```

PASS: PowerShell forwarding, canonical migration bootstrap and cleanup. PASS:
complete seeder package using an empty copy of the latest schema, verified marker
and non-superuser role, `SeedDev` twice with populated/idempotent products and
stores, and `CleanDev`/`ResetDev` refusal by the intact append-only ledger trigger.
All public table counts were unchanged after each refused operation. Final
container cleanup was verified. The POSIX wrapper has not been executed on this
Windows host.

The earlier normal migrated seeder probe deliberately exposed historical fixed
ID conflicts: migration `000008_warehouse_admin_finance_lifecycle` uses IDs ending
008/009 for seller/customer accounts, while `GetDevUsers` reuses those IDs for
support/supplier accounts under different mobiles. Its mobile-only upsert fails
on `users_pkey`. This was resolved in the test fixture contract using
`--schema-only`; production seeder logic and ledger protections are unchanged.
The old lifecycle test also expected forbidden ledger truncation to succeed;
its assertions now verify refusal and preservation instead.

## Local raw logs

These review logs contain redacted process output and remain outside committed
source under `C:\Users\LENOVO\AppData\Local\Temp\`:

- `sellersho-test01-acceptance-stable.log`: sentinel, unmarked refusal, failure cleanup
- `sellersho-test01-integration.log`: canonical bootstrap and ten SKU regressions
- `sellersho-test01-wrapper.log`: PowerShell wrapper smoke
- `sellersho-test01-seeder-schema-only.log`: complete isolated seeder package
- `sellersho-test01-seeder.log`: original migrated-data seeder collision

No labeled disposable containers remained after these checks. The harness does
not attempt to certify capacity, production migration lock durations, historical
data reconciliation, or unrelated integration/business regressions.
# Redis isolation follow-up

Redis integration uses `internal/testredis.Open` and the runner-provisioned
`SELLERSHO_TEST_REDIS_URL` / `SELLERSHO_TEST_REDIS_TOKEN`. Do not supply application
cache credentials. The guard accepts only the dedicated loopback DB15 endpoint
with generated credentials and verifies the server marker before any test write.
Absent opt-in skips; explicit invalid/unreachable/unmarked targets fail. Both
Python runners provision authenticated Redis in tmpfs and perform identity-checked
cleanup alongside PostgreSQL. The per-test runner verifies and resets only its
owned cache DB15 between tests. The supplier OTP test seeds codes directly and
never sends SMS. See [combined evidence](quality/test-suite-isolation.md).

