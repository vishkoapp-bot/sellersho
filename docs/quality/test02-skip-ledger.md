# TEST-02 skip ledger

Every `t.Skip` observed under the isolated runner
(`~/workspace/.dev/test-isolated-native.sh`, one fresh migrated DB + scratch
Redis per package, all legacy `TEST_URL_KEYS` set). A skip is either
LEGITIMATE (the test cannot run in this harness by design) or MASKING (the
test swallows a fixture/environment failure into a skip — a required DB test
must never do that; see the CI assertion below).

## Legitimate skips (allow-listed in CI)

| Test | Package | Reason |
|---|---|---|
| `TestMigrationProcessHelper` | internal/migrations | Subprocess helper: runs only when invoked as a crash child (`MIGRATION_CRASH_CHILD=1`); skips in a normal run by design. |
| `TestJobsCrashChild` | internal/worker | Subprocess helper: same pattern (crash-child entrypoint); skips in a normal run by design. |
| `TestProcessSIGTERMCheckoutRestartConservation` | cmd/server | Opt-in owned-Linux-process fixture: skips unless `SELLERSHO_LINUX_PROCESS_FIXTURE=1` (the test kills and restarts processes; it cannot run in a shared harness). |

## Masking skips (must be fixed, not allow-listed)

| Test | Package | What it swallows | Status |
|---|---|---|---|
| `TestUpdateSupplyOption_ValidValue` | cmd/server | `openSupplyTierTestDB` calls `t.Skipf("test product could not be created: %v")` when its product INSERT fails — in the baseline the INSERT failed with `pq: column "slug" of relation "products" does not exist`, i.e. a preceding fixture rebuilt `products` without `slug`. The skip hides a shared-DB pollution failure. | OPEN (F-UP-3): add the insert columns (`slug`, `title`, `category_slug`, `supplier_id`, `original_price`, `supplier_base_cost`, `platform_margin`, `stock`) to the preceding `products` rebuild — prime suspect `cmd/server/supplier_warehouse_orders_test.go` — or make the fixture fail loudly instead of skipping. Verify against the round-3 log. |
| `TestUpdateSupplyOption_InvalidValue` | cmd/server | Same as above. | OPEN (F-UP-3, same fix). |

## Env-gated skips that did NOT fire under the isolated runner

149 `t.Skip`/`t.Skipf` call sites exist in Go tests, mostly of the form
"`*_DATABASE_URL` not set" / "database unavailable". Under the isolated
runner every legacy URL key is set, so none of them fired in the baseline:
the full per-package baseline shows exactly the 5 skips above (3 cmd/server,
1 migrations, 1 worker). Any `*_DATABASE_URL not set` skip in CI means the
runner failed to provision that key — treat as a CI failure, not a pass.

## CI assertion

The `go-integration` job in `.github/workflows/ci.yml` fails the pipeline
if any required-DB test skips: it greps the per-package logs for `^--- SKIP`
and allows ONLY the three legitimate tests listed above. A new skip (or a
masking skip that reappears) fails the job until the fixture is repaired or
the ledger + allow-list are updated with a recorded reason.

## Counts

- Baseline (2026-11-19, frozen audit): 5 skips — cmd/server 3, migrations 1, worker 1.
- Round-3 verification (2026-11-19): cmd/server 3 skips — the SAME 3 as baseline
  (SIGTERM legitimate + UpdateSupplyOption x2 masking, F-UP-3 still open);
  repository 0 skips in the 9m-truncated run (long 25m run pending — counts
  to be re-checked there; the 10 SKU cases never ran in the truncated run).
