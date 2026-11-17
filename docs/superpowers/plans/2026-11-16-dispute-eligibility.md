# FIN-005 Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans (or the parent's already authorized execution method). This is one end-to-end task; its internal sections are not separate release units. Preserve the user's opt-out of repeated routine approval/method questions.

**Goal:** Make dispute verdicts, refund obligations and order release eligibility durable and consistent.
**Architecture:** Immutable admission evidence and decisions feed one transaction-aware eligibility component. FIN-003 reserves refunds and the existing canonical release helper/worker performs seller release.
**Tech Stack:** Go/database/sql/PostgreSQL, React/TypeScript, existing node test runner.
**Spec:** `docs/superpowers/specs/2026-11-16-dispute-eligibility-design.md`.

## Global constraints

- Add `202611160002` only; no applied migration edits. Verify `202611160001` SHA256 remains `7b5df7ac1a9f0eef3c580f00a430e7aad5517550d35c38e6adb215603acf4116`.
- Preserve FIN-001/003/012 amounts, lock order, balances, original split and source evidence. No provider/real-money calls.
- Exclude `docs/fa/` and `backend/internal/networkreward/` from reads, commits, frozen validation archives and deploy.
- Preserve worker limits: 100 candidates, 300ms each, existing job deadline/retry/lease. No service changes.
- Use guarded private test schemas, never public fallback or production DSNs. Full tests run only on a frozen tracked archive excluding protected paths.
- Authorized execution finishes change, validate, commit, push, `deploy.ps1 -BuildOnServer`; parent owns release arguments, exact disk checks, backup/health and production verification.

## Review focus

1. A stale hold snapshot must not overwrite a later carrier failure/cancellation: section B race test.
2. A response lost after commit must replay without another debit: section C HTTP/DB test.
3. A late return filing after its pre-lock check must not race release: section B live-writer race.
4. A hundred blocked candidates must not starve the next eligible order: section D bounded recovery test.
5. Empty live data and old historical decisions must not appear as mock successes: section C browser tests.

## Task 1: Durable dispute verdict through guarded release and truthful live UI

Complete all sections before the root's final review, validation, commit, push and deployment. Schema, resolution, eligibility/recovery and HTTP/UI must ship together. In particular, the resolution service cannot pass its GREEN gate until the shared guards and projection exist; the worker must call those same guards.

### Shared interfaces and dependency order

```go
type DisputeResolutionInput struct {
    Action, ResponsibleParty, ActorUserID string
    ExpectedClaimAmount *int64
}
func (r *FinanceRepository) ResolveDisputeWithOutcome(ctx context.Context, id string, in DisputeResolutionInput) (domain.DisputeOutcome, error)
func releaseClosedReturnWindowTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error)
func reconcileDisputeEligibilityTx(ctx context.Context, tx *sql.Tx, orderID string) (string, error)
```

Define `domain.DisputeOutcome` from the design: immutable decision identity/action/party/time/amount and replay flag, actual order status, current seller-release state/reasons and existing `OrderRefundSummary`. Share the existing bounded refund projection, with its order-level scope explicit. `GetDisputes` and POST resolve expose this same projection. Build evidence/schema and outcome types first (A), shared guarded restoration/release then atomic resolver (B), HTTP/UI (C), and recovery candidate selection/reconciliation (D); run the complete GREEN gate (E). Preserve `ResolveDispute` as a strict compatibility adapter for existing callers.

Tx helpers consume the already-held canonical financial prefix; no nested transactions or lock reordering. `releaseClosedReturnWindow` remains a transaction-owning wrapper. Resolver, worker and reviewed reconciliation all use the shared helpers. HTTP obtains ActorUserID from existing authenticated finance capability context, never the request body. ExpectedClaimAmount is optional compatibility/concurrency input, not the source of financial authority.

### A. Schema, evidence and RED contract coverage

**Files:** create `backend/migrations/202611160002_dispute_eligibility.up.sql` and `.down.sql`, `backend/internal/repository/dispute_resolution.go`, `dispute_resolution_integration_test.go`; modify `finance_repo.go`, `backend/internal/domain/models.go`, private finance/refund fixture helpers as needed.

- [x] RED: Add private-schema tests for captured pre-hold facts; invalid status rollback; stored claim used when expected amount omitted; mismatch/zero explicit expectation rejected; every existing action and party alias; contradictory pair rejected; invalid/overflow amount; multi-source conservation under existing one-active-payment/fully-reserved extra-capture policy (beyond-available claim rejects atomically; valid remaining claim leaves compensation intact), with no fabricated positive split; authenticated actor; one decision/debit/obligation set after duplicate replay; different verdict conflict. Run `go test ./internal/repository -run 'TestFIN005' -count=1` from backend in the isolated fixture environment; confirm assertion failures, not setup failures.
- [x] Implement and barrier-test the spec's additive transaction-local mixed-version fence and deferred evidence/decision constraints. Drain old writers under explicit bounded migration locks, reject absent markers before parent-lock acquisition, set the marker only inside the new canonical guarded writers, preserve unrelated updates and existing return/refund semantics. Cover pre-lock, queued/post-migration old writers, valid new writes and missing-evidence rollback. No pooled/global opt-in or unrelated service changes.
- [x] Implement schema tables/indexes, immutable guards (including update/delete rejection), and evidence capture inside CreateDispute's canonical lock. Keep admission states unchanged. Migration does not backfill financial facts. Down refuses populated evidence.
- [x] Fault-test every distinct write boundary with transaction-local fixture triggers: evidence insert/order hold, provisional verdict, each obligation, adjustment, immutable decision, compatibility update, transition record. Assert rollback preserves pending verdict, original status, balances and zero new effects. Test commit-success/response-loss replay separately.
- [x] Add migration coverage: install over prior schema, second application via migration runner, immutability rejection and populated-down refusal. No public-schema weakening to satisfy old fixtures. Atomic outcome tests remain RED until section B implements their shared dependencies.

### B. Shared restoration/release guards, atomic resolver and competing writers

**Files:** create `backend/internal/repository/dispute_eligibility.go`, `dispute_eligibility_integration_test.go`; modify `order_repo.go` canonical release, return admission and return state transitions; update impacted finance/notification/return fixtures and tests only as required by real schema contracts.

- [x] RED: Recreate the backlog's `TestAuditFinanceRejectedDisputeReleasesSeller` probe (not found under that name in current tracked backend) with reviewed confirmed evidence and expired window; assert one release pair and flag. Add unexpired/null window; shipped/unconfirmed restoration; another pending dispute; each active return state; rejected/cancelled return; succeeded return with reversals; pending/unknown/failed refund; historical refund lacking entitlement; already released consistent/inconsistent history.
- [x] Validate the spec's exact per-return successful obligations and captured item/reversal/linked-ledger set for refunded returns; test missing, duplicate, mismatched, zero-component and non-dropshipper cases. Add narrow append-only reversal/linked-evidence protection in160002. Follow the builder report's changed-field fence scope and NOWAIT whole-transaction drain/retry rules; no unrelated refund-event or note-update fencing.
- [x] Implement evidence-based restoration, hold queries and common release guard. Preserve timestamps and money fields. Reconcile current status/hold timestamp before using snapshots. Add durable needs_review reasons where proof is insufficient. No new confirmation or partial-refund allocation.
- [x] Implement atomic resolution using `reserveOrderRefundTx` and the now-defined shared eligibility/release helpers; use exact action whitelist, normalized party and actual locked amount. Preserve existing adjustment amount/target rules. Store decision before commit with compatibility fields and durable eligibility result. Legacy resolved rows cannot manufacture a new authorized refund. Implement the shared current outcome projection for both POST and GET using existing refund summary arithmetic.
- [x] Put narrow return hold writers under parent-first ordering; re-read return admission status/window/release after lock. Preserve existing return item/QC/reversal behavior. Enumerate all hold-opening paths and assert they acquire the same parent guard; closure paths must not invert child/order locks.
- [x] Add real transaction races, with barriers rather than sleeps: verdict versus release; two identical/different verdicts; final other-claim closure versus release; return filing versus expiry/release; refund reservation/execution versus release; carrier update versus evidence restoration; payout versus released/refunded balance. Assert deterministic single effects, preserved debt and no over-admission. Test independent orders to catch accidental global locking.
- [x] Inject failures after status restoration, release flag, first/second ledger row, referral update and transition append; assert all rollback together. Require no deadlocks/leaks and bounded contexts in the section E validation gate.

### C. Authoritative HTTP and live UI

**Files:** modify `backend/cmd/server/wms_and_finance_handlers.go`, `src/services/financeApi.ts`, `src/components/finance/SupportDisputesTab.tsx`; create `backend/cmd/server/dispute_resolution_http_test.go`, `src/services/financeApi.disputes.test.ts`, `src/components/finance/SupportDisputesTab.browser.test.ts`. Update dispute notification wording/tests where currently implying paid money.

- [x] RED: HTTP tests cover omitted amount using stored claim, body operator spoof ignored, invalid/contradictory action, 404, identical replay 200, conflicting verdict 409, missing authentication, post-commit projection failure and GET recovery. Assert one obligation/ledger effect across lost-response retry.
- [x] Wire typed validation/errors/projections. Remove handler `ApplyDisputeRefundEffect` invocation; compatibility method may remain for audited older callers. Fail closed on projection read errors; distinguish an uncertain response from an uncommitted decision. Never report `orderRefunded` from reservation alone or expose source/provider secrets.
- [x] RED browser tests: empty live list/count zero; live GET error with retry; successful seller win held/awaiting_window/released; customer refund pending/succeeded/failed/unknown; legacy resolved review; double-click disabled; lost POST response then reload; no call to local reducer or mock adjustment preview in live mode.
- [x] Implement screen changes using existing components/copy style. Remove all unconditional immediate-release/refunded wording and type Promise<any> response. Mock branch retains explicit mock behavior.

### D. Bounded recovery and reviewed historical evidence

**Files:** modify `order_repo.go` candidate selection/delegation; create `backend/internal/repository/dispute_reconciliation.go`, `dispute_reconciliation_integration_test.go`, and a read-only audit SQL/runbook under `docs/superpowers/plans/2026-11-16-fin005-reconciliation.md`. Update the exact FIN-005 backlog status only after evidence is complete.

Reviewed reconciliation takes dispute ID, evidence reference and actor, acquires canonical locks and calls the shared helpers. No unauthenticated public endpoint or automatic bulk backfill. Report defaults to read-only. Existing worker processes deferred proven eligibility with unchanged budget/retry API.

Historical evidence enters through an explicitly reviewed reconciliation record, not an inferred migration backfill or a request claiming a prior status. The read-only report must cite retained authoritative order/audit records establishing the pre-hold confirmed state, actual confirmation time, original recorded window end and hold linkage; preserve their source identifiers and reviewer/actor in `dispute_order_evidence` (`reviewed_legacy`). A confirmed timestamp alone, present settings, dispute date or elapsed duration cannot establish a missing status/window. Root/domain review accepts that evidence before the guarded application rechecks current order, claims and prior release ledger. Missing/conflicting provenance produces a visible needs_review transition and no automatic release. Tests supply explicit audited fixture evidence; production initial audit has no stuck/disputed orders, so no production repair or historical evidence import is presumed.

- [x] RED: Two pending legacy disputes resolve on separate transactions; final seller-win closure becomes recoverable; other return closes days after verdict; worker outage/restart; 100 currently held rows ahead of an eligible row; expired/null/missing evidence; duplicate reconciliation; ledger/flag disagreement. Assert stable due-work progress and no synthetic window, duplicate credit, or money rewrite.
- [x] Implement due-candidate filtering and deferred restoration recovery using immutable decisions/evidence. Index actual predicates. Review-only cases are visible in audit/UI without hot-loop retries. Preserve limits and stop behavior.
- [x] Add production-shaped migration fixture with one resolved legacy dispute, zero disputed orders and returns requested=1/refunded=1/cancelled=2; migration leaves all old verdicts, orders, amounts and history unchanged. Add a separate proven stuck fixture and an ambiguous stuck fixture; only reviewed proven input repairs.

### E. One GREEN and root-owned release gate

- [x] Run focused repository tests: `go test ./internal/repository -run 'TestFIN005|Test.*(Refund|Dispute)' -count=1`, including worker/reconciliation, migration and fault tests above. Run relevant races with `go test -race ./internal/repository -run 'TestFIN005' -count=1` in the frozen isolated validation environment. Require all focused assertions to pass, including the real live-writer races; a missing fixture schema is a failure to fix, not a skipped test.
- [x] Run HTTP tests: `go test ./cmd/server -run 'TestFIN005|Test.*Dispute' -count=1`; frontend tests: `node --import tsx --test src/services/financeApi.disputes.test.ts src/components/finance/SupportDisputesTab.browser.test.ts`; and `npm run build`. Inspect rendered narrow/wide screen through existing browser fixtures without provider calls.
- [x] Root freezes tracked source with protected paths excluded and runs full backend/frontend tests and build once after all sections are wired. Record the exact revision/archive hash and counts; do not reuse FIN004 evidence. No production tests/writes.
- [ ] Root reviews final diff, conservation evidence, migration hashes and the complete schema/backend/worker/API/UI change; commits explicit authorized files, pushes, then runs `deploy.ps1 -BuildOnServer` with supported existing parameters after exact headroom checks. No partial FIN005 deployment or server cleanup. Preserve runtime credentials/backups/services.
- [ ] Root verifies deployed revision, health, migration history, read-only FIN005 audit, release/refund conservation and existing service access. Legacy repair is a separate reviewed evidence application only if actual candidates appear. Report PSP sandbox limitation and remaining review-held business exceptions honestly.

## Self-review

One Task 1 covers evidence, atomic decisions, replay/conflict, competing holds, canonical release, late closure, reviewed reconciliation and truthful UI before root-owned deployment. Shared interfaces precede their consumers, section B supplies the resolver's helpers/projection, and section D reuses the same guards. No tests or product edits were performed while preparing this draft. Policy expansions identified in the spec remain excluded; they must not silently become entitlement changes during implementation.
