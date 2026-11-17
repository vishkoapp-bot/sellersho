# Coupon Reservations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or the parent's already authorized execution method. This is one coupled task with one root-owned final gate/release. Preserve the user's opt-out of repeated planning questions. Steps use checkbox syntax for tracking.

**Goal:** Attach coupon capacity atomically to accepted orders and preserve it through checkout retries, confirmed payment, cancellation and expiry.

**Architecture:** An order-owned reservation ledger and coupon row guard replace the ignored post-commit increment. Durable payment evidence drives consumption; eligible unpaid cancellation drives release; uncertain money holds capacity and late money follows existing compensation. Additive reconciliation and a database writer fence protect existing orders and mixed binaries.

**Tech Stack:** Go/database/sql, PostgreSQL, existing pricing engine, isolated Python test runner and existing server HTTP tests.

**Spec:** `docs/superpowers/specs/2026-11-17-coupon-reservations-design.md` (read both before implementation).

## Global Constraints

- Add `202611170001` only; never edit applied migrations.
- Preserve persisted prices, FIN-001/003/005/012 money facts, source evidence, refund liabilities and transaction ordering. No real provider calls.
- Exclude `docs/fa/` and `backend/internal/networkreward/` from reads, tests, compilation, commits, frozen validation archives and deployment.
- Validate only in a frozen source archive excluding protected paths, using guarded disposable PostgreSQL; never root `go test ./...` in the working checkout.
- Root owns final review, full gate, commit, push and `deploy.ps1 -BuildOnServer`; preserve server credentials, services and primary-site access.

## Review Focus

1. Coupon terms change while pricing waits: locked ID/revision comparison rejects stale pricing (B).
2. Aggregate seller discount is mistaken for coupon-only benefit: layered/zero/historical-null tests preserve attribution (A/B).
3. A confirmed receipt commits before stock admission fails: capacity and compensation remain truthful (C).
4. A legacy generic setter, cascade or old worker bypasses lifecycle: compatibility/deletion/fence tests close each path (C/D).
5. An old backend rolls back onto a migrated database: actual binary rehearsal proves retained fail-closed fence without disabling ordinary traffic (D).

---

## Task 1: Atomic coupon admission and complete lifecycle

Sections are implementation stages of the same deliverable. Do not ship admission without payment/expiry integration or migration fencing. Root owns deployment scripts; its release disk-space prerequisite removes only freshly generated release `node_modules` after successful frontend build and artifact verification. Builder review reported no important finding and 7 green prerequisite tests; that evidence does not replace the full release gate. This feature plan does not modify deployment scripts or relax gates.

### File map and interfaces

Create:

- `backend/migrations/202611170001_coupon_reservations.up.sql` / `.down.sql`: schema, revision/immutability checks, guarded backfill/issues, counters, fences and deferred consistency.
- `backend/internal/repository/coupon_reservations.go`: caller-owned coupon guard, reserve/transition and typed errors.
- `backend/internal/repository/coupon_reservations_integration_test.go`: shared guarded fixtures, admission/lifecycle/concurrency/fault tests.
- `backend/internal/repository/coupon_migration_integration_test.go`: historical reconciliation, downgrade refusal and fence tests.
- `backend/cmd/server/coupon_checkout_test.go`: HTTP error/recovery/amount contract.
- `scripts/test_coupon_old_binary.py`: reproducible disposable old/new binary rehearsal, no external providers or production target.

Modify:

- `backend/internal/pricing/discount.go` and its existing tests: preserve coupon-only contribution through layered discount calculation.
- `backend/internal/domain/models.go`: persisted nullable coupon amount and trusted non-JSON expected ID/revision fields.
- `backend/internal/repository/discount_repo.go`: revision reads, advisory validation, remove independent counter authority.
- `backend/internal/repository/order_repo.go`: shared insert, order scan, payment/cancel/compatibility paths.
- `backend/internal/repository/checkout_attempt.go`: reuse shared insertion after idempotency replay, never reserve before replay.
- `backend/internal/repository/finance_balance.go`, `transaction_contract.go`, `transaction_contract_test.go`: acquire coupon rank 350 before products for existing lifecycle; add creation-specific compatible parent-lock protocol and origin-parent ordering/catalog coverage without changing existing lifecycle lock modes.
- `backend/internal/repository/payment_verification.go`, `refund_principal.go`, `operations_repo.go`: receipt, late compensation and eligible expiry/cancel transitions.
- `backend/cmd/server/order_handlers.go`, `checkout_attempt_handlers.go`, `discount_apply.go`: coupon amount/snapshot propagation and both checkout error branches; remove ignored increment.
- `docs/architecture/transaction-contract.md`: coupon guard/order and complete writer adoption inventory.
- Existing private checkout/payment/refund/expiry fixture tests only where real new constraints require updated evidence; specifically `sku_inventory_integration_test.go` and `return_shipping_settlement_test.go` require empty `discount_code`, and `notifications_writers_test.go` requires nullable `coupon_discount_amount` for the common INSERT/reader. Empty-code short-circuit does not excuse missing order columns. Never disable production constraints or accept missing runtime coupon schema to rescue fixtures.

Interfaces produced in `coupon_reservations.go`:

```go
var ErrCouponExhausted, ErrCouponInvalid, ErrCouponChanged,
    ErrCouponEvidence, ErrCouponLifecycleRequired error

func markCouponWriterTx(ctx context.Context, tx *sql.Tx) error
func lockOrderCouponTx(ctx context.Context, tx *sql.Tx, set *TransactionLockSet, orderID string) error
func reserveOrderCouponTx(ctx context.Context, tx *sql.Tx, order domain.Order) error
func confirmOrderCouponTx(ctx context.Context, tx *sql.Tx, orderID string, verified bool) (bool, error)
func cancelOrderCouponTx(ctx context.Context, tx *sql.Tx, orderID, reason string) error
```

`lockOrderCouponTx` is called inside the existing-order canonical financial `FOR UPDATE` prefix before products; it no-ops for empty order code and fails closed for unexplained nonempty missing evidence. A matching immutable migration-origin identity issue permits canonical financial facts/refunds only, with no guessed coupon/count or new fulfillment. `reserveOrderCouponTx` consumes `Order.CouponID string`, `CouponRevision int64` (both `json:"-"`), `CouponDiscountAmount *int64`, existing store/code/seller amount and already-inserted private new order. Creation must first prehold the complete existing principal/FK-parent prefix using `FOR KEY SHARE` before that INSERT, with no upgrades; reservation then acquires coupon `FOR UPDATE` 350. This explicit creation-only private-row exception does not change existing lifecycle modes or principal/coupon/product ordering. `confirmOrderCouponTx` and `cancelOrderCouponTx` consume preheld order/coupon guards; they never acquire an earlier guard. `verified` means a matching durable structured receipt exists and is rechecked in the tx, never a request boolean. Confirmation returns `admissible=false, nil` for released, allowing its caller to commit receipt/compensation before rejecting fulfillment; no-coupon returns true. Cancellation reasons are `customer_cancel`, `payment_expiry`, `admission_rejected`, `late_payment`; they do not substitute for locked eligibility evidence.

`DiscountCode.Revision int64` reads the row version; `pricing.DiscountResult.CodeDiscountAmount int64` is the exact code layer before later sale changes. Preserve all existing public repository signatures; discounted direct insert callers must supply the trusted new fields. `IncrementDiscountCodeUse` returns `ErrCouponLifecycleRequired`, without SQL mutation. For locked coupon-backed orders only, generic `SetPaymentStatus` rejects financial assertions and `UpdateOrderFulfillment` rejects cancellation/payment-affecting bypasses; callers use canonical verification/cancellation. Preserve both methods' existing nondiscounted semantics and existing FIN-005 dispatch guards.

### A. Evidence, schema and reconciliation

- [x] Write RED `TestFIN007MigrationHistoricalReservations`: seed through prior canonical schema with paid, refunded, captured, pending, overdue, cancelled unpaid, failed, unknown inquiry, missing deadline, zero aggregate, malformed money and same-code/different-store histories. Apply the new migration; assert exact unchanged order prices and correct consumed/reserved/released/review-held classifications, nullable historical coupon amount, rebuilt counts and immutable issues. Assert oversized historical counts remain above max and new admission blocks.
- [x] Write RED `TestFIN007MigrationIdentityAndFence`: missing/ambiguous normalized coupon identity blocks its store/code, including recreation; bound coupon/store/order deletion cannot cascade evidence away; down refuses populated evidence; canonical runner replay keeps checksums/data unchanged. Test marked and unmarked forged amount/store/code/count/state writes.
- [x] Write RED `TestFIN007CouponLayerAmount`: campaign + code + widget + later sale preserve exact coupon-only contribution and original aggregate; zero-value/floor-capped/admin-locked codes consume no new use; no money overflow. Run targeted pricing and migration tests from frozen archive and retain assertion failures (not fixture/setup failures).
- [x] Implement additive migration and revision/immutability rules exactly as design; copy snapshots rather than repricing. Lock/drain all source tables before backfill, install narrow BEFORE fences and deferred consistency in same tx. Preserve counter discrepancies and oversubscription as issues. Implement nullable domain/pricing evidence and coupon revision reads. Assert applied migration digests unchanged.
- [x] Verify review backstops: runtime legacy-origin insertion denied, admission amount/revision nonnull, and supplier child-only new dispatch fenced. Preserve existing physical carrier progress/replay and FIN-005 failure holds for unresolved historical identities; include supplier shipment table in the bounded migration drain.
- [x] Run `python scripts/test-isolated.py -- go test ./internal/pricing ./internal/repository -run 'TestFIN007(Migration|CouponLayer)' -count=1 -v` **from frozen archive root**. Expected: all tests execute and pass, no integration skips or leaked database.

### B. Atomic admission, locks and retries

- [x] Write RED `TestFIN007LastUse100`: barrier-start 100 unique keys/orders against maxUses=1. Assert exactly 1 accepted discounted order/reservation/occupied count, 99 exhaustion errors and no orphan lines/shipments/attempt completions. Add unlimited 100 successes, independent store/coupon progress and a rollback freeing the last slot.
- [x] Write RED `TestFIN007CheckoutReplay`: concurrent same-key requests, committed-response loss, exhausted/deactivated/date-ended coupon on replay, changed fingerprint, abandoned attempt and direct repository wrappers. Assert same original order/amount/reservation with one increment; invalid direct coupon evidence rolls back before a discounted order is accepted.
- [x] Write RED `TestFIN007AdmissionRevalidation`: start inclusive/end exclusive, lock wait crossing expiry, deactivate/edit mode/value/max/window after advisory lookup, ID/store/code mismatch, coupon-only versus aggregate mismatch and zero actual benefit. Assert explicit invalid/changed/exhausted error and zero committed effects; no automatic repricing.
- [x] Write RED `TestFIN007CreationParentLockProtocol` using migrated real-schema FKs and two connections/barriers: two same-store creators coexist under principal `KEY SHARE`; independent coupons in that store progress; creator versus existing lifecycle `FOR UPDATE` follows canonical parent order without deadlock. Cover distinct customers, referrer stores, suppliers, origin warehouses, duplicate primary order ID and newly discovered earlier-parent rollback. Assert no creator upgrades principal/product/variant/origin-parent locks; the 100-way last-use test still yields one winner and 99 exhaustion errors rather than deadlock outcomes.
- [x] Add coupon rank 350 to shared lock contract/catalog checks; integrate discovery into `lockOrderFinanceTx` before products, preserving existing `FOR UPDATE` modes. In shared creation helper discover the complete real FK-parent set for order/items/origins/shipments, prehold stores 300 (including referrer), suppliers 301 and customer users 302 with `FOR KEY SHARE` in canonical table/ID order **before private order INSERT**. Never upgrade these during creation. Duplicate order ID aborts or uses existing checkout recovery in a fresh tx. Acquire coupon `FOR UPDATE`350, prehold products400/variants500/warehouses550/supplier_warehouses551 `FOR KEY SHARE` in table/ID order, then insert subordinate rows and seal attempt last. Include these origin parent relations in creation ordering/catalog tests; discovered earlier parents abort/restart. Add transaction-local marker only to adopted writers. Empty-code fixtures skip reservation tables but still supply required order columns.
- [x] Wire handler expected ID/revision and checked coupon-only accumulation; use positive actual benefit to attach code. Preserve early checkout replay. Remove postcommit increment and map typed failures in keyed and unkeyed HTTP branches. Keep campaign/sale pricing and existing margin-lock behavior.
- [x] Write and run `TestFIN007HTTPCheckoutErrorsAndRecovery`: race exhaustion is 409 with stable error code, invalid/changed terms never return an undiscounted success, recovery returns original snapshot; no duplicate postcommit side effects. Fault-trigger every order/line/origin/shipment/reservation/count/event/attempt-seal write and assert rollback; deferred missing-evidence/count failure must abort commit.
- [x] Run `python scripts/test-isolated.py -- go test ./internal/repository ./cmd/server -run 'TestFIN007|TestTransaction(LockOrder|Contract)' -count=1 -v` from frozen archive. Admission GREEN requires real constraints and lock-order tests; do not claim lifecycle GREEN before C.

### C. Confirmed payment, cancellation, expiry and compatibility

- [x] Write RED `TestFIN007PaymentLifecycle`: receipt consumes once before stock admission; duplicate payment verification/MarkPaymentPaid remains one use; paid cancellation/refund/return never returns capacity. Legacy `MarkPaymentPaid` without verified receipt is review-held and never promotes unknown cash. Unknown verification/failed status alone never releases; cancellation with unresolved inquiry is review-held.
- [x] Write RED `TestFIN007PaymentExpiryRaces`: use two real connections/barriers, not timing sleeps. Cover receipt-before-expiry, expiry-before-inquiry, inquiry-before-cancel, cancel-before-receipt, release then another order reserves followed by late verification, duplicate receipts and stock-admission failure. Assert one legal terminal transition, no released-to-consumed edge, no oversubscription, no stock resurrection, one durable receipt and existing correct FIN-003 compensation obligations.
- [x] Write RED `TestFIN007LifecycleCompatibility`: all wrappers in design audit, direct `SetPaymentStatus`, generic fulfillment cancellation, old independent increment, no-coupon orders, already-cancelled retries and every relevant payment writer. Pair each new coupon-backed setter rejection with the same nondiscounted operation retaining its existing behavior. Explicitly preserve normal metadata updates, paid refund projections, FIN-005 dispatch behavior and coupon deactivation on active orders.
- [x] Preserve receipts/refunds/returns for immutable migration-issue-backed unresolved historical coupon identities; deny new capture/dispatch, preserve existing captured/shipped replay, and reject every runtime issue insert even with the writer marker. Unexplained missing schema/evidence still fails.
- [x] Integrate `confirmOrderCouponTx` in durable receipt tx and legacy paid path; integrate `cancelOrderCouponTx` in canonical cancellation, expiry and late-payment cancellation. Guard payment creation/inquiry consistently with order-first finance/coupon prefix and marker. Any released-coupon receipt must still commit money fact plus compensation and deny inventory admission. Do not return an error that rolls back verified money merely because the coupon is released.
- [x] Close generic status bypasses only when the locked order identity is coupon-backed; update all adopted paths from writer inventory while preserving existing nondiscounted setter/fulfillment semantics and FIN-005 dispatch guards. Recheck marker fences do not require a coupon on historical nondiscounted fixtures or alter refund principal semantics. Do not extend expiry to failed orders or automatically resolve review holds.
- [x] Write `TestFIN007LifecycleFaults`: fixture-trigger failures on receipt insert, coupon state/event/count, order cancellation, refund obligations and deferred commit checks. Assert full tx rollback, replay recovery and no partial new stock/refund/capacity effect; separate already-committed receipt versus later failed admission transaction.
- [x] Run `python scripts/test-isolated.py -- go test ./internal/repository ./cmd/server ./internal/worker -run 'TestFIN007|TestCheckout|TestOrderPayment|Test.*(Refund|Expiry|Cancellation|Dispute)' -count=1 -v` from frozen archive. Expected: tests execute/pass with no skips; root still runs full suites because patterns are not the complete release gate.

### D. Actual mixed-binary fence rehearsal and root handoff

- [x] Implement isolated rehearsal script with explicit `--old-source`, `--new-source`, `--output` arguments. Validate both absolute source roots are frozen excluded archives, require guarded disposable test database and test-only provider adapters, bind server only to loopback, retain commit IDs/digests and bounded sanitized logs. Build actual prior server binary from frozen HEAD `08c6d5b` (resolve full commit in root gate), not a fake rewritten writer. Never run against production or invoke real payment providers.
- [x] Execute old binary against pre-migration schema; exercise discounted checkout, pending token/inquiry, cancellation and expiry. Retain an in-flight old write, attempt migration, prove bounded whole-migration refusal/drain, then retry after release. Against migrated schema exercise the old binary's checkout/payment/cancel/expiry routes/worker and prove protected writes fail closed, counts/orders remain consistent, while safe nondiscounted traffic continues. SQL-only trigger probes supplement but do not replace this run. Include a queued old statement after fence installation and actual new-binary successful lifecycle/replay.
- [x] Rehearse code rollback to old binary with additive data retained: affected coupon writes remain fenced and no capacity/price/money changes slip through. Report restricted availability explicitly. Keep fences intact; no automatic down migration or backup restore. Capture old binary startup incompatibilities separately from successful route/worker fence assertions; startup failure alone is not evidence that each writer is protected.
- [x] Rerun scoped production writer searches and update `docs/architecture/transaction-contract.md` adoption map. Self-review against each design invariant/acceptance row, list uncertainty rather than claiming unexecuted coverage. Produce ignored `releases/fin007-implementation-report.md` with files, fixture evidence, migration reconciliation categories, old-binary results and root-owned release prerequisites.
- [ ] Root reviews one coherent diff, runs full backend/finance/checkout/worker gates and frontend build from frozen excluded archive, verifies no skipped integrations and unchanged applied migrations, and records guarded DB cleanup. Root stages only approved feature docs/source (never protected paths or ignored reports), commits, pushes and runs full `deploy.ps1 -BuildOnServer`.
- [ ] Root verifies production migration history/checksums, coupon count/reservation/issue aggregates with nonsecret read-only queries, both application health and primary-site access. Root's preimplementation read-only FIN007 audit reported 0 coupons, 0 order coupon claims and 0 used_count; repeat at release because that snapshot can change. This emptiness is not migration/race acceptance evidence. All secret backups remain restricted on server.

## Executed acceptance and release handoff

Implementation checkboxes are satisfied by the maintained equivalents and root full top-level-cloned gate; the direct shared-DB Go recipes above are historical planned recipes, not the release command. Use `python scripts/test-suite-isolated.py --run '<pattern>' <packages>` from the frozen archive to avoid fixture collisions. Final root tree432e76625223b4ce65104d9505b5e38389da2908:891 Go entries/0 failures (only two intentional standalone subprocess helpers skip),309 frontend tests/0skips, TypeScript/Vite and vet,72 Linux race/0skips, cold Linux server/migrator builds,7 deploy prerequisite checks and actual old/new binary run9 pass. All owned resources cleaned. Review R1–R4 and final F1–F3 are independently addressed.

The two final root release boxes intentionally remain unchecked in the committed pre-deploy plan: commit/push/deploy and production verification necessarily occur after this source record. Verify the live committed release and local `releases/fin007-production-receipt.json` rather than rerunning or reimplementing FIN007. No down migration or historical repair is authorized by a checkbox. The owner now requests release and handoff; remaining work is documented in `docs/operations/agent-handoff-2026-11-17.md`.
