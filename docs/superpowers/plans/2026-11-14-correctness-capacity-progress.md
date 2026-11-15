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
| INV-02 | DONE | bb61fae / 3aaacc4 | [Coupled acceptance](../../architecture/exact-inbound-putaway.md) |
| INV-03 | DONE | 0209449 | [Aggregate edits](../../architecture/legacy-aggregate-stock.md) |
| INV-04 | DONE | 1e86ad2 / 3aaacc4 | [Coupled acceptance](../../architecture/exact-inbound-putaway.md) |
| INV-08 | DONE | d4e6399 | [SKU capacity](../../architecture/sku-measurement-capacity.md) |
| INV-09 | DONE | 6c9632c / a2439ab / 3aaacc4 | [Exact putaway](../../architecture/exact-inbound-putaway.md) |
| INV-06 | DONE | 54fe3da / 00b34da | [Physical occupancy](../../architecture/warehouse-occupancy.md) |
| INV-07 | DONE | 214a57c | [Stock API](../../architecture/stock-adjustment-api.md) |
| FIN-012 | DONE | bf7d1bd | [Money policy](../../architecture/money-policy.md) |
| FIN-001 | DONE | 64d235c | [Financial contract](../../architecture/financial-balances.md) |
| FIN-006 | DONE | 4524ee3 | [Quantity/time contract](2026-11-15-storage-quantity-time.md) |
| INV-10 | DONE | 4524ee3 | [Exact receipt adoption](2026-11-15-storage-quantity-time.md) |
| OBS-01 | Checkpoint OPEN | 3983baa | [Telemetry gates](../../operations/observability.md) |
| CAP-01 | Checkpoint OPEN | ff4ff13 / Harness checkpoint | [Harness and failed evidence](../../../load-tests/README.txt) |
| FIN-003 | DONE | 79ee940 | [Paid cancellation](2026-11-15-paid-cancellation-refund.md) |
| AUTH-01 | DONE | e0c25ca | [Session lifecycle](../../auth-session-lifecycle.md) |
| AUTH-02 | DONE | e0c25ca | [Session lifecycle](../../auth-session-lifecycle.md) |
| AUTH-03 | DONE | dd373f5 | [Cache isolation](../../auth-cache-isolation.md) |
| CHECKOUT-01 | DONE | checkout-attempt stage | [Durable checkout](2026-11-15-checkout-attempt.md) |
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
| TEST-02 | Checkpoint OPEN | 8302d23 / 1ef7092 | [Frontend baseline](../../frontend-test-baseline.md) |
| OPS-01 | TODO | — | — |
| REL-01 | TODO | — | — |
| BAK-01 | TODO | — | — |
| MIG-02 | TODO | — | — |
| FIN-014 | TODO | — | — |
| CHECKOUT-03 | TODO | — | — |
| PERF-01 | Checkpoint OPEN | a5187bd / 41904d0 | [Paging contract](../../architecture/catalog-pagination.md) |
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
## FIN-001 — DONE (canonical signed balance and debit authorization)

Seller display, statements and payout/service/early-withdrawal authorization share the signed transaction-aware projection. All competing writers adopt the CORE account prefix and revalidate identities. Refunded profit cannot be withdrawn; later refunds may expose debt, and debt cannot authorize spending. Supplier arithmetic retains its signed terms while earned/refund/shipping authority follows complete conserved immutable origins, never today's catalog owner. Missing or conflicting provenance is visibly reconciliation-required and blocks unsafe reads/spending; no history is guessed or repriced.

Creation endpoints require stable request keys. Real financial clients preserve scoped keys across ambiguous failures and validate acknowledgments; immutable service receipts survive later plan/configuration changes. Shared transport rejects malformed positive JSON and preserves valid204. Statements now include the canonical posted terms and separate reservations; full historical reconstruction remains FIN-009. Contract: `docs/architecture/financial-balances.md`.

Independent reviewer `review_fin001` found four Important issues: blank-party receive effects, statement omissions, destructive public fixture drops, and mutable service replay. All four had behavioral RED/GREEN regressions and were marked ADDRESSED in scoped re-review. Legacy fixtures now have schema-only pooled DSNs and prove public capture PK/FK survival. The SKU production change is only namespace-consistent optional-table detection.

Exact staged-source verification excluded every CAP and root test-runner working change:29 focused tests,31 existing regressions,35 migrated SKU/origin/carton/callback cases,2 actual keyed endpoint cases and1 treasury-read case all passed without skips, each in a fresh guarded Docker database with verified cleanup. Actual client tests9/9, typecheck, frontend production build, Linux amd64 build and default Go suite passed. Default DB integrations intentionally skip without opt-in; the guarded cases supply the domain evidence. Proof: `releases/fin001-staged-proof.json`, counts in `releases/fin001-staged-counts.json`, local logs `logs/fin001-staged-*.log`. The earlier UUID timestamp-prefix test flake remains recorded for TEST-02 and is not declared resolved by a passing rerun.

Ruling: known immutable financial principal errors were fixed in this task; unknown legacy liabilities remain a FIN-014 reconciliation gate. New request receipts are final append-only metadata after the guarded domain effect, with no independent earlier-row FK lock. Paid-cancellation/provider compensation (FIN-003/010), remaining referral/storage/payout policies, full statements, historical cutover and capacity/deployment still require their own gates. No production access, rotation or deployment occurred.

## TEST-02 — complete frontend runner and real-provider catalog checkpoint

Added `npm test` using the existing Node/tsx runner, recursively collecting all source `.test.ts`/`.test.tsx` files without new dependencies. Catalog regression tests now mount the actual provider stack in a local browser. A layout recorder checks the first committed DOM before the store-guard navigation effect; actual store navigation replaces the old source-text assertion. No production view/provider behavior was changed.

Fixed the intermittent order-ID smoke test: a legitimate random UUID beginning `179` must not be rejected as a timestamp. The test now checks RFC4122 v4 format and includes that deterministic vector. This is a format/sampled-uniqueness smoke check, not a claim about full endpoint or cryptographic correctness.

Independent review by `remaining_backlog` approved all six code/test paths. Root-only archived tree `775651fdef7092a725acd2cba659c6dabe384dcd` passed all271 frontend tests with zero failures/skips, production frontend build and20 repetitions of the order-ID/quantity smoke. The shared integrated frontend tree including FIN-001 passed277 tests without skips. An intentional disabled render guard in the source archive failed with2 central rows instead of0; restoring the original bytes returned4/4 focused passes. Evidence: `docs/frontend-test-baseline.md`. The commit adds documentation to those verified code files on top of pushed FIN-001 `64d235c`; it does not reuse the older private index to overwrite the financial commit.

TEST-02 remains OPEN for the full supplied integration/race/CI/dependency gates and domain prerequisites. No deployment, production history or intended-hardware capacity claim follows from this checkpoint.

## PERF-01 — bounded cursor protocol checkpoint; route/index gates OPEN

Added the standalone, currently unwired `internal/catalogpage` protocol: default24/max100, no unbounded or repeated offset-page request, versioned bounded cursors with public-only sort keys and exact product tie breakers. Canonical filter/store/owner/revision bindings distinguish a required restart from malformed input; binding is not authorization and future SQL must independently apply scope/visibility/revision checks. Nil/default/set-order normalization does not mutate caller collections. Strict decoding rejects oversized, malformed, unknown, duplicate and invalid-UTF8 input.

Independent review by `remaining_backlog` found case aliases could bypass duplicate detection because Go JSON field names are case-insensitive. New `v`/`V` and `price`/`Price` tests reproduced the failure, then Unicode EqualFold rejection fixed it. Unit tests,10-second fuzz passes (151330 initial /49100 final inputs) and go vet passed. Contract: `docs/architecture/catalog-pagination.md`; raw logs `logs/perf01-cursor-*.log`.

Architecture review rejected routine full-catalog invalidation/rebuild/cold503. Next integration requires a lean price index, transaction-coupled affected writes, prepared broad policy/time transitions, page-only dependency hydration, stable SQL cursors, exact search/bounds and all maintained frontend consumers. Existing owner-approved card default-variant semantics determine public sort/filter price, and displayed integer discount determines discount sort. No public route behavior or measured bottleneck was changed by this package. PERF-01 remains OPEN; CAP-02 intended-hardware certification stays open per user.

## FIN-001 — measured native-ID index follow-up

The large synthetic CAP setup exposed a supplier-origin correlated scan; converting canonical UUID join/lookup columns to text prevented the existing origin index from serving an equality Index Cond. Five SQL predicate changes now use the matching native ID types in authority, ambiguity candidates, account discovery and single-item lookups. The quantity/order/nonblank-single-party immutable authority checks are unchanged. This does not guess from mutable catalog ownership, alter historical rows, or add a schema/index migration.

New `TestSupplierOriginAuthorityUsesCanonicalUUIDIndex` copies actual migrated UUID tables/indexes into a rolled-back private schema, seeds5000 origins, proves an index condition is possible, and checks earned500000. Old code failed the index assertion; new code passed. Forced `enable_seqscan=off` is explicitly only an index-eligibility diagnostic, not a production plan or latency claim.

Independent review by `remaining_backlog` approved the three code/test paths. Exact root-only tree `12a78b63542220670dfe184efebe15f033983461` (basea5187bd, no concurrent INV/CAP source) passed55 top-level finance/origin/returns/storage tests without skips in22.551s, with verified disposable cleanup; repository go vet passed. Shared-tree55/55 also passed. Evidence: `docs/architecture/finance-origin-lookup.md`, ignored `logs/finance-origin-index-*.log`. Same-cohort large-data retiming and repeated treasury-query work remain CAP/FIN-013 gates; no100-RPS or intended-hardware claim.
## CAP-01 — test-only startup adapters checkpoint; harness remains IN PROGRESS

The isolated harness can now select a no-network verification double and a private carrier quote endpoint at startup. Both require exact APP_ENV=test and the private run marker; latency samples/modes are bounded, and carrier selection accepts only the fixed isolated alias. Missing controls preserve ordinary real/mock behavior. All controls validate before either applies; errors do not echo inputs. The selected gateway retains normal observability wrapping.

Independent coordinator review approved the five-file integration. Its exact staged-source snapshot excludes all uncommitted INV-02/PERF/harness changes: eight tests passed without skips (payment1.823s/server0.353s), and Linux amd64 server build passed. Proof: `releases/cap01-adapters-proof.json`; logs: `logs/cap01-adapters-staged-{tests,build}.log`.

Ruling: these are explicit test adapters, not provider certification or CAP-01 closure. They simulate verification only; carrier registration, provider inquiry/refund/process-loss behavior and real Zarinpal sandbox checks remain separate gates. The independent full harness, realistic fixture, all journeys/fault profiles, complete financial conservation and final hardware qualification remain open. Failed local workload baselines stay failed; no production service, credential or deployment changed.

## PERF-01 — stable bounded source-key SQL checkpoint

Added unwired `GetProductCandidatePageTx`: N+1 lightweight source keys and at mostN returned, SQL keyset ordering with unique-ID ties, direct store assortment join and actual owner/type check, all supported non-price source filters beforeLIMIT, suspended-supplier exclusion, and explicit refusal of price sorting/filtering until the authoritative public-price index exists. It does not load whole assortments, supplier costs or product/variant DTOs. A small `catalogpage.Validate` entry validates first-page requests/decoded keys. Actual `store_products.position` is a signed SQL integer without a nonnegative constraint; a RED regression led to supporting its complete signed range in store-position cursors.

The shared guarded suite passed10 top-level protocol/candidate tests without skips, including three migrated private-schema cases enumerating248 of250 products exactly once through tied/nullable keys, filter-before-limit behavior, two store assortments, inactive picked products, supplier suspension, original/stock predicates, bound search text and price-index gating. Protocol/repository go vet passed. No public route/consumer uses the primitive. Authoritative scope revision, priced eligibility, exact public price ordering/bounds, page-only hydration and complete UI migration remain activation gates; PERF-01 stays OPEN.

Candidate checkpoint independent review approved the four code/test paths with no Important/Critical finding. Exact staged source tree `84c3a78a20d7238e600505ed1b3fd0c0f8d9b954` (based on pushedff4ff13, excluding concurrent INV02/CAP-harness work) passed the same10 top-level tests0skip, repository0.760s, verified disposable cleanup and both package vet checks. Logs: `logs/perf01-candidates-exact.log`, `logs/perf01-candidates-exact-vet.log`.


## INV-02 — conserved transfer provenance checkpoint; coupled lifecycle OPEN

Atomic carton and legacy ASN booking now reserve exact named supplier source allocations; explicit dispatch moves those same units to transit, and cumulative receipt stages only its counted delta in quarantine. Undispatched cancellation releases original balance IDs once. Explicit external replenishment requires independent unique evidence and cannot reuse transfer allocations. Additive migration 202611140006 records immutable line provenance and monotone counters; historical unknown stock is reported for reconciliation, without invented replenishment or automatic repair. Both inbound UIs expose actual source/destination and carton dispatch/cancel.

Fresh review found three Important issues: partial ASN receipt closed too early, physical balances were acquired per product rather than one global bytewise order, and external SKU discovery was not revalidated after guards. Each had an actual guarded PostgreSQL RED and was fixed; scoped re-review approved all three fixes with no new Critical/Important finding. ASN 2 -> 5 -> 5 conserves five units, six is rejected, and unchanged partial replay preserves completion metadata. Controlled blocking/NOWAIT tests prove global balance acquisition, and changed discovery rolls back without a header.

The exact reviewed source tree e6e27bdd7a38603d55c0f676f4de0e5b2bbdc49f, based on ff4ff13, excluded concurrent PERF/CAP changes. Its guarded affected run executed 47 top-level backend cases successfully (repository 28.203s; server 1.520s), with disposable cleanup verified. Exactly seven retained positive QC/putaway integration cases remain explicitly skipped: three tracked/origin putaway cases and four legacy inbound mode cases. These skips are pending integration evidence, not passing end-to-end acceptance. Five client tests passed without skips; typecheck, frontend production build and default Go suite passed. Default DB fixtures intentionally skip without opt-in configuration. Proof: releases/inv02-staged-proof.json; logs/inv02-exact-{affected,client,typecheck,build,default}.log.

Ruling: commit this reviewed transfer infrastructure as Checkpoint OPEN, because INV-04 disposition and INV-09 exact putaway must consume the same physical allocations before the seven retained positive branches execute without skips. The current full receipt lifecycle remains gated; no QC-created sellable stock or silent second supplier debit is enabled. A fresh INV-04 implementer is continuing the coupled work immediately. No production data, reconciliation, service or deployment changed; intended-hardware capacity stays OPEN under the user's local-only ruling.


## QUAL-01 / CAP-01 discovery — supplier warehouse geography schema checkpoint

The guarded carrier fixture exposed a real canonical-schema defect: existing Tapin supplier-origin lookups selected province/city provider IDs absent from every supplier warehouse migration. Actual fresh migrated PostgreSQL reads failed with undefined-column 42703. Additive migration 202611140007 supplies nullable INTEGER columns matching central warehouse types, with no default, inferred geography or backfill. Existing unmapped responses remain zero; recorded explicit IDs round-trip. Down refuses any recorded mapping.

Independent coordinator review approved the three source/test files. Exact source tree 702d8c0f6f63c27b714db80c1cb4b53203eaaffe (base bb61fae) excluded uncommitted harness, QC and catalog work. The focused guarded migrated-schema regression passed without skips (0.411s), including unknown-null compatibility, column shape, both real repository lookup paths, and rollback refusal tested in an always-rolled-back transaction. Disposable cleanup verified. Proof: releases/geo-staged-proof.json; log: logs/geo-exact.log. Contract: docs/architecture/supplier-warehouse-geography.md.

Ruling: repair this application-schema contract through a normal migration rather than adding fixture-only columns, because the real supplier carrier path otherwise fails on a fresh canonical database. QUAL-01 and CAP-01 remain OPEN; this does not certify carrier behavior or capacity. The same conserved 140k-line financial cohort now measures five supplier reads at 1667/1616/1644/1733/2035ms after the native-UUID lookup fix, improved from the observed 503.61s scan but still outside the read-latency target. Those measurements remain failed performance evidence for FIN-013/PERF work. No production data or service changed.

## FIN-012 / QUAL-01 — authoritative suspended-sale wire follow-up

An actual variant-engine+JSON regression reproduced a below-R sale being correctly suspended on the backend but losing `onSale:false` through omitempty. The browser intentionally treats a missing flag as a legacy backend and may apply the configured sale, so absence was not equivalent to false. New value JSON marshaling emits explicitfalse only when a sale price is configured and did not win; it preserves no-sale response shape and onSale:true. No price calculation, charge or historical snapshot changes.

Independent review approved the three new code/test paths. Root-only tree `fe70a0d89cc112e991eb918d90c5bedd37f46cb8` based on031df37 excludes concurrent INV04/CAP-harness work and passes complete default server unit package (DB opt-ins absent), new actualengine/wire regressions, four client card-price cases0skip and typecheck. Client tests also pin active priced store-default→product-default→first-active order and secondary range separation. Evidence: `docs/architecture/catalog-price-authority.md`, `logs/catalog-price-wire-*.log`. Full shared asOf service, index, temporal consistency and public pagination gates stay OPEN.

## Shared pricing extraction and requested release freeze

Moved existing variant/widget/campaign composition and public variant-price JSON serialization into internal/catalogpricing. HTTP wrappers retain their established defaults, while the reusable service requires explicit source settings and one AsOf for all SKU/widget/sale checks. Existing integer pricing, campaign/lock precedence and suspended-sale false authority remain intact. Active campaign loading and complete transactional index snapshots are still activation gates.

Independent coordinator review approved all seven pricing source paths and two deployment guard paths. Root-only frozen source tree48d2c3b908117419e19e7d8823f0de8a3adfb4b2 passed full default Go suite (DB opt-ins absent) and frontend production build. Separate guarded pricing/service/P1/P2/wire suite passed13 top-level cases without skips and verified cleanup. Logs: release-freeze-go.log, release-freeze-build.log, catalogpricing-service-integration.log under ignored logs/.

User requested freezing current work, pushing it and deploying production. Read-only server preflight found191 legacy migration rows, no checksum column,12 extra legacy aliases and8 pending current migrations. Prefix202610190003 has two canonical candidates; no history has been deleted or guessed. Added early read-only checksum adoption guard in deploy.ps1 before builds/uploads/service changes, and unknown-history guard in deploy_remote.py before replacement. A reviewed isolated history-adoption/upgrade rehearsal is required for this production database. Existing backend health returnedHTTP200. Final intended-hardware capacity remains open per user.


## INV-04 — conserved public QC disposition checkpoint; putaway OPEN

Both real QC entry points now consume exact persisted receipt allocations through one bounded transaction, without the obsolete second supplier debit. Cumulative passed/failed decisions have immutable receipt-line/business identities; partial receipt/disposition, delayed exact retry, opposite-endpoint replay and selected SKU conserve stock. Passed units remain qc_accepted, failed units damaged, and undisposed units quarantine: none become sellable before INV-09 exact putaway. Shortage is an audit snapshot, never inventory credit. Additive migration202611140008 protects append-only event evidence; historical unmapped receipts remain visible as reconciliation exceptions and cannot be silently repaired.

Fresh independent review found one Important UI issue: same-product SKU rows were visually indistinguishable. A real mounted regression failed before visible SKU/receipt identity was added and then passed. Scoped re-review approved both fixed files with no new finding; backend conservation/replay review found no blocker. The current UI preserves cumulative progress and exact retry payload after ambiguous acknowledgments.

Final source tree43768dd8f48ed27466d5a28150f1429429fe146c is based on9221c89 and excludes CAP working changes. Its complete backend subtree is identical to separately tested tree87d1ef687f2f8ae1c9ce3e54cb2fc5cef0ac13b0. Guarded affected verification passed58 top-level cases (repository36.239s/server2.249s), with verified disposable cleanup and exactly seven retained positive INV-09 integration skips. Three final client/browser tests passed without skips; typecheck, frontend production build and default Go suite passed. Default DB opt-ins remain absent and are not integration evidence. Proof: releases/inv04-staged-proof.json; logs/inv04-exact-{affected,client,typecheck,build,default}.log.

The separate broad all-opt-in Go suite FAILED with109 named top-level failures and verified cleanup. The sanitized complete name list and scope are committed in docs/quality/inv04-broad-integration-failures.txt. They require TEST-02 fixture/domain triage and are not all proven harmless. No unrelated fixture repair or business-rule weakening was performed during the user's release freeze.

Ruling: this is Checkpoint OPEN, not full INV-02/04/09 lifecycle completion. The seven positive putaway/capture/storage branches remain retained but skipped, pending INV-09 integration. The user has now requested freezing/pushing current changes and a guarded deployment attempt; FIN-003 was never started. No production migration or data change was performed by this checkpoint.


## CAP-01 — frozen disposable harness and realistic fixture checkpoint OPEN

The coherent51-file checkpoint supplies a guarded disposable stack, 1000 independent principals, the fixed100 API-RPS mix, valid lifecycle queues, protocol drivers, private SMS/carrier/payment doubles, frontend/browser preparation and synthetic historical/stock/fee fixtures. Source, binary, migration, image and selected raw evidence hashes identify the actual measured inputs; HEAD alone is never asserted to identify an uncommitted snapshot. The representative fixture has100000 historical orders/140000 lines, immutable conserved origin/financial snapshots, actual release/replay, payout reservation/replay, statement deltas and aged fee/stock reconciliation. The retained full-fixture run passed337.80s; it included the explicitly recorded then-uncommitted0008 migration and is not an exact-clean-HEAD capacity claim.

Fresh review rejected three Important harness defects: container network names were not joined to attachment IDs, recursive temporary cleanup lacked a resolved ownership check, and overdue arrivals silently caught up. Actual focused RED/GREEN fixes now pin immutable network IDs before start and validate attachments afterward/at cleanup, verify canonical temporary path and file identity before recursive deletion, and drop missed schedule slots under a documented10ms tolerance while reporting actual rates/lag separately from planned rates. A bounded real unique internal-network create/start/cleanup check passed. Scoped re-review approved all three findings with no new blocker.

Final archive tree37764bc2465c2c2f70133199eeac371702b1f4b8 is based on1e86ad2. All51 archived file SHA-256 values match the reviewed frozen bytes, including evidence. Its Python helpers passed15 tests and Node helpers passed26 without skips. The existing Go/SQL fixture sources were unchanged by review fixes; no six-minute fixture/load/browser retest was substituted for the retained measurements. Proof: releases/cap01-staged-proof.json; helper logs: logs/cap01-exact-{python,python-extra,node}.log. Its complete default Go suite also passed; DB opt-ins are intentionally absent and are not full integration evidence (logs/cap01-exact-default.log). PowerShell entry-point syntax validation passed.

Retained hot-SKU evidence proves five paid captures from five units,95 explicit prepayment stock conflicts,10 duplicate callbacks,320 matched API/domain calls and zero stock/amount mismatch. Latency still FAILED: token p95 1393.317ms and callback p95 1017.468ms. Browser setup FAILED with zero rendered pages and zero HTTP/domain/API requests. Earlier dropped/timeout/self-throttling failures remain failed. Pre-fix timing runs are additionally qualified by their old missed-schedule limitation; no raw measurement was rewritten to imply a new-scheduler pass.

Ruling: this is a reviewed partial environment checkpoint, not CAP-01 closure or CAP-02 intended-hardware certification. Full independent pricing/refund/ledger oracle, successful browser/static paths, expiry/device/fault/recovery/staff/hot-account/cold-cache work, full-duration qualification and application latency remain OPEN. The user's explicit release freeze supersedes continuing implementation; FIN-003 was never started. Raw evidence is preserved byte-for-byte; the frozen script's cosmetic final blank line is deferred. No production service, provider or customer data changed.


Final combined frontend verification at the release freeze: npm test passed285/285 with zero failures or skips (147.27s), including the final QC mounted regressions. Log: logs/final-freeze-frontend.log. This frontend result does not turn the109-failure broad backend integration run or failed capacity/browser results into passes.

## Production deployment attempt — 2026-11-14

At user request all current reviewed work was frozen and pushed through code commit01cbcba (INV04:1e86ad2; CAP harness:01cbcba). Tracked source/index were clean; HEAD/origin divergence0/0; only standing-excluded backend/internal/networkreward/ remained untracked. No new FIN003 work was started.

Executed deploy.ps1 -BuildOnServer. The read-only preflight returned: DEPLOYMENT BLOCKED: production migration history needs reviewed legacy adoption and an isolated upgrade rehearsal. This happened before builds/uploads/traffic changes. Production was NOT deployed or migrated; no rollback was required. Subsequent backend health returnedHTTP200 and the existing frontend answered its internal root probe successfully. Raw local output: logs/production-deploy-request.log. Production history reconciliation/adoption and isolated rehearsal remain required; no checksum/alias guard was bypassed.

Historical delivery count at the earlier release freeze:11 DONE local gates,7 partial checkpoints,36 TODO. That snapshot had109 backend failures. It is superseded by the execution evidence below; retained capacity qualification failures remain open.

## Continued implementation and full integration triage

Committed source3aaacc4ed256c39051bb7c958bddb4ac794fa5f9 ran572 discovered
top-level tests with zero failures in individually cloned migrated PostgreSQL
databases and a separately authenticated disposable Redis cache. The single
intentional subprocess helper skip does not omit a domain gate. The original109
failures were resolved through isolation, current-policy fixture corrections and
scoped domain fixes; no blanket business-rule weakening or history deletion.
Full evidence and each stage: [integration record](../../quality/test-suite-isolation.md).
Every prior stage was committed/pushed and deployed using deploy.ps1; runtime
credentials, backups, unrelated services and the standing excluded paths were
preserved. Migration-history adoption was separately rehearsed before its earlier
production upgrade; migration202611140009 adds exact putaway provenance without backfill.

Coupled INV-02/04/09 acceptance now executes all retained positive branches without
skips, including canonical legacy/SKU receipt -> QC -> exact putaway -> capture,
source conservation, failed/partial QC, source/capacity refusal, replay and storage
coverage. These three local task gates are DONE. INV-06, INV-10 and FIN-006
are distinct remaining occupancy/age/fee tasks.

INV-03's scoped absolute aggregate, reason/actor, INT bounds, API/UI, controlled
capture/update and atomic publication requirements passed real isolated tests.
The reviewer approved the final race/report fixes. [Stock evidence](../../architecture/legacy-aggregate-stock.md).
Current ledger:18 DONE local gates,5 partial checkpoints,31 TODO. This is not a
production readiness or intended-hardware1000-user/100-RPS certificate. TEST-02
remains partial for CI/race/dependency and other listed domain prerequisites.


## INV-08 — verified effective SKU physicals

The same effective verified physical resolver now drives the queue, suggestions,
assignment and confirmation. Independent immutable warehouse measurements identify
product fallback or exact SKU overrides; supplier declarations remain unverified.
Reservations and completed placements retain the exact revision and rounded
volume/weight. Measurement changes reset affected pending work; completed records
and historical bin occupancy remain unchanged. SQL guards refuse forged/stale
placement evidence before it can occupy an immutable task identity.

Local RED/GREEN evidence includes the original parent-only capacity reproduction,
exact-SKU HTTP and mounted browser regressions, and direct SQL forged-placement
regression. Final isolated affected suite19/19 passed, followed by migration
preservation rehearsal and the full working-tree suite591/591 with zero failures.
That full count includes3 tests in the paused untracked networkreward package;
588 tests cover release-eligible packages. The only top-level skip is the
intentional migration subprocess helper. Both disposable containers were verified
removed. Frontend286/286 passed with no skips, production frontend build and
repository/server go vet passed. Independent review approved the final guards and
locking with no unresolved Critical/Important finding.

Evidence: logs/inv08-{before,http-before,ui-before,placement-before,combined,
migration,full-integrated,frontend-full,frontend-build}.log. Contract:
[verified SKU capacity](../../architecture/sku-measurement-capacity.md).
This closes the local INV-08 gate; occupancy release remains INV-06. Paused
networkreward and docs/fa remain excluded from source delivery.


INV-08 delivery: commitd4e639984dc97c21381a3f0343200a0a4ca8426a was pushed
and deployed with deploy.ps1 -BuildOnServer. Backup, migration010, backend/frontend
health and root/nginx checks succeeded. Evidence: logs/inv08-production-deploy.log.

## INV-07 — stock reasons and safe SKU inputs

The exact-SKU form already supplied the reason after INV-03; its mounted browser
regression now proves blank/missing-SKU/invalid quantities send no request,
409 retains selected SKU/quantity/reason, Persian zero is accepted, and the
refreshed authoritative product total is displayed. The API requires an explicit
non-null quantity; omission can no longer zero stock. UUID validation and typed
held/origin/ownership/projection errors produce400/409/404 before mutation.
Reasons are trimmed and retain the authenticated actor in movement/audit records.

Real PostgreSQL RED tests reproduced missing quantity changing sibling stock,
malformed-SKU500, held-stock500, and aggregate INT overflow500. The final gate
covers missing/null quantity, missing and foreign SKU, numeric bounds, exact
INT_MAX aggregate success, one-over rejection, no mutation/movement after invalid
requests, held locations, zero, reason/actor and inactive origin.

The release-eligible full suite completed589 top-level tests with zero failures
and verified PostgreSQL/Redis cleanup. The final upper-bound review fix was made
after its server binary had compiled; all185 server tests were therefore rerun
from final source and passed, including the expanded regression. Repository tests
in the full run compiled after that fix. No paused networkreward package was
selected. The single intentional migration subprocess helper skip remains.
Frontend287/287 passed with no skips, production build and repository/server vet
passed. Focused final HTTP3/3 passed. Independent final review found no unresolved
Critical/Important issue. Logs: inv07-{http-before,overflow-before,final-http,
full-integrated,final-server-full,ui,frontend-full,frontend-build}.log under logs/.
Contract: [stock API](../../architecture/stock-adjustment-api.md).


INV-07 delivery: commit214a57c59f4dba563b6bce78c3520b5b55c9e1b4 was pushed
and deployed with deploy.ps1 -BuildOnServer. Backup and health/root/nginx checks
succeeded. Evidence: logs/inv07-production-deploy.log.

## INV-06 — physical departure core checkpoint

Actual capture/bin allocations now release archived measured placement occupancy
at warehouse shipment or supplier-return handover, once per immutable business
identity. Payment, picking and pre-departure cancellation retain physical
occupancy. Cumulative remaining-volume rounding, mixed measurement revisions,
multiple bins, concurrent departures, refill, strict SQL business caps, reasoned
historical disputes and actual actor/replay identity are covered. Supplier-first
return creation/review/parcel/handover guards fix the reproduced FK lock inversion.
Unknown old bin/origin metadata preserves custody transfer and numerical counters,
recording unresolved evidence and disputing known bins. Shared-bin unknown sources
are preflighted before releasing any product in that bin.

A preliminary evolving-tree run (logs/inv06-core-full-integrated.log) had600 tests
and8 failures: TestCancellationDispatchRaces,
TestProductModelV2IsolatedSupplierReturnLifecycleKeepsExactSKU,
TestSupplierReturnInventoryFinancialAndTariffLifecycle,
TestINV08MigrationPreservesHistoricalCapacity,
TestINV06SharedBinRevisionFIFOAndDatabaseEventCaps,
TestINV06ReturnCreationWaitsForSupplierBeforeProduct,
TestINV06LegacyReturnWithoutOriginMetadataKeepsCustodyTransfer,
TestINV06MigrationPreservesAndFlagsHistoricalCounters. Empty owner identities,
a missing fixture supplier/actor and private floor-only scope, supported legacy
metadata, dependent migration rehearsal and the actual review defects were fixed.
That run is retained and is not release qualification. No retained exception was
deleted to make migration tests downgrade successfully.

Final exact source tree8372ea16604998e2e0ceab7c107fe107386135df completed
**603 top-level tests with zero failures** in individually cloned migrated
PostgreSQL and authenticated disposable Redis. Only the intentional migration
subprocess entry skipped; its parent tests execute. Both containers were verified
removed. Exact archive repository/server go vet passed. Source/runtime files are
frozen; only this evidence documentation is added afterward. Proof:
releases/inv06-core-proof.json; log: logs/inv06-frozen-full.log.
Final affected18/18, empty-replay follow-up2/2 and model/earlier-migration/audit
rechecks passed. Independent core review found no unresolved Critical/Important
finding after RED/GREEN fixes. Frontend source is unchanged from the287/287 passing
INV-07 stage; production deployment still builds both applications.

INV-06 remains Checkpoint OPEN: the explicit fresh measured physical-return-to-bin
entry branch is the next substage. Its existing receipt path credits a warehouse
or supplier floor, and must not fabricate an ASN allocation or move already-paid
floor quantities into a guessed bin. Read-only keyset audit, immutable history and
upgrade rehearsal are included; unresolved production counts are not certified.
Contract: ../../architecture/warehouse-occupancy.md.


## INV-06 — DONE (fresh measured customer-return bin entry)

The preceding core checkpoint was pushed and deployed as54fe3da, with verified
backup/migration/backend/frontend/nginx health. Its previously OPEN return-bin
acceptance is now implemented by an explicit post-QC item action. Received floor
stock, shipping settlement and refund behavior remain intact. Proven surviving
central-floor items may enter a matching verified bin using current verified
physical measurement. Supplier/defective destinations and ambiguous legacy pools
stay in their supported floor/reconciliation flows; no source/age/SKU is guessed.

Migration202611150002 derives source/measurement/receipt facts in BEFORE and applies
header, conserved floor debit/bin credit, movement, new measured placement, bin
counters and receipt-dated returned storage atomically in AFTER. An injected late
SQL collision rolls back every effect. Placements distinguish inbound tasks from
return entries explicitly. Paid capture/reservation excludes floor units from the
new action while retaining bin occupancy until actual departure. Active inbound
capacity reservations remain protected. Existing snapshots/counters are preserved.

Ruling: use a separate post-QC command rather than fabricate ASN/QC putaway tasks
or automatically move already-paid floor stock. The failure cost of an incorrect
source decision is duplicate/incorrect custody and capacity; therefore exact
receipt movement/reference/location and source remainder are required. Legacy
same-product multi-line pools are refused for counted reconciliation.
Ruling: one-statement AFTER application replaces the planned deferred orphan check;
no orphan header can commit, including direct SQL and later-step failures.
Ruling: financial refund cannot hide proven unshelved physical work; eligible
refunded items remain in the warehouse queue and can receive the same bin action.

Verified RED-to-GREEN review regressions: concurrent exact replay before uniqueness
checking, dependent trigger preventing empty downgrade, and refunded queue visibility.
Fresh receipt->original placement->payment->shipment->actual return create/approve/
QC receive->fresh measured bin entry->second payment/shipment passes for tracked
and uniquely attributable legacy stock. Tests cover changed/identical replay,
last-unit races, partial entries, real paid-floor capture/cancel, actual supplier
reservation, actual active inbound capacity reservation, unverified SKU override,
wrong warehouse, disputed/inconsistent counters, insufficient capacity, defective
QC, exact tracked split items versus ambiguous legacy pools, direct SQL forgery,
atomic failure, preserved receipt timestamp, and populated downgrade refusal.
The mounted browser test retains exact item and command after conflict/response
loss, issues a distinct key for changed input, refreshes actual remainder and can
retry the original command even after a refresh shows zero remaining stock.

Initial frozen tree f4aebca173db45ed6ffb2408911a98b2738247da completed615 tests with
exactly2 failures: TestWarehouseQueueDestinationFilter and
TestReturnShippingCODRefused. Both built old hand-written DTO schemas missing the
new receipt-source view. Only their fixtures were upgraded to canonical migrations
and actual create/approve flows; all destination, refusal, no-receipt/no-charge/
no-wallet and queue visibility assertions were preserved. No runtime schema
fallback or skipped scenario was added. Independent review approved both changes;
corrected affected4/4 passed. Earlier failure logs remain retained.

Final exact source tree eb66f58726b8df57dd195a74060ada7568c77664 completed **615
top-level tests with zero failures**, using individually cloned migrated
PostgreSQL and authenticated disposable Redis. Only the intentional migration
subprocess helper entry skipped; parent process tests execute. Both owned
containers were verified removed. Final archive repository/server go vet passed.
Frontend **288/288**, production build and mounted new form passed. Independent
review found no unresolved Critical/Important findings. Proof:
releases/inv06-return-final-proof.json; logs/inv06-return-final-full.log,
logs/inv06-return-frontend-full.log and logs/inv06-return-build-final.log.

Only evidence documentation is added after this exact runtime/test freeze.
Next: FIN-006 storage quantity/time plus INV-10 exact receipt/departure adoption;
its separately prepared plan is not included in this release. Production storage
fees were read-only verified disabled and remain so. Historical counted occupancy
reconciliation and intended-hardware1000-user/100-RPS qualification remain OPEN.


## FIN-006 / INV-10 — DONE (coupled local receipt quantity-time gate)

The deployed INV06 return-bin base00b34da was independently verified pushed and
healthy before this stage. This stage separates each accepted QC/physical receipt
cohort with true source, physical SKU, supplier and warehouse, actual physical
arrival and distinct QC billing start. All current ASN/carton receipts converge
on the same protected ledger; putaway and return-bin entry map existing cohorts
without double credit. Failed QC and supplier-premises stock remain outside the
central sellable-goods fee policy. Proven customer returns start fresh at QC.

Immutable physical departures consume actual paid captures/consumed reservations
and measured placement releases, not sibling SKU/product FIFO. Paid capture and
cancellation retain held quantity. Daily fees use frozen measurement/tariff/free
period and completed Tehran-midnight quantity, including unpaid days on closed
positions. Both supplied probes charge34800, not348000 or0. Exact rational
half-up per-unit rounding and checked money bounds replace floats. SQL protects
receipt identity, quantity/closure/fee projections, daily charges/certificates and
checkpoints; a checkpoint cannot discard an unrecorded positive quantity-day.
Unknown policy/measurement/history never manufactures debt or blocks custody.

Existing positions, dates, charges and financial links are preserved. Migration003
adds source/cutover/exception evidence; it does not reprice old merged quantities.
Populated downgrade refuses retained evidence. Existing001/002 checksums are not
changed. Pre-cutover custody remains explicitly unreconciled. An incomplete actual
source records a whole-source exception with no guessed partial debit, retains
pending days and blocks unsafe active accrual until counted reconciliation. A
later exception cannot suppress an already-closed known cohort's earlier unpaid
accrual. SQL derives exception creation time, so an insert cannot backdate it.

Independent review found unbounded discovery under LIMIT. Corrected with a
persistent transactional keyset frontier and matching C-collated index:<=100
receipt identities examined/pass,<=32 days/lot, explicit MoreWork and bounded
unresolved sample. Real102-cohort test proves no starvation. Reviewed/root fixes
were observed RED then GREEN, including SQL source forgery, rate precision,
quantity/debt/checkpoint forgery, interruption recovery, unknown exception clock
and actual closedA->later unknown stagedB->same-bin placement/shipment interaction.

Final exact frozen tree7377809a62086863f5e5a62134b559c798087054 passed **639 top-level
entries with zero failures** in guarded PostgreSQL/Redis clones. Only the intended
TestMigrationProcessHelper entry skipped; its parent process tests executed.
Both owned containers were verified removed. Frontend **288/288**, production
build and final repository/server go vet passed. Linux server/migrator builds
passed. Frontend/Go runtime sources are hash-identical to those build/test trees;
only the SQL fix and extra regressions changed in the final candidate.

Evidence: releases/fin006-release-source-proof.json;
logs/fin006-release-frozen-full.log, fin006-release-vet.log,
fin006-frozen-frontend.log, fin006-frozen-build.log;
focused fin006-closed-exception-after.log5/5 and
fin006-exception-boundaries-after.log2/2. Initial evolving discovery635/41fail,
first frozen636/1fixture-fail, corrected intermediate636/0 and final639/0 logs are
retained separately; failures were fixed, never skipped or called release success.

Ruling: preserve the existing architecture; no new central-to-central transfer
feature is invented. Current inbound writers accept supplier->central or explicit
external replenishment. Pure transfer policy preserves root age/rate and one
midnight owner; unsupported persisted transfer lineage fails closed. Cost: any
future central-transfer writer needs physical lineage and its own acceptance.
Ruling: historical unallocated returns remain uncertified; actual order capture,
QC and conserved physical receipt are required for new billable return cohorts.
Cost: those old goods require counted review, not guessed supplier/age charges.
Ruling: active uncertain histories remain pending for counted reconciliation,
while later unrelated uncertainty cannot block already-proven closed history.
Cost: active uncertain cohorts wait for reviewed resolution; no pending debt is
silently discarded or charged from guessed quantities.

Production fee activation and historical reconciliation remain OPEN. Storage fees
were read-only verified OFF on production; this stage does not enable them.
Hardware1000-user/100RPS and production-scale execution-plan qualifications remain
OPEN under the user's local-only choice. Actual release delivery is recorded by
deploy.ps1 receipts/backup path and the production deployed-commit marker.

Task inventory after this local gate: **20 DONE,5 checkpoints OPEN,29 TODO** out of54.
Next: FIN-003 durable paid-cancellation refund liability, then the remaining task
contracts. A next-stage refund-destination preference was requested asynchronously;
it does not gate this storage release. No FIN003 implementation is in this commit.

## Paid cancellation local acceptance — 2026-11-15

FIN-003 local gate passed: frozen source
`e6fe0a059613d06854129838eee514e9c21b5a8c`,689 top-level backend entries,0
failures;688 pass and the intentional migration subprocess helper skip (its parent
process tests execute the helper). Owned PostgreSQL/Redis cleanup verified.
Frontend290/0, TypeScript, production frontend build,vet and Linux builds pass.
The final source differs from that executable test source only in these release
notes/status; backend/src/scripts trees are verified identical before commit.

Evidence: logs/fin003-e6fe-frozen-full.log, logs/fin003-d860-frontend.log,
logs/fin003-d860-typecheck.log, logs/fin003-d860-build.log and
logs/fin003-e6fe-vet.log. The earlier d860 full run688/1 exposed an outdated SKU
fixture; the actual current migration/guards and exact paid-source liability were
added to that fixture (24 SKU/origin tests passed), then451e full688/0 passed.
The final689/0 includes the added accepted-source replay regression. Independent
review findings were reproduced and fixed; no known failing gate is omitted.

Current counts:21 DONE,5 Checkpoint OPEN,28 TODO. DONE is the local gate defined
above. FIN-010 original-payment execution, uncertain historical reconciliation and
production-equivalent1000-user/100-RPS certification remain OPEN. Wallet checkout
remains disabled. No live PSP request or historical automatic wallet credit was
used. Deployment follows the standing committed-source deploy.ps1 workflow.


## CHECKOUT-01 — DONE (local acceptance; production release pending)

One immutable checkout attempt now persists through timeout, response loss,
reload and concurrent tabs. IndexedDB coordinates opaque account-scoped IDs
and request hashes without contact/address data. An explicit new purchase
rotates the identity; materially changed payloads require resolution or explicit
abandonment. The server stores a normalized immutable fingerprint and commits
order/items/allocations plus attempt completion atomically. Abandonment uses
the same lock and inserts a tombstone even before the original request arrives.
It never cancels an already-created order. Owned recovery returns that order
without starting payment or replaying address/discount-use side effects.

Migration005 adds immutable new-checkout product-title snapshots. Historical
NULL titles retain the explicit legacy catalog fallback; no historical title,
stock, wallet or money fact is invented. Item loading failures and incomplete
browser recovery representations remain retryable instead of acknowledged.
Migrations001-004 are unchanged.

Independent whole-change reviewer review_checkout_release found two Important
recovery defects (mutable catalog description and swallowed item-read errors).
Both were reproduced RED and fixed GREEN; the browser incomplete-response
case was also reproduced RED/GREEN. The final candidate source tree is
845571502a0134ae15eac14e912b6425ed34ebef. Full isolated Go: **700 top-level
entries,0 failures** (one intentional subprocess helper skip, exercised by its
passing parent). Both disposable containers were verified removed. Frontend:
**300/300**, including10 mounted checkout regressions. TypeScript, production
frontend build, Go vet, Linux server and migrator builds passed. Windows archive
text checkout converted LF/CRLF; source-tree identity and normalized text equality
are retained in the ignored release proof. No executable change follows this gate.

Logs: logs/checkout-8455-frozen-{go,frontend,build,vet}.log,
logs/checkout-8455-linux-build.log. Proof: releases/checkout-source-proof.json.
Production preflight from this device times out on SSH/HTTPS; this local gate
does not claim a new production release. Direct GitHub access was verified.
FIN003 remains deployed at79ee940 with its prior verified backup/health receipt.
Production-equivalent1000-user/100-RPS certification remains OPEN.

Scope decisions: existing cross-tab cart editing is not certified by attempt
coordination; recovering an order does not discard a changed cart. Post-commit
best-effort ancillary delivery remains JOB-01 work (replays skip duplicates).
Explicit browser storage removal/eviction loses the opaque identity; authenticated
order history remains available. Provider/deployment/hardware acceptance requires
its separate gates. Deferred Minor: compressed browser-test formatting.

Current counts: **22 DONE,5 Checkpoint OPEN,27 TODO** of54. DONE means the
local gate above; deployment, historical reconciliation and capacity are distinct.
