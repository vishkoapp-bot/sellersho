# Paid cancellation preserves payment and refund liability

Authority: FIN-003 in ../plans/2026-11-14-correctness-capacity-tasks.txt.
The user explicitly authorizes continuous implementation, validation, commit,
push and production deploy.ps1 after each stage. The detailed backlog is the
approved work; do not introduce a repeated design-permission gate. This design
records implementation rulings, not additional approval or a completed feature.
Base4524ee3 is pushed/deployed healthy (storage639/0); production storage fees OFF.

## Outcome

Cancelling a paid10m-Toman order preserves the captured payment facts and creates
exactly10m of durable refund liability/confirmed credit, less already reserved or
refunded principal. Fulfillment cancellation and refund completion are separate.
No customer response claims bank/wallet money moved merely because order status
changed. Unpaid/no-funding cancellation creates no refund. Dispatched children
retain the existing rejection policy and roll back every side effect.

## Current facts

CancelOrder sets both order status and payment_status to cancelled; it records no
refund. It serializes on order -> payment -> account -> product/SKU -> inventory.
MarkPaymentPaid rejects cancelled/expired orders before recording verified funds.
The HTTP callback verifies at the gateway but checks Redis holds before calling
MarkPaymentPaid, so verified money may be lost from the accounting state.
Customer-return refunds credit customer_wallets directly; shipping reimbursement
is separate from captured product principal. Admin wr69AdminPaymentRefund only
books ledger credit (reason prefix استرداد وجه سفارش:) and may mark transactions
refunded. It does not actually call a PSP. The old refunds table has no unknown
outcome and no immutable source/destination/currency/principal reservation model.
Wallet CHECKOUT is explicitly disabled by handleCreateOrder; keep it disabled.
Do not invent wallet spending/split checkout in this task. Historical nonzero
wallet_amount without debit evidence requires review, never a guessed credit.

## Refund policy

The owner explicitly selected original_payment_v1: return each genuine wallet
portion to that wallet and each gateway portion to the same original payment.
Destination is captured immutably per obligation before any money moves.
Original gateway principal becomes pending original-payment refund, not an
immediate wallet credit. Genuine known wallet principal returns to that wallet.
The existing return-to-wallet policy remains separate from cancellation policy.
Internal service tests also exercise an explicitly selected wallet destination.
Real original-payment execution/merchant operation is FIN-010 (Zarinpal selected
by the user), not a fake implementation in this task. No live provider calls,
charges, messages or credentials in chat. Unconfigured provider keeps requests
pending, not succeeded/failed. An unknown attempted result stays reserved.

## Funding and principal boundary

Record immutable successful verification receipts by payment ID, order, customer,
TOMAN amount, source and business identity. Validate success, amount equality,
positive bounded integer money and source binding. Accept SDK results only after
backend verification; raw callback payload cannot certify a receipt. Existing
paid records lacking new receipt evidence are preserved as recorded/legacy facts,
not retroactively certified real captured cash by a migration.

A successful provider verification must be durably recorded before a Redis hold
rejection. Do not hold a SQL transaction across the provider request. Under the
order/account prefix, receipt recording and an already-cancelled order's refund
obligation commit atomically. If cancellation happens after receipt commit, its
same guard sees and reserves that receipt. Pending fulfillment may still capture
stock exactly once; compensation never resurrects a cancelled order or mints stock.
Adopt the existing expiry cancellation writer as well: verified funds must not be
lost merely because expiry won the order guard. Preserve money facts separately
from fulfillment admission and expose pending refund state to the customer.

All refund reservations share the order guard and a captured-principal pool.
Bind each claim to currency, source payment/funding evidence, amount, destination,
policy version and unique server-derived business identity. Amounts come from
server facts, never customer cancellation input. Claims in pending/unknown/
succeeded states consume principal; retries reuse the same claim. Confirmed
failure is recorded explicitly; it does not silently release an ambiguous hold.
A business key cannot change its source, amount, currency or destination.

Prior return/admin/dispute/partial refunds must be counted once. Reuse documented
immutable events where available. Existing admin bookkeeping is an existing
unknown claim, not proof of a PSP payout. If old source/amount cannot be proved,
retain an explicit reconciliation hold rather than invent exact refunded cash.
Do not sum shipping reimbursements into captured-order principal. Multiple paid
attempts, duplicate callbacks and old refunded statuses need source-level scrutiny;
never infer one source or silently cap away an actual verified extra payment.

Customer-return wallet credit, legacy admin refund recording and dispute monetary
writers must respect new reserved principal under the same order guard. New
cancellation claims cannot coexist with an unguarded second refund route. Keep
wallet journal/credit, claim outcome and state change in one SQL transaction.
No schema-absence monetary fallback. Upgrade meaningful private financial fixtures
where necessary; do not skip their successful custody/money assertions.

## Outcomes, retry and UI

States: none (summary only), pending, succeeded, failed, unknown. Immutable outcome
and attempt evidence drives current projections; preserve uncertain attempts.
A bounded service/reconciler uses stable operation identity, checks confirmed
amount/currency/source and applies a confirmed outcome once. No blind resending
unknown provider requests or switching their destination. A local deterministic
fake exercises lost response, replay and crash recovery. The production original
adapter remains unavailable until FIN-010, with honest pending state.

CancelOrder remains an error-only compatibility wrapper. Add
CancelOrderWithRefund(ctx,idOrNumber,customerUserID,isAdmin)(OrderCancellationResult,error),
returning resolved order ID, cancelled fulfillment and OrderRefundSummary.
Customer-owned/admin reads expose state, TOMAN amount, destination and need for
review; no tokens, raw provider data, full bank/card information or other tenants.
Actual UI shows pending/unknown versus credited, persists on reload, and does not
show a successful whole cancellation for a rejected child shipment.

## Migration and operational limits

New forward migration004 only; applied001/002/003 are immutable. Add source,
reservation, attempts/outcome/outbox evidence with bounded indexes and restrictive
history FKs. Preserve existing paid/financial rows, wallet balances and charges.
Populated rollback refuses evidence loss. Read-only historical audit identifies
cancelled orders with paid transactions, prior effects and uncertain simulator
history. No automatic historical wallet credits or PSP requests during migration.
Historical reconciliation, genuine PSP sandbox operation and hardware capacity
remain explicit OPEN gates, not hidden skips.

## Acceptance and builder discretion

Reproduce paid10m preservation/liability first. Test unpaid, replay, changed key,
concurrent cancellation/callback, expiry and Redis hold rejection after successful
verification, partial prior refund, uncertain historical effects, amount/currency
mismatch, multiple payment attempts, wallet rollback, child shipment rejection,
crash after receipt/claim commit, unknown provider result, idempotent reconciliation,
raw SQL source/cap/state guards and full canonical migration preservation.
Mounted customer UI must retain honest pending/unknown state and reload it.
Use existing Go/PostgreSQL/Redis/React, typed locks and checked money. Builder may
settle table/file decomposition, index names, bounded batch sizes and test helpers
without changing these outcomes. Keep implementation in focused files rather than
expanding the large order repository with the entire refund engine. One fresh
whole-stage review and TDD fix pass precedes final exact-source full gates.


Builder check2026-11-15: independent read-only review_fin003_design reported
"nothing important left". Owner destination choice was subsequently received
and applied above. Execution continues under the standing approved task workflow.
