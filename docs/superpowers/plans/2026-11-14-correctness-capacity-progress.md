# Correctness and capacity execution ledger

Source contract: [the supplied 54-task backlog](2026-11-14-correctness-capacity-tasks.txt).
Baseline: `07aff40ffc8914a9deaa153aa7894b9d3fd73400`.
Execution branch: `codex/correctness-capacity`. Implementer: Codex.

## Current task status

DONE refers to the task-specific local gate described below. It does not certify production history, payment-provider operation, deployment or hardware capacity. Checkpoint OPEN means required acceptance is still outstanding. The original supplied backlog remains verbatim.

| Task | Status | Commit | Evidence |
| --- | --- | --- | --- |
| TEST-01 | DONE | 1b988ac / 7cb9b77 | [Test environment](../../test-environment.md) |
| CORE-01 | DONE | cf69d11 | [Transaction contract](../../architecture/transaction-contract.md) |
| INV-01 | DONE | 45c1bf1 | [Read-only audit](../../../scripts/audit-cancelled-dispatched.sql) |
| MIG-01 | DONE | 9093c5f | [Migration contract](../../../backend/internal/migrations/README.md) |
| INV-05 | DONE | efb37fc | [Origin contract](../../architecture/inventory-origins.md) |
| INV-11 | DONE | b402f80 | [Carton contract](../../architecture/carton-creation.md) |
| INV-02 | TODO | — | — |
| INV-03 | TODO | — | — |
| INV-04 | TODO | — | — |
| INV-08 | TODO | — | — |
| INV-09 | TODO | — | — |
| INV-06 | TODO | — | — |
| INV-07 | TODO | — | — |
| FIN-012 | DONE | bf7d1bd | [Money policy](../../architecture/money-policy.md) |
| FIN-001 | IN PROGRESS | — | Implementation and verification in progress |
| FIN-006 | TODO | — | — |
| INV-10 | TODO | — | — |
| OBS-01 | Checkpoint OPEN | 3983baa | [Telemetry gates](../../operations/observability.md) |
| CAP-01 | IN PROGRESS | — | Implementation and verification in progress |
| FIN-003 | TODO | — | — |
| AUTH-01 | DONE | e0c25ca | [Session lifecycle](../../auth-session-lifecycle.md) |
| AUTH-02 | DONE | e0c25ca | [Session lifecycle](../../auth-session-lifecycle.md) |
| AUTH-03 | DONE | dd373f5 | [Cache isolation](../../auth-cache-isolation.md) |
| CHECKOUT-01 | TODO | — | — |
| OPS-02 | TODO | — | — |
| FIN-002 | TODO | — | — |
| JOB-01 | TODO | — | — |
| SEC-01 | Checkpoint OPEN | 409dc02 / 03a5051 | [Credential gates](../../operations/credential-lifecycle.md) |
| FIN-010 | TODO | — | — |
| INV-12 | TODO | — | — |
| FIN-004 | TODO | — | — |
| FIN-005 | TODO | — | — |
| FIN-007 | TODO | — | — |
| FIN-008 | TODO | — | — |
| FIN-011 | TODO | — | — |
| FIN-009 | TODO | — | — |
| FIN-013 | TODO | — | — |
| CHECKOUT-02 | TODO | — | — |
| TEST-02 | Checkpoint OPEN | Unit fixture checkpoint | [V2 measurement fixture](../../../backend/cmd/server/supplier_product_gaps_test.go) |
| OPS-01 | TODO | — | — |
| REL-01 | TODO | — | — |
| BAK-01 | TODO | — | — |
| MIG-02 | TODO | — | — |
| FIN-014 | TODO | — | — |
| CHECKOUT-03 | TODO | — | — |
| PERF-01 | TODO | — | — |
| PERF-02 | TODO | — | — |
| PERF-03 | TODO | — | — |
| PERF-04 | TODO | — | — |
| EDGE-01 | TODO | — | — |
| REL-02 | TODO | — | — |
| QUAL-01 | TODO | — | — |
| CAP-02 | TODO | — | — |
| GO-01 | TODO | — | — |

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

## TEST-01 — initial checkpoint (historically IN PROGRESS)

The first checkpoint removed application database defaults from every PostgreSQL test entry point. `internal/testdb.Open` skips an absent explicit DSN before opening a connection. It rejects malformed URLs, application database names, libpq override parameters and duplicate URL parameters before connecting. An explicit unreachable or unmarked target fails instead of silently skipping. Before fixture writes, the guard verifies `current_database()` and the server-side marker against the explicit unique `sellersho_test_` name, with cleanup registered first.

All 47 PostgreSQL opens in 46 files were updated, including seeder, payment concurrency, capacity, returns and supply-tier fixtures. A source guard and real TCP-listener subprocess tests proved zero application endpoint connections for default opt-out and unsafe explicit targets. The source regression first failed against the original referral fixture and then passed. Verification used Windows and the local Go toolchain, without application credentials.

Initial commands and results:

- `go test ./internal/testdb -count=1`: passed, including sentinel and unsafe-target checks.
- `go test ./internal/testdb ./internal/database/seeder ./internal/repository ./internal/worker -count=1`: passed with DB integration explicitly skipped because no disposable DSN existed.
- `go test ./cmd/server -run '^Test.*(SupplyOption|ReferralBonus|PriceIncrease|PriceGov)' -count=1`: passed, with DB-gated tests skipping without opt-in.
- `go test ./... -count=1`: failed in the five known baseline tests: `TestProposeProductRejectsMissingMeasurements`, `TestCampaignDiscountOnlyFromMargin`, `TestCampaignFixedAndFloor`, `TestCampaignNeverRaisesPrice`, and `TestDiscountWithNonZeroPlatformPctFeeUntouched`. Local output: `logs/correctness-baseline.log`.

At that checkpoint, live unmarked-target rejection, migrated disposable lifecycle/cleanup, complete default-suite sentinel schema/data proof, and integration/chaos target protection remained open. No completion was claimed from skipped tests. Docker initially could not start; production PostgreSQL was not substituted. The other 53 tasks were still TODO at that time, and no deployment or capacity certification had occurred.

Independent reviewer `review_test_safety` found a conflicting legacy exact-name guard in `supplierFlowDB`; it was removed in favor of the shared verified guard, and focused suites were rerun. The frontend build passed. Docker became available after user startup, allowing the separate disposable harness and canonical migration stages below.

## TEST-01 disposable harness stage — acceptance passed

Evidence and commands: `docs/test-environment.md`. Nine Python lifecycle/credential-isolation regressions passed. The live sentinel/default-suite check proved zero connections and unchanged schema/data. Unmarked-target refusal, setup/test-failure cleanup, all ten existing SKU tests, and the complete schema-only seeder package passed. Docker runs locally; the PowerShell wrapper ran successfully. The POSIX wrapper still needs a POSIX host check.

Ruling: seeder tests use a schema-only disposable copy to avoid migration demo-user ID collisions. An intact append-only ledger must refuse `CleanDev`/`ResetDev`, replacing obsolete assertions that expected forbidden truncation. Production seeder behavior was not changed. Reviewer `mig01` independently reviewed the harness. Strict bootstrap was jointly validated with MIG-01; final TEST-01 closure uses that integrated revision.

## MIG-01 — DONE

The shared runner serves CLI and HTTP startup. It uses a dedicated session advisory lock, ordered filename identities, immutable SHA256 history, explicit reviewed legacy adoption, transactional DDL plus ledger recording, bounded deadlines and nonzero failure exits.

All 179-migration bootstrap, 178-to-179 data-preserving upgrade, concurrent-runner, injected failure/retry, drift/missing-directory/missing-file, timeout, and killed-child rollback/unlock tests passed. Reviewer `test01_runner` found a SQL-string transaction-boundary escape; a standard-conforming/E-string-aware lexer and regressions fixed it. Dollar-sign identifiers also have coverage.

Nontransactional mode is explicitly refused; none of the existing 179 migrations required it. Production-scale rehearsal and legacy adoption remain release gates. Contract: `backend/internal/migrations/README.md`. This stage closed TEST-01's bootstrap dependency, so TEST-01 is DONE with `docs/test-environment.md` evidence. The five application baseline failures remained assigned to their owning tasks at this stage.

## CORE-01 — DONE

Added the global row-lock contract and caller-owned lock-plan helper, with a writer map covering 292 repository symbols in 41 files, 43 direct HTTP/worker SQL writers, and six Redis mutators. Existing inverted callers are assigned to INV/FIN/JOB owners; the prerequisite does not claim those domain flows are repaired.

Root review caught a nonexistent future placement relation in the helper. It was removed from the executable allowlist, and catalog validation now checks every allowed table/primary key plus rejection of that future name. Typed account guards use stores, suppliers and users; every monetary authorizer must adopt this contract.

`python scripts/test-isolated.py -- go test ./internal/repository -run '^TestTransaction(LockOrder|Contract)' -count=3 -v` passed in 14.865s without skips. It exercised 18 two-connection flows, bounded lock waits, rollback/retry and independent-account concurrency, with verified cleanup. Root reviewed independently of implementer `core01`. No historical data was written. Contract and evidence: `docs/architecture/transaction-contract.md`.

## INV-01 — stock and dispatch gate DONE; financial/history gates retained

Six customer/admin mixed, two-supplier and legacy cases reproduced cancellation restoring dispatched stock from 7 to 10. The shared aggregate-order lock now serializes cancellation with every dispatch writer. Any dispatched supplier leg or warehouse custody evidence rejects whole-order cancellation with typed 409. One hundred supplier races and 100 warehouse races passed; replay restores once and partial-return captures fail atomically.

Reviewer `review_test_safety` found two defects: direct `sql.Open` fixtures bypassed TEST-01, and a TEXT/UUID audit join failed with 42883. Both were fixed and regressed. The final guarded gate included `TestEveryDatabaseTestUsesGuard`, `TestCancellationAuditReportMigratedSchema`, six audit cases, races/replay/alternate writers/carrier legacy/API409 tests. It passed (testdb 2.091s, repository 34.344s, server 0.681s) with cleanup verified.

The delivered read-only audit SQL ran against the actual migrated schema with two seeded anomalies and a control; expected itemized rows appeared and before/after JSON state was unchanged. Local evidence: `inv01-review-fixes.log`. Production historical execution remains FIN-014/release work. No stock correction was performed. FIN-003 must still atomically record paid-cancellation refund liability before release. The older payment fixture missing `payment_expires_at` is recorded for TEST-02.

## AUTH-01 / AUTH-02 — DONE (ship with AUTH-03)

Shared logout clears credentials and all role/capability sessions synchronously and broadcasts storage deletion. Public per-store guest product carts are retained consistently in memory and storage. Authentication owns generation/expected-credential installation; all five real panel entry paths guard later profile/store success and failure. API 401 handling checks generation. Current expiry triggers common logout and completes readiness; an obsolete 401 during a new pending login cannot clear account B.

Mounted regressions first reproduced three role-logout failures, stale refresh/restore corruption and actual 401 readiness failure. Reviewer `core01` caught direct panel bypass, current 401 and readiness supersession issues; dedicated scenarios and production caller adoption fixed them. The final browser/auth/routing/entry-point contract suite passed 41 tests with no skips; the frontend build passed.

Coverage includes late supplier-profile success/error, stale raw setters, refresh replacing restore, refresh ordering, unmount, two tabs, reload and absent authorization headers after logout. Details: `docs/auth-session-lifecycle.md`. AUTH-03 private-cache isolation was a separate gate at this stage. No server JWT revocation or real-provider certification was claimed. Existing money-preview panel hunks were staged separately for FIN-012.

## AUTH-03 — DONE

Verified immutable account/tenant boundaries reset private Order/Store/UI caches. Credential-generation guards reject old asynchronous payloads. The backend already uses `claims.UserID`; UI full-name/recipient-phone ownership filters were removed. Private wishlists and supplier drafts are scoped, legacy unowned catalog authority is removed, and public guest carts remain available.

Reviewer `review_auth03` reproduced stuck create/confirm/dispute busy flags after a rejected replacement login for the same account A. Per-mounted-operation ownership fixed the flags while preserving private-data guards and newer operations. All 26 mounted browser cases, three existing ProductContext tests and TypeScript passed. Actual seller OTP guard navigation passed while preserving input during pending authentication.

The fixture defines 1,000 canary identities across 10 stores; 20 sequential browser identities were exercised. This is not 1,000 active capacity evidence. The user chose local tests and left hardware capacity certification OPEN. Details: `docs/auth-cache-isolation.md`. Unknown legacy ownership was not guessed, and backend money/order ownership rules were unchanged.

## FIN-012 — DONE (internal arithmetic/policy gate)

Integer toman/basis-point policy is versioned: nearest 100 half-up rounding, nonnegative cost floors, early-fee truncation, and exact remainder/cumulative allocation. Four stale campaign assertions now match the current percent-off contract. Raw JSON precision is validated before float/database conversion, including 128-character limits, bounded exponents, case-folded aliases and duplicate financial-key rejection. Review reproduced an unsafe direct admin-ceiling write returning 200 instead of 400; the writer was fixed.

Invalid current public prices fail closed. History remains unchanged with the legacy-unversioned default. Quote/order/addon/return/shipping/Tapin paths use checked integer weights, volumes, fees, conversions and overflow handling. Last-gram loss and rial-plus-five overflow were reproduced and fixed. Reviewer `review_fin012` found alias/direct-SQL gaps; real HTTP no-write regressions proved the fixes. A zero fixed helper is now a documented no-op (1249→1249); checkout reachability remains unconfirmed.

The exact financial-only staged tree caught a hidden INV origin reference in Tapin quoting; its corrected variant preserves legacy origin lookup. That tree passed pricing/shippingquote/testdb units, full server compilation, focused migrated server tests 0.465s and policy/return/repository tests 2.299s, all 180 migrations, and verified cleanup. Its exact frontend build passed. Local evidence: `releases/fin012-exact-staged-*.log`; policy: `docs/architecture/money-policy.md`.

Actual refund integration (FIN-003), PSP integration (FIN-010) and real history (FIN-014) remain required. Broad integration fixtures remain TEST-02 work and are not claimed green.

User decisions: final hardware certification stays OPEN with local tests only; PSP implementation targets Zarinpal using the official contract inputs in `docs/architecture/zarinpal-contract-input.md` and a generated sandbox UUID; operator alerts go to the admin panel, with no external webhook. No production deployment has occurred.

## INV-05 — exact origin identity/allocation gate DONE

Persisted supplier and central-warehouse identities govern eligible-stock planning, immutable checkout splits, capture and restoration. Cartons/inbounds persist destination and source-at-birth; explicit carton destination is wired through the UI/API. Unknown history enters reconciliation views rather than a guessed backfill.

Reviewer `review_test_safety` identified four caller gaps: first-origin-only storage departure, mutable carton receipt source, unattributed future legacy declarations, and Tapin registration using current product/default pickup. Each had a RED/GREEN reproduction. A legacy inbound lifecycle after an SW1→SW2 catalog edit originally minted 7 units from 5; source-at-birth QC debit now conserves 5 and preserves the new SW2 lot. Unknown source and shortage roll back. Warehouse multi-origin splits consume exact positions; multiple unsupported supplier pickups reject before PSP under the one-parcel-per-supplier model.

Actual carton creation→origin edit→receipt and idempotent putaway-position tests passed. The exact staged tree passed guarded `TestAuditSupplyWarehouseOrigin|TestInventoryOrigin|TestSKUInventory|TestGap13Warehouse` with `-count=2` in 26.380s, all 181 migrations and verified cleanup. Its frontend production build and shipping/quote/Tapin tests passed. Local evidence: `releases/inv05-exact-staged-*.log`; contract: `docs/architecture/inventory-origins.md`.

Deployment still needs real origin reconciliation, INV-02 reservations, INV-04 QC, integrated INV-12, FIN-003/010 shortage compensation and the final capacity gate left OPEN by the user. No production data was moved or negative balance automatically corrected.

## OBS-01 — infrastructure checkpoint; acceptance remains OPEN

Commit/push: `3983baa`. Implemented bounded HTTP metrics and correlation, SQL query/prepared/streaming-row spans, real gateway verification decoration, worker-to-SQL correlation, Redis command/Lua/pool instrumentation, PostgreSQL pool/connection/lock/deadlock/transaction gauges, Go/Linux process metrics, expiry backlog, private edge telemetry, dashboard and durable admin-panel alerts.

The actual protected admin read/unread routes expose all five tested failure alerts; customer access is denied. Persistent incident state deduplicates restarts and coalesces notifications within 15 minutes. Reviewer `review_obs_checkpoint` found that a suppressed second incident could remain unreported forever. A guarded regression reproduced count 5 instead of 10; durable `delivery_pending` fixed delivery after cooldown without another recovery transition.

Final integrated verification at `efb37fc`: three independent disposable migrated Docker runs passed (observability 3.477s; admin routes 1.544s; worker/SQL correlation 2.716s), each with verified cleanup. Gateway/testdb and edge privacy tests passed, as did network-disabled nginx/Caddy validation and Linux/amd64 cross-compilation. Commands and remaining gates are in `docs/operations/observability.md`; raw local logs are `logs/obs01-final-{core,admin,worker}.log` and the earlier RED log.

Ruling: this checkpoint does not close OBS-01. Other worker metrics, collector integration, qualified edge traffic, admission rejection metrics, host monitoring, backup signal and alert-rule fixtures remain required. Admin notification during total DB outage stays in process memory until recovery; simultaneous process loss needs independent monitoring. No production access or deployment occurred.

## SEC-01 — source hardening checkpoint; acceptance remains OPEN

Commit/push: `409dc02`. Production Compose requires seven injected credential keys; Redis health checks use an environment value and AOF is explicit. Production configuration rejects known development JWT/webhook material and short webhook secrets. The scoped Compose inventory contains 17 metadata-only current/history records. Its current source has seven required injections, two runtime expressions and no literal credential candidates. Active use is unknown; no runtime/private file was read and no key was rotated.

RED/GREEN: configuration tests first reproduced unsafe development signing/webhook acceptance. All now reject. Six Python checks passed (final 1.865s), including missing-secret refusal and ephemeral injection through `docker compose config --quiet`, plus actual disposable Redis process UID verification. JWT rotation with fresh random keys rejected old sessions and accepted new sessions; auth/config suites passed (1.688s/0.897s), and the backend server built. Review `remaining_backlog/review_sec_checkpoint` caught a shell launcher bypassing Redis privilege dropping; the actual UID test first failed at root, then passed after re-entering the official entrypoint. Scoped review found the fix addressed. Container cleanup checked the exact unique name and test label.

Ruling: SEC-01 stays open for full source/history/artifact inventory, restricted active-use comparison, coordinated rotation, ciphertext/provider compatibility, permissions and rollback. No current Go/TypeScript Tapin encryption consumer was found; that does not establish historical ciphertext absence. Existing runtime configuration must provide the newly required names before deployment. The runbook is `docs/operations/credential-lifecycle.md`. No credentials or raw Compose output are included in the inventory or tests, and no production deployment occurred.

## SEC-01 — constructor diagnostic privacy follow-up

A unit regression reproduced PostgreSQL and Redis constructors returning malformed URL credential material in errors subsequently logged by server startup. Both cases first failed, then passed after initialization errors became value-free diagnostics. Failed startup clients are closed. The test uses malformed synthetic inputs and opens no database connection.

Independent root review approved the 13-line change and tested the three proposed files against an isolated committed-HEAD backend snapshot: repository regression PASS 0.328s and configuration suite PASS. This avoided unrelated in-progress FIN compilation. No caller relies on wrapped startup error classification. Existing unbounded PostgreSQL Ping remains assigned to OPS-01/02; this change does not close that gate or SEC-01. Only the two constructors, regression, runbook and this ledger are included in the follow-up commit.
## INV-11 — DONE (atomic, retry-safe carton creation; INV-02 integration retained)

One guarded transaction now validates the complete draft and integer bounds, locks supplier/products/SKUs in CORE order, rechecks exact owned source/destination, and writes the header and all lines. Supplier-scoped creation keys bind canonical payload fingerprints. Replay returns the committed shipment; changed payload returns 409. Code collisions and bounded whole-transaction retries leave no partial rows. The actual supplier component/API retains one key after ambiguous failure and rearms only after validated completion or changed draft/session.

Regressions reproduced second-line 2147483648 leaving a partial header/line, cumulative overflow, injected-line failure, missing keys and lock-retry failure. Fresh review `review_inv11` found two further defects: truncated 200/missing shipment identity incorrectly acknowledged success, and reused keys with changed nonexistent SKU returned 404 before fingerprint comparison. Both had behavioral RED/GREEN regressions; scoped re-review marked both ADDRESSED with no new blockers.

Final exact staged-source verification excludes all uncommitted FIN-001 changes: canonical migrated Docker suite 34/34 passed (repository88.743s/server2.640s), actual client tests 4/4 passed, and typecheck passed. Cleanup was verified. Source snapshot proof is `releases/inv11-staged-proof.json`; local logs `logs/inv11-staged-{source,client,typecheck}.log`. The earlier shared-tree affected suite also passed 34/34. Migration up/down/up, uniqueness refusal and executable read-only orphan/duplicate/physical-claim preflight passed without deleting or merging historical quantities. Contract: `docs/architecture/carton-creation.md`.

Ruling: INV-11 closes the atomic creation contract; INV-02 reservations must join the concrete caller transaction before that lifecycle is enabled. Production historical review and capacity remain open. The broader frontend baseline remains 270 tests / 268 pass / 2 known CatalogView provider failures; the broad backend run remains red with 143 failure names retained for TEST-02 triage. These are not a green full-suite claim. No production access or deployment occurred.

FIN-012 verification addendum from independent root review: `TestSellerCampaignLifecycleAndIsolation` and `TestStoreWidgetPersistenceAndOwnership` both passed against its exact financial-only staged snapshot (0.38s/0.23s; package 1.134s), with all 180 migrations and verified cleanup. Prior shared-fixture failures did not reproduce in isolation; no source fix was required. Evidence: `releases/fin012-existing-isolated.log`.

## TEST-02 — V2 fixture and default unit baseline checkpoint

The remaining default Go failure was a stale measurement fixture, not a changed
publication rule. Its three bodies omitted schemaVersion=2, productSchema and the
mandatory active SKU/niche, so the existing V2 schema gate rejected them before
measurement validation. The original three cases failed in
`logs/test02-product-fixture-red.log`. Their fixtures now provide a valid simple
V2 definition while retaining the missing parent weight/dimensions and all
original Persian missing-field assertions. A separate regression rejects absent,
legacy and future schema versions before measurement validation. No production
handler or business rule was changed.

Independent reviewer: remaining_backlog. Focused existing/new helper tests passed
in 0.570s. An isolated archive of committed HEAD plus only this fixture passed
`go test ./... -count=1`; raw output is
`releases/test02-exact-unit-baseline.log`. Database integration tests intentionally
skip without explicit disposable opt-ins. This proves the default unit baseline,
not the required integrated/race/CI/capacity gates. TEST-02 remains checkpoint OPEN.
