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

## MIG-01 — DONE

Shared runner now serves CLI and HTTP startup. Dedicated session advisory lock, ordered filename identities, immutable SHA256 history, explicit reviewed legacy adoption, transactional DDL plus ledger record, bounded deadlines and nonzero failure are implemented. All179 migration bootstrap, 178-to179 upgrade preserving data, concurrent runners, injected failure/retry, drift/missing-dir/missing-file, timeout and killed-child rollback/unlock tests PASS. Reviewer test01_runner found a SQL-string transaction-boundary escape; fixed with standard-conforming/E-string-aware lexer and regressions. Dollar-sign identifiers also have regression coverage. Nontransactional mode explicitly refused (none required by existing179); production-scale rehearsal and legacy adoption remain final release gates. Exact checks and operational contract: backend/internal/migrations/README.md. This integrated migration stage closes TEST-01's canonical-bootstrap dependency; TEST-01 is DONE with the evidence in docs/test-environment.md. Baseline5 application failures remain for owning tasks.

## CORE-01 — DONE

Global row-lock contract and caller-owned lock-plan helper added, with writer map292 repository symbols/41files plus43 direct HTTP/worker SQL writers and6Redis mutators. Existing inverted callers are explicitly assigned to INV/FIN/JOB owners; this does not claim repaired domain flows. Root review caught nonexistent future placement relation in helper; removed it from executable allowlist and added actual migrated-catalog validation for every allowed table/PK plus regression refusing future placement name. Typed account guards use stores, suppliers and users rows; every monetary authorizer must adopt the same contract. Command: python scripts/test-isolated.py -- go test ./internal/repository -run '^TestTransaction(LockOrder|Contract)' -count=3 -v => PASS14.865s, no skips,18 two-connection flow runs, bounded lock waits, rollback/retry and independent-account concurrency, verified cleanup. Reviewed root independently of implementer core01. No historical data writes. Contract and detailed evidence: docs/architecture/transaction-contract.md.

## INV-01 — stock and dispatch gate DONE; financial/history release gates retained

Original six customer/admin mixed/two-supplier/legacy cases reproduced cancellation restoring dispatched stock7to10. Shared aggregate-order lock now serializes cancellation and all dispatch writers; any dispatched supplier leg/warehouse custody evidence refuses whole-order cancellation with typed409.100supplier and100warehouse races PASS; replay restores once, partial-return captures fail atomically. Independent reviewer review_test_safety found2issues: directsql.Open fixtures bypassed TEST01, and audit TEXT/UUID join failed42883. Both fixed and regressions executed. Final guarded command includes TestEveryDatabaseTestUsesGuard, TestCancellationAuditReportMigratedSchema, six audit cases, races/replay/alternatewriters/carrierlegacy/API409 tests => PASS (testdb2.091s repo34.344s server0.681s), verifiedcleanup. Full delivered read-only audit SQL executed on actual migrated schema seeded with two anomalies andcontrol; expected itemized rows returned and before/after JSON state unchanged. Raw local evidence inv01-review-fixes.log. Remaining production historical report execution belongs final FIN014/release rehearsal; no production stock correction performed. FIN003 still must atomically record paid-cancellation refund liability before release. Existing payment fixture missing payment_expires_at is recorded for TEST02, not silently fixed in this task.

## AUTH-01 / AUTH-02 — DONE (ship with AUTH-03)

Shared all-role/capability logout clears credentials and role sessions synchronously, broadcasts via storage deletion; public per-store guest product carts intentionally retained consistently in memory/storage. Authenticate owns generation+expectedcredential installation; all5real panel entry paths adopted guard, later profile/store success+failure checked. API401 includes generation check; current expiry signals commonlogout and completes readiness; obsolete401 during newpendinglogin cannot clearB. Mounted regression first reproduced3rolelogout failures, stale refresh/restore corruption and actual401readiness failure. Reviewercore01 caught direct panel bypass/current401/readiness supersession; fixed with dedicated scenarios and productioncaller adoption. Final browser+existing auth/routing+entrypointcontract41tests PASS0skips; npm runbuild PASS. Includes late supplierprofile success/error, raw stale setter rejection, currentrefresh replacingrestore, two refresh ordering, unmount,two-tabs,reload,real subsequent headerabsence. Details docs/auth-session-lifecycle.md. AUTH03 privatecache isolation remains separategate; no serverJWT revocation or real-provider certification claimed. Panels unchanged money-preview hunks staged separately forFIN012.

## AUTH-03 — DONE

Verified immutable account/tenant boundary resets private Order/Store/UI caches; asynccredentialgeneration guards preventoldpayloads. Backend alreadyselectsclaims.UserID, UI fullname/recipientphone ownershipfilterremoved. Privatewishlists/supplierdraftscoped, legacyunownedlocalcatalogauthorityremoved; publicguestcartsretained. Independentreviewreview_auth03 reproducedpendingcreate/confirm/dispute busyflagsstuckafterrejectedreplacementlogin (sameA); fixedper-mountedoperationownershipwhilepreservingprivateguards andneweroperationbusy.26mountedbrowsercasesPASS+3existingProductContexttestsPASS+tscPASS. ActualSellerOTPguardnavigation passeswhileinputpreservedduringpendingauth.1000canaryidentitiesdefined10stores,20sequentialbrowseridentitiesexercised; NOT1000activecapacityproof. UserexplicitlychooselocaltestsandkeepfinalhardwarecapacitygateOPEN. Details docs/auth-cache-isolation.md. Unknownlegacycacheownershipnotguessed; no backendmoney/orderownershipruleschanged.

