# Financial balances (FIN-001)

This contract defines current seller/supplier availability and the guarded writes that authorize spending it. It complements the [CORE transaction contract](transaction-contract.md) and [FIN-012 monetary contract](money-policy.md). It does not certify production history, full historical statements, or capacity. FIN-014 reconciliation and capacity/release gates remain open.

## Signed availability

Amounts are integer toman within FIN-012's JSON-safe range, including negative balances. Checked signed arithmetic must preserve debt; displays, treasury aggregation and spending gates must not floor it to zero. Authorization reads `BalanceTx` through the caller's transaction while holding the same typed account guard as every competing writer. A positive debit exceeding signed availability is rejected.

Seller availability is:

```text
released delivered-order profit
+ released referral bonuses
+ released referral commissions on non-cancelled orders
+ signed seller-payable finance adjustments
- signed seller-profit return reversals
- payouts in pending/approved/paid/processing/completed/transfer_confirmed
- service purchases
- approved early-withdrawal fees
- pending early-withdrawal gross amounts
```

Released profit requires `profit_released` and order status `delivered` or `delivered_confirmed`. Adjustment credits add and debits subtract; only the canonical `store_payable:<id>` seller ledger legs with the finance-adjustment reason prefix participate. Return-event debits subtract and credits restore availability. Approved early withdrawal converts its gross reservation into its fee plus a net payout reservation; it must not count the gross again. Locked bonuses and unreleased profit do not authorize spending. Other summary fields such as seller `balance` are not substitutes for `availableAmount`.

Supplier availability is:

```text
earned wholesale snapshots
+ signed supplier-payable finance adjustments
- pending payout reservations
- payouts in approved/paid/processing/completed/transfer_confirmed
- signed supplier stock-return charges and supplier-borne customer-return shipping
- signed customer-return product-cost clawbacks on non-cancelled orders
- assessed warehouse storage fees
```

Earned is `order_items.wholesale_cost × quantity`, once per line on paid, delivered/delivered-confirmed orders. The supplier has no seller profit-release hold. The cancelled-order exclusion in product-cost clawbacks avoids reversing wholesale cost twice when earned already excludes that order. Supplier adjustment legs use `supplier_payable:<id>`, target type `supplier` and the finance-adjustment reason prefix. Panel, `BalanceTx`, settlement authorization and treasury share these terms; treasury is the signed sum of supplier availability. Supplier `approved` payouts belong to the paid term, while seller `approved` payouts remain reservations; preserve these existing status classifications.

## Immutable financial principals

An earned supplier line and its matching customer-return product-cost/shipping effects use persisted `order_item_origins.supplier_id`, not today's product supplier. Evidence must have:

- Matching order and item identities, positive origin quantities and full quantity coverage.
- Exactly one nonempty financial supplier across the line's origins.
- The accepted order-item unit wholesale-cost snapshot; no repricing.

Several physical origin rows for one supplier are valid when their quantities sum to the line quantity. They do not multiply the line's earned amount. A partial refund uses the same accepted unit cost times returned quantity. Conflicting supplier splits cannot establish a partial return's financial party and fail closed.

A warehouse ID, blank supplier, incomplete coverage, wrong-order origin or old capture allocation without trustworthy persisted supplier evidence cannot establish financial authority. Blank evidence remains invalid even if the current catalog supplier is also blank. No catalog fallback, guessed supplier, synthetic opening funds or historical allocation backfill is permitted. Known supplier A retains the line's earned term and refund even after the catalog changes to B. Catalog IDs may help identify accounts whose reads must be blocked, and parent rows may still be locked for FK compatibility; neither grants spending authority.

Other principal sources are explicit: seller/referrer IDs come from order/referral snapshots; customer-wallet refunds must match the immutable order customer; supplier stock-return parcel charges use the return-request supplier; storage charges use the storage-position supplier; payout/adjustment requests use the authenticated typed account ID. Refund discovery is revalidated after the complete financial/product guard prefix. Changed discovery rolls back rather than appending an earlier account lock late.

## Statements: posted flow and reservations

`GetStoreStatement` uses one read-only repeatable-read snapshot. Its period is UTC calendar month `[start, next month)`. Posted terms are released profit/bonuses/commissions, signed seller returns and adjustments, money-out payouts (`paid`, `processing`, `completed`, `transfer_confirmed`), services and approved early fees. The source dates are release dates, return-event creation, adjustment ledger date, payout processed date (creation fallback), service creation and early processed date. A nonzero posted term lacking its required date is reconciliation-required.

`totalCredit`, `totalDebit` and `netBalance` describe that month's posted flow. Pending/approved seller payouts and pending early gross are reservations, not posted debits. The statement exposes opening/closing reservations and signed availability separately:

```text
netBalance = totalCredit - totalDebit
closingAvailable - openingAvailable
  = netBalance - (closingReserved - openingReserved)
```

`currentAvailable` is the current canonical balance in the same snapshot; it may differ from a past month's closing amount. Cutoff projections use recorded event dates and the currently accepted state rows, including recorded reservation completion/rejection dates. They are not a complete temporal audit of every prior status or correction. FIN-009 retains full historical statement/audit/frontend work; this projection must not be represented as a reconstructed immutable history.

## Financial request identity and service receipts

These creation routes require a nonempty `Idempotency-Key` of at most 128 bytes:

- `POST /stores/:slug/services`
- `POST /stores/:slug/payouts`
- `POST /stores/:slug/early-withdrawals`
- `POST /supplier/settlement/request`
- `POST /finance/adjustments`

Missing/oversized keys return HTTP 400 before a financial write. CORS permits the header. Cached clients must upgrade to create requests. Equal amounts are never a deduplication identity. The key is scoped by principal type/ID and operation, binds an immutable payload fingerprint, and returns the original result on an unchanged retry; a changed payload conflicts. Internal callers without a key explicitly issue a new command, while existing state transitions and monthly cycles retain their separate business identities.

Live clients keep opaque pending keys scoped to immutable user/account identity and payload. They retain them through ambiguous transport, HTTP or malformed-result failures, and rearm only after validating a complete acknowledgment. Raw JWTs are not persisted as operation-key identities. Declared-JSON success with malformed JSON is an ambiguous transport error; valid JSON must still satisfy the mutation's identity/shape contract. Valid 204 behavior remains separate.

Service fingerprints bind owner/service intention. The atomic receipt stores original store ID, chosen plan, amount, duration and resulting expiry. Under the account guard and revalidated ownership, recovery reads that receipt before today's offer configuration. Later price, offer removal or plan/expiry changes cannot change the original acknowledgment or charge again. The acknowledgment is an immutable purchase result, not a mutable Store snapshot. A legacy receipt lacking accepted terms requires reconciliation.

## Guard and receipt order

Discover the complete plan, then follow CORE: order → payment → typed accounts (`stores`, `suppliers`, `users`) → all products → all SKUs → work records → physical resources, sorted within each class. Re-read ownership, state, immutable principal evidence, amounts and availability through the transaction before effects. Account-only paths may not acquire an order/payment later. No provider call belongs inside the lock interval.

`finance_command_requests` is append-only final metadata under the existing typed account guard. Receipt reads are ordinary MVCC reads after that guard; the table is not an independently locked work record. Its composite primary key is `(principal_type, principal_id, operation, client_key)` and it has no earlier-parent FKs. `recordTx` runs after all required class 1–7 guards and owning money/entitlement effects, as the last business statement before commit. No new guard follows it. It therefore needs no single-column guard allowlist/catalog entry. Adjustment audit and supplier return-event identities likewise supplement owning guards rather than replacing them.

## Reconciliation and fixture limits

`ErrFinancePrincipalUnresolved` blocks unsafe balance/treasury authorization and return receive/refund effects when immutable supplier authority is absent or ambiguous. Treasury must not turn unresolved liabilities into zero. Ambiguous unassigned provenance can conservatively block candidate account reads. Unsupported monetary magnitudes, missing posting dates and incomplete legacy service acknowledgments also require reconciliation. Unknown historical funds and duplicates remain untouched; FIN-014 must establish evidence before historical repair/release. Correctness here does not close cancellation/return/storage policy tasks, provider reconciliation, bounded-worker or capacity gates.

CAP financial cohorts must carry conserved immutable origins, accepted line snapshots, explicit statuses and posting dates, preferably produced through real release/return helpers. Cardinality-only cancelled/unpaid history cannot establish financial validity. Do not seed a ledger effect as well as call a helper that posts it. Assert signed availability and the posted/reserved equation, not only row counts. Synthetic disposable history cannot certify production reconciliation.

DB fixtures use `internal/testdb.Open` through the guarded runner. Legacy fixture DDL uses a uniquely quoted private schema and an explicit schema-only DSN `search_path` for every pooled/reconnected connection, without public fallback. The guard marker remains public-qualified; optional relation probes resolve the same namespace as the business SQL. Two pinned connections verify private resolution and public capture/return constraints survive setup. Normal migrated SKU tests run against the runner's default public schema.

## Focused verification

Run from the repository root. The runner provisions and removes a guarded disposable database; do not substitute a production DSN. The financial selector covers signed debt, exact refund/withdrawal authorization, competing writers, statement reconciliation, blank-origin receive, receipt recovery and fixture isolation:

```powershell
python scripts/test-isolated.py -- go test -p 1 ./internal/repository ./cmd/server -run '^(TestFinanceReview|TestAuditFinanceRefundCannotBeWithdrawn|TestFinanceBalance|TestFinanceSignedBalanceArithmetic|TestFinanceAdjustmentConcurrentReplay|TestFinanceEarlyWithdrawalReservationTransitions|TestFinanceCORSAllowsIdempotencyHeader|TestFinanceBusyRequestReturnsRetryableStatus)' -count=1 -v
python scripts/test-isolated.py -- go test ./cmd/server -run '^TestSellerFinanceCommandReplay$' -count=1 -v
python scripts/test-isolated.py -- go test ./internal/repository -run '^(TestGap6|TestGap15|TestPayoutStatusAvailableNeverJumpsBack|TestReturnShipping|TestReturnQuoteLegsMatchSettlementWithOriginFlagDisagreement|TestWarehouseCarrierFailedRepeatIsNoop|TestReviewSettlementAndTransferNotifyStoreOwner|TestTeam03Confirm|TestStorageFee)' -count=1 -v
python scripts/test-isolated.py -- go test -p 1 ./internal/repository ./cmd/server -run '^TestCarton|^TestSKUInventory|^TestInventoryOrigin|^TestAuditSupplyWarehouseOrigin|^TestGap13Warehouse|^TestCarrierFailurePreservesLegacyDispatchEvidence' -count=1 -v
npx tsx --test src/services/apiClient.finance.test.ts src/services/financeOperation.test.ts src/services/cartonDraft.test.ts
npx tsc --noEmit
```
