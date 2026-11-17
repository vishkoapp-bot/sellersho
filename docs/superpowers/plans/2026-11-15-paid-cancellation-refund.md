# Paid cancellation refund implementation plan

> Execute inline with superpowers:executing-plans. The user's standing workflow
> is validate,commit,push,deploy.ps1 and continue. No implementer delegation.

**Current status:** Local acceptance passed on the frozen source below; publication follows the standing commit/push/deploy workflow.

**Goal:** Preserve paid facts and a durable, capped refund obligation on cancellation.
**Architecture:** Immutable funding and principal reservations under the existing
order/account guard; explicit outcomes and honest customer state. Provider I/O is
outside database transactions and real original-payment execution belongs FIN-010.
**Tech stack:** Existing Go,PostgreSQL,Redis,React and integer TOMAN helpers.
**Spec:** ../specs/2026-11-15-paid-cancellation-refund-design.md (FIN-003 authority).

## Global constraints

- Base4524ee3 is pushed/deployed healthy; storage work is complete, do not redo it.
- Exclude docs/fa and paused backend/internal/networkreward from commit/deploy.
- Applied migrations001/002/003 cannot change. Add004 and preserve all data.
- Owner selected original-payment split: genuine wallet principal to the same
  wallet, gateway principal to the same transaction. Wallet checkout stays disabled.
- Never call a live provider or print credentials. Production fee flag stays OFF.
- No simulated historical paid row automatically becomes real captured cash.
- No SQL lock across HTTP; CORE order/payment/account/product/work/inventory order.

## Review focus

1. Verified funds rejected by Redis/expiry/cancellation must still have a durable
   receipt and exactly one capped compensation obligation.
2. A second refund route/partial prior refund cannot spend principal already
   reserved by pending or unknown work, even for a different destination.
3. Historical source ambiguity, multiple successful payments and currency/amount
   mismatches must not become invented wallet credits or lost verified cash.
4. Crash after a committed receipt/claim or unknown provider response cannot cause
   duplicate attempts, released holds, false success or silent loss.
5. Ownership/child-shipment rejection, post-lock identity changes and private old
   fixtures cannot bypass the financial prefix or create schema fallbacks.

## Task1: Reproduce and add preserved funding/principal evidence

Files: new backend/internal/repository/refund_principal.go,
refund_obligation_repo.go,refund_obligation_integration_test.go;
new backend/migrations/202611150004_order_refund_principal.{up,down}.sql.

- [x] Read actual payment callback, CancelOrder, expiry writer, return wallet,
  admin refund and dispute monetary sources before choosing schema details.
- [x] Write TestFIN003PaidCancellationPreservesTenMillionLiability against
  canonical operationsDB and actual paid/captured stock; observe current paid
  accounting lost and missing durable liability (RED, not a compile-only failure).
- [x] Add conserved immutable funding facts, source-linked claims, unique stable
  business identities, checked amounts/currency and explicit reconciliation state.
- [x] Add SQL identity/cap/outcome guards and non-destructive up/down/up rehearsal.
- [x] Test unknown historical sources, prior documented partial refunds, zero
  principal/unpaid cancellation, replay and changed source/payload identity.
Expected: positive canonical paid10m is preserved with exactly10m liability/credit;
no new money from unpaid or ambiguous old history; migration preserves all rows.

## Task2: Connect cancellation, verified callback and expiry atomically

Files: order_repo.go,operations.go/expiry owner (resolve exact path via rg),
payment_handlers.go,refund_obligation_repo.go and typed transaction contract if needed.
Interfaces: CancelOrderWithRefund -> OrderCancellationResult{OrderID,Status,Refund};
RecordVerifiedPaymentReceiptTx and ReserveOrderRefundTx use caller-owned ordered
transaction; confirmed verification is recorded before Redis hold rejection.

- [x] Write cancellation/callback/expiry and hold-rejection regressions first;
  include each winner, duplicate callback, mismatched amount and lost response.
- [x] Preserve fulfillment-child rejection and no inventory resurrection/capture
  during compensation. Preserve paid facts rather than overwrite them as cancelled.
- [x] Add error-only CancelOrder wrapper for existing callers; actual customer
  handler returns summary. Read/replay uses resolved ID and principal source.
- [x] Test crash after durable verification receipt and after cancellation commit,
  then replay/reconcile once. Reject changed account/source plans after guards.
Expected: real verified funds remain accounted for with one obligation regardless
of cancellation winner; stock and source evidence conserve across every branch.

## Task3: Honor shared refund principal and explicit outcomes

Files: refund_obligation_repo.go/new focused refund service;
existing RefundCustomerReturn,wr69AdminPaymentRefund and dispute monetary writers;
new focused outcome/reconciliation tests, optional bounded internal worker wiring.

- [x] Reproduce competing cancellation/admin/return/partial-refund reservations.
- [x] Route monetary claims through one pool under the same order/customer guards.
  Keep shipping reimbursement separate and preserve existing return policy.
- [x] Implement pending/succeeded/failed/unknown events, attempts and bounded
  durable retry/reconciliation with deterministic local provider fake.
- [x] Credit only explicitly selected known wallet refunds atomically with source
  claim, journal and outcome; never issue original and wallet for the same amount.
- [x] Missing original adapter remains pending. Never fake PSP success or resend
  unknown attempts. Validate confirmed source, operation key, currency and amount.
- [x] Prove rollback, duplicate outcome, failed/unknown holds, crash recovery,
  direct SQL cap/source/outcome enforcement and no unguarded legacy refund route.
Expected: all principal reservations/outcomes sum within captured source; pending/
unknown remain reserved and all confirmed effects occur once.

## Task4: Customer state, audit and release

Files: order_handlers.go,domain.Order/read projections,orderApi.ts,
CustomerOrdersView.tsx and mounted browser test; new read-only historical audit;
this plan/spec and public54-task progress ledger.

- [x] Test current API ownership/roles and mounted cancellation response/state
  first; pending/unknown is retained on reload and rejected cancellation is honest.
- [x] Expose only safe source-free summary fields; use bounded/batched reads.
- [x] Add read-only legacy audit with explicit uncertain rows, no automatic cash
  correction or mutation of simulator history.
- [x] Independent finance/code review of the complete stage; one TDD fix pass.
- [x] Exact frozen isolated PostgreSQL/Redis full suite, affected browser tests,
  frontend build,vet/Linux build; all failures fixed with retained evidence.
- [x] Update status only for met local acceptance; preserve FIN-010 sandbox,
  historical reconciliation and hardware qualification OPEN.
- [ ] Explicit commit,push,deploy.ps1; verify backup/migrations/root/health/marker,
  then continue the remaining supplied tasks without another permission loop.

## Initial ledger

Read-only facts and policy uncertainties are in the design; no FIN003 code yet.
Owner destination choice received: original-payment split. Independent read-only
builder check review_fin003_design found nothing important left. No live PSP calls
or invented return of money. Settle implementation details locally, record rulings
and preserve the approved source contract.


Task1 BASE4524ee3b342e1f8e1701049da99cebf7c75e1a3d. Existing isolated codex branch
verified, only paused networkreward was untracked before this design. Execution
inline with TDD; no implementer delegation. Shared interface preflight: verified
receipt recording must precede Redis admission, and cancellation/expiry and all
principal spenders must use the same order/account guard. Wallet spending is
currently disabled and is not implemented by this task. Provider execution is
FIN010; an unavailable adapter is not a failed or succeeded refund.


Task1-2 progress2026-11-15 ~05:20 local. FIN003 stillUNCOMMITTED. New004 request
snapshot/verification events, receipts segregate gateway/simulated, capped claims,
legacy adminpartial/oldreturn/refundedmarker review holds. Cancel preservespayment
facts; lateMarkPaid commits money+compensation but stillErrExpired/noresurrection.
BeginPaymentVerification commits stable inquiry before providerI/O, freezes source,
RecordPaymentVerification persists receipt beforeRedis and atomically handles
alreadycancelled order. Unknown and mismatch observations retained, no fakecredit.
HTTP simulator calls boundary; no livePSP. Expiry nowoneorderTx financialprefix,
preservesfacts and reservations; worker cleans committed results despite latererror.
MissingRedis503 retains receipt untilretry/expiry. Missinghold/stock compensation
uses guardedRejectVerifiedPaymentAdmission; capturewinner cannotbecancelled.
GREEN10/0 callback-cancel-after; GREEN14/0 verification-source-after; focusedHTTP
callback-simulator-after passes. Expiry/admission18test run had1fixtureissue unique
RRN thenreference length; correctedhelper nowunique<=30chars. Finalfocused
capture-retry-after-valid-reference1/0. REDs retained for eachactualfailure.
IMPORTANT stillrequired: sharedspenders/admin/return/dispute+outcomes/walletjournal,
realDB HTTP/expiry/callback race and recovery, SQLbypass/rollback,migrationpopulated
refusal,customerUI/readsummary,audit,freshreview,exactfullgates,commitpushdeploy.
Do NOT ship core beforeallspendersadopted. New004 mutable; applied003 immutable.
Current public20DONE/5OPEN/29TODO, FIN003 remainsTODO. Production4524ee3healthy.

## Current acceptance evidence (supersedes earlier checkpoints)

Owner policy is original-payment split. Task1-3 and customer/admin state are
implemented. Independent core review found three gaps (old PSP claims, extra
money on legacy fulfillment and expiry starvation), reproduced by five failing
canonical tests and then fixed, five passing. Whole-stage review found a callback
response TypeScript mismatch and a late compatibility attempt blocked by the
ordinary active-payment index; both reproduced and fixed. The reviewer confirmed
no material issue remains in that delta. Compensation classification never proves
real gateway funding; even a recorded legacy late capture must retain full source
principal in the same transaction, enforced by a deferred SQL constraint.

Evidence: financial/return compatibility84 tests,0 failures in
logs/fin003-finance-compat-current.log; latest full FIN003 feature gate48 tests,0
failures in logs/fin003-release-review-after.log; TypeScript exits0 in
logs/fin003-typecheck-after.log; mounted customer/admin browser tests2/0 in
logs/fin003-refund-browser-after.log. Read-only historical audit and explicit failed
projection are tested against canonical PostgreSQL. Populated004 rollback refuses
evidence loss; empty down/up and unchanged historical paid rows are covered.

Original-payment execution remains unavailable until FIN-010. The local provider
fake proves lost-response inquiry, no second transfer, exact outcome binding,
unknown/failed holds and account locks released before I/O. The production adapter
is not simulated success. Wallet checkout remains disabled; unproved historical
wallet principal or PSP/return/dispute projections require review. New wallet
refund credit and matching immutable success outcome must commit together.

The ordinary pending/paid unique index remains for checkout; only verified or
legacy-recorded compensation captures may be classified separately. Full capped
principal must be reserved before commit, and legacy/simulator facts cannot drive
real provider execution or wallet credit. Earliest real receipt funds fulfillment;
later real receipts reserve their own extra principal without a second stock
capture. Uncertain legacy fulfillment funding gets a source-bound review hold.

Historical reconciliation and production-equivalent capacity certification remain
OPEN. Production fees stayOFF. Final broad gates, commit,push and deploy.ps1 follow
before this task's local completion status is recorded.

Additional replay regression: a later eligible payment history row must not change
an already accepted admin request's source or result. The request fingerprint now
uses caller target/amount/reason; the persisted result freezes its accepted source.
Actual409 reproduction is retained in fin003-admin-source-replay-before.log; the
three relevant tests pass in fin003-admin-source-replay-after.log. Independent
narrow review confirmed the fix. This changes no schema or frontend input; final
full-source backend validation follows before local completion or deployment.

Final local release gate: frozen tree
`e6fe0a059613d06854129838eee514e9c21b5a8c`,689 backend entries/0 failures,
owned container cleanup verified; frontend290/0, TypeScript/build,vet/Linux builds
pass. One intentional migration helper skip is executed by the passing parent
process tests. Final commit adds only these completion notes to the exact tested
backend/src/scripts trees. See the main progress ledger for retained logs.
Production deployment receipt is recorded separately after deploy.ps1 completes;
no migration or production success is inferred from the local gate.
