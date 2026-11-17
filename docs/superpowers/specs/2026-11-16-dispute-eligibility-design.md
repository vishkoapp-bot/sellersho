# FIN-005: durable dispute decisions and guarded release

Design, 2026-11-16. Baseline c270fcc2025d86946f9314747cb60300b0b67c99. Implements the FIN-005 contract in `docs/superpowers/plans/2026-11-14-correctness-capacity-tasks.txt:1382`. Routine implementation choices below are engineering proposals under the user's execution opt-out, not newly agreed business policy.

## Intent and constraints

A resolved dispute must carry a durable verdict and the money obligation/eligibility implied by it. A seller win must not invent delivery, restart/backdate a return window, erase another claim, or report withdrawal availability before release. A customer win reserves the original-payment refund through FIN-003 in the same transaction; reservation is not payment.

Keep FIN-001 signed balances, FIN-012 integer TOMAN and captured historical amounts, FIN-003 original source allocation, canonical locks, existing return reversals, and bounded release worker. No PSP/provider requests, repricing, new partial-refund policy, new service, or generic workflow engine. Never read/commit/deploy `docs/fa/` or `backend/internal/networkreward/`. All applied migrations are immutable, including `202611160001` (SHA256 `7b5df7ac1a9f0eef3c580f00a430e7aad5517550d35c38e6adb215603acf4116`); add `202611160002` only.

## Approaches

1. Patch the post-commit effect: smallest diff, but leaves decisions and recovery untracked and cannot safely reconstruct lost delivery state. Reject.
2. A small transaction-aware dispute component with immutable evidence/decision records, guarded canonical release, and existing worker recovery: selected engineering approach. More explicit schema, but one financial algorithm and recoverable outcomes.
3. General claim workflow/outbox framework: broader than this contract and duplicates existing refund/worker infrastructure. Reject.

## Current source facts

`CreateDispute` accepts shipped/delivered_unconfirmed/delivered and overwrites status without saving its previous value. `ResolveDispute` already reserves FIN-003 refund liability atomically but trusts a caller amount and does not repair seller-win eligibility. HTTP still invokes a compatibility effect after commit. The UI omits that amount, falls through into local accounting after live success, falls back to mocks on an empty live result, and claims funds were released/refunded regardless of evidence.

Confirmation owns `confirmed_at` and `return_window_ends_at`. Release currently requires delivered_confirmed plus a recorded expired window, without explicit competing-claim guards. Customer returns post immutable item reversals without changing the order's physical status; those debits already enter FIN-001 availability. Return creation currently checks status/window before acquiring its order lock, so it needs an under-lock recheck.

## Records and interfaces

Add one migration with three narrow tables and targeted indexes:

- `dispute_order_evidence`: one immutable row per dispute, order ID, prior order status, prior disputed/delivered/confirmed timestamps, recorded return-window end, prior release flags/timestamps, the exact disputed timestamp written by this hold, captured time, evidence kind (`admission` or `reviewed_legacy`), actor and review reference. Admission inserts it under the order lock before changing status. Nullable historical facts stay nullable. No bulk invented snapshots.
- `dispute_financial_decisions`: one immutable row per dispute, normalized action/party, stored claim amount, authenticated actor, decision time, evidence reference, and decision kind. This is the authoritative idempotency record. Existing `disputes` columns remain a compatible projection.
- `dispute_order_transitions`: append-only, unique business identity per restoration/reconciliation, dispute/order IDs, old/new status, actor, reason, evidence reference, time, and result (`restored`, `already_current`, `needs_review`). An unresolved evidence problem is durable and discoverable even though its verdict is final. A later reviewed correction appends a new transition, never edits the original verdict or money history.

Use a focused `dispute_resolution.go` and `dispute_eligibility.go`; avoid growing the finance repository with another money algorithm. Proposed interfaces:

```go
type DisputeResolutionInput struct { Action, ResponsibleParty, ActorUserID string; ExpectedClaimAmount *int64 }
func (r *FinanceRepository) ResolveDisputeWithOutcome(ctx context.Context, id string, in DisputeResolutionInput) (domain.DisputeOutcome, error)
func releaseClosedReturnWindowTx(ctx context.Context, tx *sql.Tx, orderID string) (bool, error)
func reconcileDisputeEligibilityTx(ctx context.Context, tx *sql.Tx, orderID string) (string, error)
```

All Tx functions require the already-acquired complete canonical financial prefix; they never begin nested transactions or reorder locks. Keep the old `ResolveDispute` signature as a compatibility adapter if repository tests/callers require it. Positive supplied expected amount must equal the stored amount; omitted HTTP amount is not zero authorization. New HTTP rejects malformed/zero explicit expectations. Actor always comes from authentication, never body `operator`.

`domain.DisputeOutcome` contains dispute/order ID, immutable action/party/decision time/amount, replay flag, actual order status, `sellerReleaseState` (`awaiting_delivery`, `awaiting_window`, `held`, `eligible`, `released`, `needs_review`), safe hold reason codes, and `OrderRefundSummary`. GET returns the same current projection; current eligibility/refund progress may evolve without mutating the verdict. `released` means seller accounting release, never bank payout. Scope refund summaries clearly as order-level; do not claim another obligation is this dispute's refund.

## Atomic decision and replay

Resolve order identity read-only, then `lockOrderFinanceTx(..., true)` (order, payments, ordered accounts, product parents, identity revalidation), then lock/revalidate the dispute. Whitelist the five existing UI actions; normalize party aliases with existing mapping and reject contradictory action/party combinations. Seller-win party is a stable rejection code rather than arbitrary prose.

If an immutable decision exists, the same normalized action, party and expected amount returns its decision plus current projection without financial writes; a different verdict returns 409. Actor changes on an authorized retry do not create another decision. A legacy resolved row has no retroactive decision authority: return historical status with needs_review where evidence is insufficient, never reserve/refund again automatically.

For a new customer win use the locked persisted positive claim amount, validate bounds, run `reserveOrderRefundTx(..., "dispute", disputeID, "original_payment", actor, actualClaim, "")`, and preserve the existing responsible-party ledger debit exactly once. Insert immutable decision, update compatibility verdict fields, and record eligibility/restoration state in the same transaction. Failure at any write rolls all of it back. No success can leave refund work absent. Retain FIN-003 failed/unknown liabilities and original source allocation. Remove the HTTP post-commit `ApplyDisputeRefundEffect` call. Notifications remain best effort and say verdict/request recorded, not paid.

For seller win, write the decision and reconstruct physical eligibility from evidence in that transaction. Never set profit flags independently. Call the extracted canonical release Tx helper only when its full guard passes. A post-commit projection error is recoverable by retry/GET; the response must not state that rollback occurred when commit is uncertain.

## Fulfillment evidence and release

Normal new admission retains the current accepted status set. Do not open confirmed/already-released orders to new disputes as a side effect of this fix. Capture the actual locked state, not a pre-lock read. Seller win restores exactly that state and timestamps; shipped stays shipped, delivered_unconfirmed stays unconfirmed. A verdict is not a customer confirmation. Normal confirmation after restoration starts its normal window; the verdict itself creates none.

For historical confirmed evidence, require a provable confirmation and its original recorded window. Preserve both timestamps. Expired valid windows release once; unexpired windows wait; missing/conflicting evidence becomes needs_review. Do not infer confirmed status solely from a legacy `delivered` label or settings. Before replacing delivery_disputed, compare the hold timestamp/evidence to the current row. A newer carrier failure, cancellation, delivery update, or other transition cannot be overwritten by an old snapshot. Keep stronger current physical facts; conflicting evidence receives review.

Other pending disputes continue to hold the order. If restoring a disputed order would erase a distinct unresolved hold, defer restoration with a durable transition reason. The worker must revisit it after the final claim closes. Restoring known physical status for a refund verdict must not independently authorize seller profit; unknown financial consequences stay review-held.

Canonical release rechecks under the order lock: confirmed physical state; non-null valid expired window; not already released; no pending dispute; no active customer return (`requested`, `approved`, `shipped`, `received`); no unresolved/failed/unknown refund obligation or review hold. Unknown return states fail closed. Rejected/cancelled returns cease holding. Completed customer returns cease holding only with their FIN-003 refund result and established return reversal evidence; release the existing recorded gross profit once and let FIN-001 net the existing reversal, never subtract it twice. A completed non-return refund whose seller entitlement is not established remains needs_review rather than minting a partial-profit rule.

The release helper keeps its current profit/referral ledger writes and idempotent flags in one transaction. Every writer that can create/reopen a hold serializes on the parent order first and revalidates after lock acquisition. Return admission must recheck status/window and release state under that lock. Migrate narrow terminal return transitions to the same ordering so closure/recovery races are deterministic; do not alter inventory/QC semantics.

## Recovery and reconciliation

Use the existing delivery-release worker, preserving 100 candidates, 300ms per candidate, job deadline, lease/retry/continuation behavior. Include deferred seller-win restoration candidates whose evidence exists and blockers have closed, even while status is still delivery_disputed. Exclude currently blocked/review-only candidates from the bounded due scan so they cannot starve eligible work. Recheck all predicates under lock. Missing windows are review work, not hot retries.

Migration is schema-only. A read-only reconciliation report lists resolved seller wins still disputed, evidence, other claims, release flags, existing ledger effects, and proposed transition or review reason. Initial production audit found one resolved legacy dispute, zero pending/disputed/stuck seller-win orders; no repair is expected. Re-run before deploy. Reviewed repairs use the same locks/guard and append audit transitions. Reconcile any prior release against its ledger before enabling automated restoration; inconsistent release flags/ledger are review-held, never blindly cleared or credited again. Additive down migration must refuse to discard populated financial evidence.

## UI

Keep the current screen and Persian style. Live mode uses only the server list, including empty/error states and counts. Local reducer and adjustment preview are mock-only. Disable a dispute's buttons during submission, consume typed outcome, refresh authoritative results, and retain an explicit unknown/error state after uncertain transport failure. Seller-win copy says claim rejected and shows waiting/held/released based on outcome; customer-win copy distinguishes refund requested, pending, succeeded, failed and needs review. Remove all unconditional instant-release and refunded wording, including button labels and historical resolved cards. A reload must preserve the same meaning.

## Material business questions and boundaries

No owner answer is required for the core seller-win repair, durable refund reservation, UI correction, or reviewed recovery. Two potential expansions are deliberately excluded: admitting new disputes after delivery confirmation/release, and automatically releasing seller profit after a completed non-return customer-win refund where existing posted effects do not prove the entitlement. Source does not establish a general partial-profit rule for that second case. If encountered, preserve a visible durable needs_review hold; obtaining a business rule is necessary before that specific money release, not before independent FIN-005 work.


## Mixed-version deployment safety (root engineering ruling)

The existing deploy script migrates while the old backend still serves, then
replaces the binary. Go-only guards cannot close this interval. Add narrow
transaction-local FIN005 writer opt-in fencing in the new additive migration for
dispute admission/verdict changes, order release-flag activation, and customer
return admission/state changes. Old incompatible mutations fail closed, including
already queued writers; unrelated reads and updates remain compatible. Set the
marker only in new canonical guarded transaction paths, never globally on pooled
connections. The marker is a version fence, not authorization or financial proof.
Require actual admission evidence and immutable decisions through deferred
constraints at commit. Reject an absent marker before any trigger tries to take
parent locks, avoiding old child-first writer inversions. New writers still own
the full canonical financial prefix before child work.

Install the constraints/fences atomically after explicit bounded drain locks in
old-writer-compatible order. Preserve existing migration timeouts. Tests must
exercise old transactions before and after migration lock acquisition, queued old
writers after commit, valid new writers, rollback on evidence absence, unrelated
writer progress, and existing return/refund paths. A code rollback keeps these
incompatible mutations held until compatible roll-forward; never down populated
evidence or restore a database over newer orders.


## Completed-return evidence and bounded drain details

For a terminal `refunded` return, validate per-return identity and FIN003
`customer_return` / `return_wallet_v1` obligations, their latest event_no states,
and principal equal to captured accepted-item totals. Order-level refund sums or
the historical status alone are insufficient. Shipping remains separate. Check
the complete applicable positive product_cost/seller_profit reversal set against
captured wholesale_cost/seller_margin, quantity, exact account identities and
linked ledger rows. Zero components need no event; non-dropshipper stores need no
seller-margin reversal. Missing, duplicate or inconsistent proof is review-held.
Existing return events are not database-immutable: add append-only protection for
relied-on reversal rows and protect linked ledger evidence in160002. Do not
rewrite their older migration or repair old facts silently.

Narrow fences cover changed protected fields only: dispute admission, identity,
claim and verdict/delete; customer-return admission, identity, status/delete;
order release activation and entry into disputed hold. Unrelated note/tracking
updates and existing FIN003 refund outcome/event machinery remain compatible.
Before fence installation acquire required ACCESS EXCLUSIVE locks NOWAIT in
parent-first order (orders, disputes, customer_returns, then protected reversal/
ledger evidence relations). Any contention aborts the entire migration and
releases earlier locks; retry the whole runner, never continue a partial fence.
No internal unbounded wait/retry and no unrelated supplier-event locks. Test
both deliberate contention rollback and successful install after old writers
finish, including readers that discover children before financial locks.

Source correction during implementation: current orders has no delivered_at column. Do not query/write such a field or use a nullable placeholder as fulfillment authority; preserve the actually recorded status, disputed_at, confirmed_at, original window and release facts.

Carrier-only physical failure is not dispute admission: preserve its independently guarded/marked hold and actual timestamp without synthetic dispute evidence. Deferred admission-evidence linkage applies to actual dispute rows; stale dispute restoration cannot replace a newer carrier hold.

Ruling: Preserve established responsible-party adjustment target: initial ledger target_account uses the recognized supplied party label/code (trim transport whitespace; empty fallback canonical action party), target_type normalized. Immutable decision/replay uses canonical party; equivalent alias retry has no new debit. Do not invent supplier/store payable allocation in FIN005. Unknown/contradictory aliases reject. Test initial target and alias retry. This compatibility choice preserves existing accounting scope rather than introducing a new entitlement/counterparty policy.

Ruling (supersedes prior historical two-paid-source fixture suggestion): actual FIN003 one-active-payment index and compensation-capture constraints prohibit two unreserved paid sources for one current order. Do not weaken them or fabricate verified receipts. FIN005 tests an independently verified extra capture remaining fully reserved, atomic rejection of a claim beyond original available principal, and a within-remaining claim consuming only original source with compensation intact; assert captured/reserved/remaining conservation and canonical replay across both source facts. Retain FIN003 multiple-capture tests. A positive single-dispute split over two unreserved sources is not admitted by current checkout schema; this is a documented existing limitation, not a new funding policy or skipped required behavior. Cost if initial suggestion had been followed: invalid fixture or weakening actual money guards; rejected before adoption.
