# Correctness and capacity execution ledger

Source contract: [the supplied 54-task backlog](2026-11-14-correctness-capacity-tasks.txt).
Baseline: `07aff40ffc8914a9deaa153aa7894b9d3fd73400`.
Execution branch: `codex/correctness-capacity`. Implementer: Codex.

## Delivery and dependency rules

- Execute each task's acceptance contract before marking it DONE; push reviewable checkpoints separately.
- Preserve the Go/PostgreSQL/Redis monolith and existing frontend.
- Keep `docs/fa/` and `backend/internal/networkreward/` out of every commit and release.
- Follow the supplied dependency list. Prepare independent work when an environment gate is unavailable.
- Validate, commit and push each completed stage. Production rollout uses `deploy.ps1` after release gates pass.
- Ruling: prepare changes on a dedicated branch in the current checkout, preserving the pre-existing untracked network-reward directory. No production changes are made by this test-only checkpoint.
- Ruling: the supplied backlog already specifies implementation and acceptance, so retain it verbatim instead of producing a second competing plan.
- Ruling: old exact database-name checks are superseded by a common unique-name + server-side marker check; legacy opt-in variable names remain supported. Old unmarked databases are intentionally rejected.
- Shared interfaces: CORE-01 lock order governs all inventory/finance writers; MIG-01 runner is shared by CLI/server/release; AUTH-01 generation invalidation is consumed by AUTH-02/03; FIN-012 monetary units govern FIN-010 conversion. Preserve the integration decisions in the source contract.

## TEST-01 — IN PROGRESS

First checkpoint: remove application database defaults from all PostgreSQL test entry points.

- Added `internal/testdb.Open`: an absent explicit DSN skips before initializing a connection. Malformed URLs, application database names, libpq override parameters and duplicate URL parameters are rejected before connecting. An explicit integration target that is unreachable or lacks the marker fails, rather than silently skipping.
- Before fixture writes, verify `current_database()` and `public.sellersho_test_guard.database_name` against the explicit URL's unique `sellersho_test_` name. Connection cleanup is registered before verification.
- Updated all 47 PostgreSQL test opens in 46 files, including seeder, payment concurrency, capacity, returns and supply-tier fixtures. Removed application configuration loading and hardcoded application targets.
- Added a source guard and real TCP-listener subprocess checks: default opt-out and unsafe explicit targets make zero application endpoint connections. The source regression was executed against the original referral fixture and failed, then passed with the fix.

Verification environment: Windows, local Go toolchain. No application credentials used.

Commands/results:

- `go test ./internal/testdb -count=1`: PASS, including sentinel and unsafe-target checks.
- `go test ./internal/testdb ./internal/database/seeder ./internal/repository ./internal/worker -count=1`: PASS with DB integration explicitly skipped because no disposable DSN exists.
- `go test ./cmd/server -run '^Test.*(SupplyOption|ReferralBonus|PriceIncrease|PriceGov)' -count=1`: PASS (DB-gated tests skip without opt-in).
- `go test ./... -count=1`: FAIL with the five known baseline top-level failures: `TestProposeProductRejectsMissingMeasurements`, `TestCampaignDiscountOnlyFromMargin`, `TestCampaignFixedAndFloor`, `TestCampaignNeverRaisesPrice`, `TestDiscountWithNonZeroPlatformPctFeeUntouched`. Raw local output: `logs/correctness-baseline.log` (ignored).

Remaining gates: executed unmarked-database rejection, migrated disposable database lifecycle and failure cleanup; sentinel application database schema/data proof for the complete default suite; integration/chaos runner target protection. No task is marked DONE on skip-only evidence.

Environment issue: Docker engine reports `Docker Desktop is unable to start`. The user is preparing local Docker. Do not substitute production PostgreSQL for integration verification.

All other 53 tasks remain TODO. No production deployment or capacity certification has occurred.

Checkpoint review: independent reviewer review_test_safety found one incompatible legacy exact-name guard in supplierFlowDB; removed it in favor of the shared verified identity guard. Focused suites rerun. Frontend npm run build PASS. Docker became available after user startup; disposable harness and canonical migration preparation are now running as separate tasks permitted by the supplied execution plan.

## TEST-01 disposable harness stage — acceptance passed

Evidence and exact commands: docs/test-environment.md. Nine Python lifecycle/credential-isolation regressions PASS; live sentinel/default-suite zero connections with unchanged schema/data PASS; unmarked refusal PASS; setup/test-failure cleanup PASS; all ten existing SKU tests PASS; complete schema-only seeder package PASS. Docker now runs locally. Ruling: seeder test uses a schema-only disposable copy to avoid migration demo-user ID collisions; intact append-only ledger must refuse CleanDev/ResetDev, replacing obsolete assertions expecting forbidden truncation. No production seeder changes. Reviewer: mig01 (independent harness review). Strict canonical bootstrap was jointly validated against pending MIG-01, whose separate commit follows; final task closure uses that integrated revision. POSIX wrapper still awaits a POSIX host check, PowerShell wrapper ran successfully.

