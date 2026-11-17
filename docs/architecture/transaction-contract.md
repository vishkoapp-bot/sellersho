# Cross-domain transaction contract (CORE-01)

Status: shared helper and executable contract fixtures implemented; production adoption remains with the INV/FIN/JOB owners below. This prerequisite does not certify existing domain flows, financial reconciliation, or the 100 RPS acceptance gates.

## Transaction boundary and identities

Discover all order, payment, financial principal, product, SKU, work-record and physical-location identities without row locks. Start a bounded caller-owned transaction, acquire the complete plan in the order below, then re-read authorization, ownership, state, quantities, amounts and immutable event payload **through that transaction**. If discovery changed, roll back and rediscover; never append a newly discovered earlier guard after a later guard. An account-only worker must not subsequently lock an order or payment. Storage assessment must discover its principal and product before positions.

All transaction helpers receive the caller's `context.Context` and `*sql.Tx`. No helper opens a nested transaction or uses `*sql.DB` for authorization. Do not move a provider, payment, DNS, carrier, email or SMS call inside a row-lock interval. Claim/record an intent in one short transaction, call the provider after commit with a stable provider event identity, then reconcile the result in another short transaction. Notification writes currently marked best effort are observations, not proof that the business effect committed.

A request key is `(principal_type, principal_id, operation, client_key)`. Bind it to a canonical fingerprint of immutable IDs, quantities, integer amounts, currency, destinations and operation version. Same key and same fingerprint returns the original result; a changed payload is a conflict. A separate database unique business-event identity prevents duplicate money/stock effects when clients use different request keys. Examples are `(order_item_id, capture)`, `(return_item_id, component, operation)`, `(parcel_id, charge_kind)`, `(position_id, fee_date)` and `(settlement_period, store_id)`. Random journal/event row IDs alone are not that uniqueness guarantee. The task owner must add constraints using a distinct additive migration; CORE-01 changes no schema or historical rows.

## Global row-lock order

Within each numbered class, lock **every** row in the listed table order, then bytewise ascending primary-key text. Never interleave product A / SKU A / product B. FIFO determines which positions are consumed after their rows have been locked in canonical ID order; FIFO timestamps are not the acquisition order. Do not rely on SQL join/planner order or locale-dependent sorting.

| Class | Table / typed identity order | Key |
| --- | --- | --- |
| 1 | `orders` | `id` |
| 2 | `payment_transactions` | `id` |
| 3 | `seller` = `stores`, then `supplier` = `suppliers`, then `wallet/customer` = `users` | principal `id` within type |
| 3.5 (350) | `seller_discount_codes` | `id` |
| 4 | all `products` | `id` |
| 5 | all `product_variants` | `id` |
| 5.5 (550/551, creation FK parents) | `warehouses`, then `supplier_warehouses` | `id` |
| 6 | `supplier_shipments`, `inbound_shipments`, `shipments`, `shipment_items`, `customer_returns`, `return_items`, `supplier_return_requests`, `supplier_return_parcels`, `putaway_tasks`, `order_inventory_captures`, `disputes`, `payout_requests`, `seller_early_withdrawals`, `monthly_settlement_cycles`, `monthly_settlement_items`, `pick_tasks`, `supplier_return_back_requests` | `id`, except capture `order_item_id`, cycle `period` |
| 7 | `inventory_balances`, `inventory_reservations`, `warehouse_bins`, `putaway_capacity_reservations`, `inventory_placements`, `warehouse_storage_positions` | `id`, except bins `bin_code` |

`inventory_placements` is a reserved relation name/order for INV-06's future exact placement records; no such production table or historical backfill is introduced here. The helper explicitly rejects this relation until that task introduces the actual schema and adds its key to the allowlist/catalog test. If that task chooses a different name, it must update this contract and the fixtures together. The storage/departure fixture uses the existing `putaway_capacity_reservations` occupancy reference, and does not claim exact placement lifecycle coverage. New work-record or physical-resource tables must receive an explicit unique order before use. Ancillary metadata/configuration writers in the inventory below may lock their own sorted entity rows only when they never subsequently enter a core class. Append-only journal/event/audit/notification records are inserted after their owning guards; their uniqueness is a final database backstop, not a replacement for guards. Foreign keys and stock projection triggers also acquire locks: prehold affected parent products/SKUs and typed financial principals before inserting/updating children. Never discover an earlier parent by triggering an update after balances are locked.

Existing principal rows are the common account guards. There is no global account mutex, advisory lock, or newly created guard table. A seller payout and a seller refund must lock the same store even when they use different journal tables. Supplier charges and supplier adjustments similarly lock the same supplier. Customer wallet credits/debits lock the same user. Platform/warehouse journal offsets do not create authority to spend a principal's balance; guard each affected seller/supplier/customer principal. Any future withdrawable account type needs an explicit typed guard here before introduction.

FIN-001 adds `finance_command_requests` as append-only request-receipt metadata, with an explicit final position **after every required class 1–7 guard and after the owning money/entitlement writes**. `financeCommand.recordTx` is the last business statement before commit; no new row guard may follow it. Its composite identity is `(principal_type, principal_id, operation, client_key)`, with an immutable payload fingerprint and original result. Every reader/writer of a receipt first holds that existing typed principal; receipt reads use ordinary MVCC `SELECT`, and receipts are never updated, deleted, or independently locked by the application. The table has no foreign keys that acquire an earlier guard. It therefore is not an account guard or a `LockTransactionRowsTx` allowlist relation and does not need the single-column guard catalog fixture. Its primary-key constraint is the final request-identity backstop, and the receipt commits or rolls back with the reservation/entitlement. Legacy repository callers without a client key create a new command per invocation; all live seller debit, supplier settlement, and finance-adjustment clients supply stable keys.

The creation endpoints `POST /stores/:slug/services`, `/stores/:slug/payouts`, `/stores/:slug/early-withdrawals`, `/supplier/settlement/request`, and `/finance/adjustments` require a nonempty `Idempotency-Key` of at most 128 bytes. Missing/oversized keys return HTTP 400 before any financial write; equal amounts never invent an identity. Stale clients must upgrade before creating requests. The current clients persist opaque pending identities scoped to immutable user/account identity and request payload, retain them after ambiguous transport/HTTP/malformed-result failures, and rearm only after a valid acknowledged result. CORS preflight permits the header. Internal non-network invocation is a new explicit command when no key is supplied; existing monthly cycles and state transitions retain their separate durable cycle/payout/return identities.

The existing `finance_adjustments` audit rows and `supplier_return_financial_events` remain append-only metadata under their owning account guards. New adjustments have a sparse unique `(target_kind, target_id, request_key)` identity; new parcel charges have a sparse unique `event_identity` after supplier → return request → parcel guards. Historical rows remain untouched and are recognized before posting a new parcel effect. Those event/audit inserts add no independent guard relation or late-lock exception. FIN-014 must reconcile historical duplicate or overdrawn effects before production release.

FIN-001 financial principal evidence is explicit: seller/referrer IDs come from the immutable order/referral-bonus snapshots; customer-wallet refunds require the return customer to match the immutable order customer; supplier return-parcel charges use their immutable return-request supplier; storage charges use the storage-position supplier; requested payouts/adjustments use their authenticated, typed account ID. Supplier sales, customer-return product-cost reversals and return-shipping debits use the persisted `order_item_origins.supplier_id`, never today's catalog owner. That evidence must cover the full order-item quantity with positive origin quantities, matching order/item identities, and one nonempty financial supplier. Several physical origin rows for the same supplier are permitted only when their quantities conserve the original line quantity. Earned remains the immutable `order_items.wholesale_cost × order_items.quantity` **once per line**, and a partial refund remains that same unit-cost snapshot × returned quantity; no amount is repeated per physical origin row and no repricing occurs. Conflicting supplier splits cannot identify a partial return's financial party and require reconciliation.

A warehouse ID alone, a blank supplier on a warehouse origin, missing/partial/wrong-order origin rows, or old capture allocation IDs without a trustworthy persisted financial supplier are insufficient evidence. No catalog-owner substitution or guessed historical allocation is made. The relevant supplier panel/transaction authorization returns `ErrFinancePrincipalUnresolved`, and the treasury refuses to render unresolved liabilities as zero. Per-supplier ambiguity checks include catalog/origin candidate accounts (catalog identity is a reporting/denial hint only, never spending authority); an entirely unassigned catalog party blocks all candidate reads conservatively. The aggregate treasury rejects any unresolved positive wholesale liability. Return receive/refund surfaces reconciliation conflicts without committing money or stock effects. Existing current product suppliers may still be preheld for parent/FK compatibility, but are not financial authorities. Historical reconciliation remains FIN-014 work; the fail-closed runtime gate is implemented now.

`LockTransactionRowsTx` in `backend/internal/repository/transaction_contract.go` allowlists relations and key columns, sorts and deduplicates a complete discovery plan, requires an acquisition deadline, fails on absent rows and rejects extensions that move backward. A `TransactionLockSet` belongs to one transaction and one goroutine. Reusing a preheld row is permitted. After any SQL acquisition failure the set is poisoned and the caller must roll back. The helper cannot observe arbitrary locks taken outside it: adopters must acquire their complete plan first, then call helpers under the preheld contracts below. Do not claim that merely creating a lock set repairs legacy SQL.

## Failure and retry policy

State/authorization/amount conflicts roll back and return the existing domain conflict response (normally HTTP 409); never retry a changed fingerprint as if it were the same command. PostgreSQL `40001` (serialization), `40P01` (deadlock), `55P03` (lock timeout) and a cancelled acquisition roll back the **whole** transaction. A request/worker may retry after fresh discovery under its original overall deadline, at most three attempts with bounded jitter; exhaustion is an explicit retryable unavailable response/failed job record, not a success or an indefinite wait. A cancelled request does not continue its business mutation. Preserve SQLSTATE through wrapped errors. `ErrTransactionLockOrder` is an implementation defect, not a retry candidate. Database event uniqueness makes a retry return the already committed effect; it must not apply the effect again.

Use a transaction-local server `lock_timeout` shorter than the overall context deadline where a bounded same-connection retry is required. lib/pq may discard a connection on context cancellation; do not require that physical connection to survive. The fixtures use 400 ms server lock timeout and a 2 s acquisition deadline, then a fresh transaction on the same second backend. Production budgets are set by the owning request/job and measured in INV-12/FIN-013/CAP-02; these fixture budgets are not production capacity targets.

## Helper / preheld-lock contracts

These are adoption requirements. Names marked planned do not yet exist; the table does not claim they are implemented by CORE-01.

| Helper | Preheld before invocation | Permitted work and required revalidation | Owner of incompatible current code |
| --- | --- | --- | --- |
| `LockAccountsTx` | All relevant orders, then payments, if the event has either. No later-class locks. | Sort/deduplicate typed principals; lock existing store/supplier/user rows through caller tx; revalidate principal/ownership. An account-only call may never later acquire order/payment. No nested tx or provider call. `LockOrderAccountsTx` also preholds all order products/SKUs and revalidates the principal discovery before later work records. | FIN-001; FIN-002 for bounded settlement workers |
| `BalanceTx` | Every principal whose authorization/balance is read, using the same typed guards all its writers use. | Read signed seller/supplier availability in the caller tx; no `*sql.DB` authorization, no new earlier locks, integer arithmetic. Include committed and in-transaction reservations/effects exactly once. Customer-wallet arithmetic remains outside this projection. | FIN-001, FIN-012 |
| `captureOrderInventoryTx` | Order, payment when applicable, all affected account guards when the event writes money, **all** product rows then **all** SKU rows, applicable work/capture records. | Re-read immutable order-item/origin plan; then sorted balance/location locks; conserve exact captured units and persist unique capture identity. Trigger projection parents already held. Current helper interleaves product/SKU/balance work per item. | INV-05, INV-12 |
| `restoreOrderInventoryTx`, `restoreCapturedReturnTx` | Order/payment, affected accounts, all products then SKUs, then capture/return records in class 6 order. | Re-read capture/restored quantities; lock exact allocation rows in class 7 order; restore only outstanding persisted units. Current order restoration locks captures before products, and return restoration can lock capture then product. | INV-05; FIN-003 for paid cancellation integration |
| `reserveInboundTransferTx` (planned) | Affected accounts if any, all products then SKUs, then inbound/shipment/task records. | Source ownership and available units revalidated after guards; exact source reservations are unique per inbound/item/source event; reserve and release within caller tx. It cannot lock a source first and then discover its product. | INV-02; INV-04 for QC adoption |
| `releasePlacementOccupancyTx` (planned) | Order/payment/accounts when applicable, products/SKUs, shipment/return/task records, then all balance/reservation/bin/placement/position guards required by the complete event. | Revalidate physical departure and residual placement units; release exact persisted volume/weight once under unique departure identity. No bin-first task/product discovery or recomputation from current dimensions. | INV-06; INV-08 for physical snapshots |
| `creditStoragePositionTx`, `decrementStoragePositionsFIFO`, `consumeShippedOrderPositionsTx` and storage accrual | Discover account/product/warehouse/position IDs first; acquire the full relevant prefix through sorted positions. | Re-read quantities, stocked age and assessed-through boundaries while locked. Settle quantity-time before decrement/closure; FIFO selection cannot invert ID acquisition. Assessment never locks a position before its account/product; no provider calls. | FIN-006, INV-10; INV-06 for departure |
| `ReceiveTrackedSKUShipmentItemTx`, SKU credit/move helpers, legacy inventory movement helpers | All affected products, then SKUs, then shipment/task/capture/return records; class 7 rows follow the common order. | Re-read shipment receipt/QC/provenance and balance coverage; child writes may not trigger acquisition of an unheld product/SKU. Exact source and single business event required. | INV-02, INV-04, INV-05, INV-09, INV-12 |

## Existing incompatibilities and required adoption

| Existing entry points / helpers | Observed contract gap | Owning tasks |
| --- | --- | --- |
| `MarkPaymentPaid`, `captureOrderInventoryTx`, legacy capture fallback | Order then payment is already the leading order; inventory loops can interleave products, SKUs and balances. Exact allocation/origin and preheld trigger parents still need adoption. | INV-05, INV-12; FIN-010 for provider/reconciliation |
| `CancelOrder`, `ShipOrder`, `SubmitShipping`, `MarkShipped*`, carrier status writers | Guard all fulfillment legs and order before terminal mutation; cancellation currently lacks a full dispatched-leg fence and paid refund obligation. Some shipment-only writers must discover and prehold order first. | INV-01, INV-05, FIN-003 |
| `CreateCustomerReturn`, `ReceiveCustomerReturn`, `RefundCustomerReturn`, `restockReceivedReturnTx`, `recordReturnReversalsTx`, `restoreOrderInventoryTx` | Return/capture-first locking or late product/account acquisition; money/stock revalidation must share the guarded order event. | INV-05, FIN-001, FIN-003, FIN-004, FIN-005 |
| Supplier `CreateReturnRequest*`, `ReviewReturn`, `CancelReturn`, `HandoverReturn`, `ReceiveReturn`, `InspectReturn`, SKU reservation helpers | Product/SKU guards exist on several paths, but full reservation/balance/parcel/position ordering and earlier money guards are not uniformly preheld. | INV-02, INV-04, INV-06, INV-10; FIN-004, FIN-006 |
| `CreateReturnParcel`, `ConfirmReturnParcelCharge` | Parcel/request locked before supplier guard; request-key uniqueness alone cannot guarantee one parcel charge. Discover supplier/product first; then principal/products/SKUs/request/parcel. | FIN-004, FIN-001 |
| `RecordProductMeasurement`, variant physical/pricing edits, `ApproveProposal`, `ConfirmPutaway` | Measurement/product/SKU parents and task/reservation/bin order need joint adoption; putaway currently locks task/reservation before product and moves inventory after bin. | INV-08, INV-09, INV-10; FIN-012 for price fingerprints |
| `CreateInbound`, `DispatchInbound`, `CancelInbound`, `ReceiveWarehouseShipment`, `CompleteInbound`, `RecordQCPass`, `CompleteQC`, `applyInboundInventoryTx` | Inbound/shipment-first discovery, implicit product locks from child triggers, exact source provenance/QC conservation and storage age must be ordered together. | INV-02, INV-04, INV-09, INV-10 |
| `CreateCartonShipment`, `CreateProductVariantsBulk`, stock setters, `SetTrackedSKUStockTx`, `syncProductStockTx` | Multi-item/SKU writes must prelock all products/SKUs before any child balance, preserve source location and bind event payload. Carton creation needs one transaction and durable identity. | INV-11, INV-03, INV-07, INV-12 |
| `AssignPutaway*`, `ConfirmPutaway`, bin capacity edits, position helpers | Task/reservation/bin/product inversion and position FIFO acquisition must use full sorted plans; exact placement release is not yet implemented. | INV-06, INV-08, INV-10; FIN-006 |
| `RequestOwnedStorePayout`, `RequestStorePayout`, `RequestEarlyWithdrawal`, `RequestSettlement`, `ReviewSettlement`, `ConfirmManualTransfer*`, `PurchaseSellerService`, `AddLedgerEntry`, finance adjustments | Some request paths lock principal rows, others authorize from separate projections or lock payout first. All balance-affecting writers need the same typed guard and canonical tx projection. | FIN-001, FIN-008, FIN-009, FIN-012 |
| `RunMonthlySettlement` | Read/claim cycle and balances without a complete shared per-store serialization/reservation contract. Process principal-scoped bounded transactions; never hold account then lock order/payment. | FIN-002, FIN-001, JOB-01 |
| `RunStorageFeeAssessment`, `advanceStorageAssessed`, storage departure helpers | Assessment reads unlocked position snapshots and updates later; debit/quantity-time/close must share account/product/position guards and revalidate quantity. | FIN-006, INV-10, INV-06, JOB-01 |
| `CreateDispute`, `ResolveDispute`, `ApplyDisputeRefundEffect`, `ReleaseClosedReturnWindows`, referral creation/release | Dispute-first verdict and independently committed order effects can diverge; all affected order/payment/account rows must be discovered before verdict/release. | FIN-005, FIN-003, FIN-011, FIN-001 |
| `IncrementDiscountCodeUse`, order creation, campaign/pricing writers | Coupon use must be attached to the guarded order event; monetary snapshots/fingerprints must not be re-read unlocked during retry. | FIN-007, FIN-012 |
| `ExpireUnpaidOrders`, worker delivery auto-confirm/release/fallback, QC/SLA/storage/settlement job entry points | Bound batches and whole-event retry, remove flag-only financial fallback, retain per-event guards and observable failed/exhausted retries; no account-to-order worker inversion. | JOB-01, INV-01, FIN-002, FIN-005, FIN-006, FIN-011 |
| Tapin attempt/registration writers, domain verification, shipment carrier handlers | Short claim transaction, provider call after commit, guarded response recording with stable provider identity. No database-lock interval spanning network I/O. | FIN-010 for payments; INV-01 for dispatch; JOB-01 for retries |

## Complete writer inventory and profile key

The inventory below includes repository methods with SQL mutation/DDL, helpers they delegate to, and wrappers that reach them, across **41 repository source files / 292 symbols** at this checkpoint. It deliberately includes ancillary catalogue, identity and notification writers so a caller cannot enter a later core class unnoticed. Constructors and read-only helpers are excluded. `delegates` means effects are performed by another listed helper; `dynamic/guard` means SQL is assembled or the helper obtains a row guard without a literal DML target. Source links identify the entry point; the tables column lists direct targets, not transitive effects. A profile supplies the invariant, business-event identity, acquisition, retry and external-side-effect contract for **each** mapped entry. The specific incompatibility table above overrides the general profile and assigns domain ownership. This is an adoption map, not a claim of existing compliance.

| Profile | Invariant and business-event identity | Required acquisition and retry | External side effect / owner |
| --- | --- | --- | --- |
| O | One valid order/fulfillment transition; no cancellation after any leg dispatch. Identity `(order, transition, leg/event)` and persisted capture origin. | Order → payment if changed → affected typed accounts → all products → SKUs → work → physical rows. State conflict 409; whole-tx bounded database retry. | Notify only after commit; carrier call outside locks. INV-01/05, FIN-003/005. |
| P | Payment state and inventory capture agree; one capture per order item and one provider payment result. Identity provider transaction/token + order. | O prefix including payment, then capture guards. Amount/currency/order binding revalidated. Same bounded retry; changed payment is conflict. | Provider verify/refund outside locks, durable reconciliation. FIN-010, INV-05. |
| R | Return quantity/QC cannot restore/refund twice; shipping components conserved. Identity `(return item, component, operation)` / `(parcel, charge kind)`. | Discover order/payment/principals/products before return/parcel; complete core hierarchy, then exact balances/positions. Same retry; invalid verdict/state conflicts. | Postcommit notices; carrier/payment outside tx. INV-05/06, FIN-003/004/005/006. |
| I | No negative/duplicate stock; exact location, SKU, volume, weight and age conserved. Identity `(reference type, reference, item/source, operation)`. | Order/payment/accounts if event has them; all products → all SKUs → work → class 7 table/ID order. Bounded whole-tx retry; insufficient units/capacity is conflict. | Postcommit notification/cache publication. INV-02 through INV-12 according to incompatibility table. |
| S | Quantity-time fees settled before departure, each position/day assessed once. Identity `(position, fee date)` and departure event. | Discover supplier/product before positions; typed account → product → SKU/work if needed → sorted class 7 rows. Re-read quantities/checkpoints. Same bounded retry. | Postcommit fee notification only. FIN-006, INV-10, JOB-01. |
| A | Canonical available balance cannot be overdrawn/reserved twice; journal components conserve integer money. Identity `(principal, operation, business event)`; settlement `(period, store)`. | Order/payment first if event-bound; otherwise typed accounts only, then work records; never account → order. Same retry; amount/fingerprint conflicts. | Bank/provider transfer outside locks; postcommit notices. FIN-001/002/008/009/011/012. |
| C | Product/SKU/publication/pricing version, ownership and approved immutable snapshot agree. Identity `(product/SKU/proposal, version, operation)`. | All products → SKUs → review/task rows, before balance child writes. Metadata-only changes lock sorted owned entities and never later core rows. Same bounded retry; stale version conflict. | Publish/invalidate/notify after commit. INV-03/08/11, FIN-012; ancillary catalog owner for non-core metadata. |
| K | Coupon/campaign usage is attributable once to its owning order/event; budget/reservation arithmetic conserved. Identity `(order, coupon)` / ad event. | Order/payment/accounts first if money-bound, then coupon350, products/SKUs/work; standalone config stays metadata-only. Same bounded retry; stale/duplicate payload conflict. | Postcommit campaign analytics/cache. FIN-007/012; FIN-001 when debiting money. |
| N | Notification/audit is attributable to its committed business event, cannot authorize stock/money. Identity `(business event, recipient, notification kind)` when durable. | Insert after owning guards or in a bounded postcommit transaction; never lock notification then business rows. Best-effort failure never changes committed result. | Delivery outside business tx. JOB-01 for durable retry; domain event owner. |
| M | Owned identity/configuration/entity mutation is atomic; no accidental money/inventory authority. Identity `(principal/entity, operation, version)`; DB PK/unique constraints apply. | Sorted owned metadata rows only; if it affects balances/stock it must use A/C/I instead before any write. Same bounded retry; authorization/version conflict. | DNS lookup/provider call outside locks, postcommit audit/invalidation. Ancillary owner; JOB-01 for worker effects. |
| J | Each worker business effect retains its domain invariants; runs cannot overlap into duplicates/unbounded backlog. Identity domain business event plus separate job-attempt ID. | Delegate O/P/R/I/S/A plan in bounded batches/transactions; whole-event retry and visible exhaustion. Job-log failure does not mask domain failure. | Network/cache/notifications after commit. JOB-01 jointly with domain owner. |

The `N` profile includes helpers that only choose recipients and call the actual notification writer. The `C` and `M` profile rows explicitly prohibit turning an otherwise standalone metadata update into a late core lock. New mutation entry points must be added to this inventory and choose a profile or supply an equally explicit contract.

### ad_rate_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`AllowAdChallenge`](../../backend/internal/repository/ad_rate_repo.go#L8) | `ad_request_limits` | M |

### admin_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreatePendingApproval`](../../backend/internal/repository/admin_repo.go#L81) | `pending_approvals` | M |
| [`ReviewPendingApproval`](../../backend/internal/repository/admin_repo.go#L109) | `pending_approvals`, `warranty_properties`, `warranty_property_requests` | M |
| [`InsertAuditLog`](../../backend/internal/repository/admin_repo.go#L199) | `audit_logs` | N |
| [`SetUserActiveStatus`](../../backend/internal/repository/admin_repo.go#L275) | `users` | M |
| [`ReviewStore`](../../backend/internal/repository/admin_repo.go#L304) | `stores` | M |
| [`LockStoreMargin`](../../backend/internal/repository/admin_repo.go#L313) | `stores` | M |

### capability_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`Grant`](../../backend/internal/repository/capability_repo.go#L86) | `user_capabilities` | M |
| [`GrantSuperAdminAfterOTP`](../../backend/internal/repository/capability_repo.go#L112) | `user_capabilities` | M |
| [`Revoke`](../../backend/internal/repository/capability_repo.go#L125) | `user_capabilities` | M |
| [`BackfillOwnershipCapabilities`](../../backend/internal/repository/capability_repo.go#L136) | `user_capabilities` | M |

### catalog_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`EnsureSearchLogTable`](../../backend/internal/repository/catalog_repo.go#L780) | dynamic/guard | N |
| [`LogSearchEvent`](../../backend/internal/repository/catalog_repo.go#L799) | `search_queries` | N |

### discount_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateDiscountCode`](../../backend/internal/repository/discount_repo.go#L60) | `seller_discount_codes` | K |
| [`SetDiscountCodeActive`](../../backend/internal/repository/discount_repo.go#L101) | `seller_discount_codes` | K |
| [`IncrementDiscountCodeUse`](../../backend/internal/repository/discount_repo.go#L132) | `seller_discount_codes` | K |

### domain_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`RequestDomain`](../../backend/internal/repository/domain_repo.go#L36) | `pending_approvals`, `store_domains` | M |
| [`VerifyOwnedDomain`](../../backend/internal/repository/domain_repo.go#L72) | `store_domains` | M |
| [`DisableOwnedDomain`](../../backend/internal/repository/domain_repo.go#L103) | `store_domains` | M |

### finance_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`AddLedgerEntry`](../../backend/internal/repository/finance_repo.go#L66) | `ledger_entries` | A |
| [`CreateDispute`](../../backend/internal/repository/finance_repo.go#L117) | `disputes`, `orders` | R |
| [`ResolveDispute`](../../backend/internal/repository/finance_repo.go#L211) | `disputes`, `ledger_entries` | R |
| [`RequestOwnedStorePayout`](../../backend/internal/repository/finance_repo.go#L623) | `payout_requests` | A |
| [`RequestStorePayout`](../../backend/internal/repository/finance_repo.go#L660) | `payout_requests` | A |
| [`ReviewSettlement`](../../backend/internal/repository/finance_repo.go#L905) | `payout_requests` | A |
| [`ConfirmManualTransfer`](../../backend/internal/repository/finance_repo.go#L982) | delegates | A |
| [`ConfirmManualTransferWithDate`](../../backend/internal/repository/finance_repo.go#L990) | `ledger_entries`, `payout_requests` | A |

### granular_review.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SaveFieldReview`](../../backend/internal/repository/granular_review.go#L251) | `pending_approvals`, `proposal_field_reviews`, `proposal_review_history` | C |
| [`ResubmitGranularReview`](../../backend/internal/repository/granular_review.go#L315) | `pending_approvals`, `proposal_field_reviews`, `proposal_review_history` | C |

### historical_reconciliation_report.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`ReviewHistoricalInventory`](../../backend/internal/repository/historical_reconciliation_report.go#L241) | `audit_logs`, `historical_reconciliation_reviews` | M |

### inventory_unify.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`syncProductStockTx`](../../backend/internal/repository/inventory_unify.go#L92) | `products` | I |
| [`NotifyUser`](../../backend/internal/repository/inventory_unify.go#L156) | `notification_outbox` | N |

### notifications.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`notifyBestEffort`](../../backend/internal/repository/notifications.go#L66) | delegates | N |
| [`notifyManyBestEffort`](../../backend/internal/repository/notifications.go#L92) | delegates | N |
| [`NotifyBestEffort`](../../backend/internal/repository/notifications.go#L107) | delegates | N |
| [`notifyOrderPlaced`](../../backend/internal/repository/notifications.go#L335) | delegates | N |
| [`notifyPaymentConfirmed`](../../backend/internal/repository/notifications.go#L351) | delegates | N |
| [`notifyPaymentFailed`](../../backend/internal/repository/notifications.go#L368) | delegates | N |
| [`notifyOrderCancelled`](../../backend/internal/repository/notifications.go#L389) | delegates | N |
| [`notifyExpiredOrder`](../../backend/internal/repository/notifications.go#L403) | delegates | N |
| [`NotifyDeliveryConfirmed`](../../backend/internal/repository/notifications.go#L424) | delegates | N |
| [`notifyProfitReleased`](../../backend/internal/repository/notifications.go#L448) | delegates | N |
| [`notifyCarrierFailed`](../../backend/internal/repository/notifications.go#L473) | delegates | N |
| [`notifySupplierAccepted`](../../backend/internal/repository/notifications.go#L490) | delegates | N |
| [`notifyLowStock`](../../backend/internal/repository/notifications.go#L520) | delegates | N |
| [`notifyDisputeFiled`](../../backend/internal/repository/notifications.go#L590) | delegates | N |
| [`notifyDisputeResolved`](../../backend/internal/repository/notifications.go#L607) | delegates | N |
| [`notifyRefundIssued`](../../backend/internal/repository/notifications.go#L653) | delegates | N |
| [`NotifyFinanceAdjustment`](../../backend/internal/repository/notifications.go#L678) | delegates | N |
| [`notifyPayoutReviewed`](../../backend/internal/repository/notifications.go#L708) | delegates | N |
| [`notifyPayoutTransferConfirmed`](../../backend/internal/repository/notifications.go#L737) | delegates | N |
| [`notifyPayoutRequested`](../../backend/internal/repository/notifications.go#L759) | delegates | N |
| [`notifySettlementSkipped`](../../backend/internal/repository/notifications.go#L774) | delegates | N |
| [`notifySettlementCreated`](../../backend/internal/repository/notifications.go#L789) | delegates | N |
| [`notifyProposalApproved`](../../backend/internal/repository/notifications.go#L804) | delegates | N |
| [`notifyApprovalRejected`](../../backend/internal/repository/notifications.go#L823) | delegates | N |
| [`notifySupplierRegistered`](../../backend/internal/repository/notifications.go#L840) | delegates | N |
| [`notifyLegacySupplierDecision`](../../backend/internal/repository/notifications.go#L855) | delegates | N |
| [`NotifyTicketReply`](../../backend/internal/repository/notifications.go#L883) | delegates | N |
| [`notifyReturnRequested`](../../backend/internal/repository/notifications.go#L923) | delegates | N |
| [`notifyReturnDecision`](../../backend/internal/repository/notifications.go#L938) | delegates | N |
| [`notifyReturnShipped`](../../backend/internal/repository/notifications.go#L962) | delegates | N |
| [`notifyReturnQCCharged`](../../backend/internal/repository/notifications.go#L991) | delegates | N |
| [`notifyReturnCODRefused`](../../backend/internal/repository/notifications.go#L1024) | delegates | N |
| [`notifyInbound`](../../backend/internal/repository/notifications.go#L1038) | delegates | N |
| [`notifySupplierReturnReview`](../../backend/internal/repository/notifications.go#L1059) | delegates | N |
| [`notifySupplierParcelCharge`](../../backend/internal/repository/notifications.go#L1080) | delegates | N |
| [`notifySupplierReturnTransit`](../../backend/internal/repository/notifications.go#L1095) | delegates | N |
| [`notifyStorageFeeCharged`](../../backend/internal/repository/notifications.go#L1111) | delegates | N |

### operations_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`ExpireUnpaidOrders`](../../backend/internal/repository/operations_repo.go#L26) | `orders`, `payment_transactions`, `supplier_shipments` | J |
| [`EscalateQCDeadlines`](../../backend/internal/repository/operations_repo.go#L93) | `qc_discrepancies` | J |
| [`RefreshSupplierSLA`](../../backend/internal/repository/operations_repo.go#L105) | `supplier_shipping_methods`, `supplier_sla_metrics` | J |
| [`RecordOperationsJob`](../../backend/internal/repository/operations_repo.go#L142) | `operations_job_runs` | J |

### order_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateOrder`](../../backend/internal/repository/order_repo.go#L77) | delegates | O |
| [`CreateOrderWithShipments`](../../backend/internal/repository/order_repo.go#L83) | `order_item_origins`, `order_items`, `orders`, `supplier_shipments` | O |
| [`ConfirmOrderDelivery`](../../backend/internal/repository/order_repo.go#L259) | `orders`, `referral_bonuses` | O |
| [`ConfirmOrderDeliverySystem`](../../backend/internal/repository/order_repo.go#L324) | `orders`, `referral_bonuses` | O |
| [`UpdateOrderStatusFromCarrier`](../../backend/internal/repository/order_repo.go#L385) | `orders` | O |
| [`UpdateWarehouseOrderStatusFromCarrier`](../../backend/internal/repository/order_repo.go#L438) | `orders` | O |
| [`SetPaymentStatus`](../../backend/internal/repository/order_repo.go#L683) | `orders` | P |
| [`UpdateOrderFulfillment`](../../backend/internal/repository/order_repo.go#L689) | `orders` | O |
| [`CreatePaymentTransaction`](../../backend/internal/repository/order_repo.go#L715) | `payment_transactions` | P |
| [`RecordPaymentFailure`](../../backend/internal/repository/order_repo.go#L765) | `orders`, `payment_transactions` | P |
| [`MarkPaymentPaid`](../../backend/internal/repository/order_repo.go#L794) | `orders`, `payment_transactions`, `product_variants`, `products` | P |
| [`ReleaseClosedReturnWindows`](../../backend/internal/repository/order_repo.go#L992) | `ledger_entries`, `orders`, `referral_bonuses` | R |
| [`CancelOrder`](../../backend/internal/repository/order_repo.go#L1094) | `orders`, `product_variants`, `products` | O |
| [`ApplyDisputeRefundEffect`](../../backend/internal/repository/order_repo.go#L1211) | `orders`, `payment_transactions` | R |
| [`CreateCustomerReturn`](../../backend/internal/repository/order_repo.go#L1754) | `customer_returns`, `return_items` | R |
| [`transitionCustomerReturn`](../../backend/internal/repository/order_repo.go#L1962) | `customer_returns` | R |
| [`ApproveCustomerReturn`](../../backend/internal/repository/order_repo.go#L1983) | delegates | R |
| [`RejectCustomerReturn`](../../backend/internal/repository/order_repo.go#L1999) | delegates | R |
| [`CancelCustomerReturn`](../../backend/internal/repository/order_repo.go#L2016) | `customer_returns` | R |
| [`ShipCustomerReturn`](../../backend/internal/repository/order_repo.go#L2032) | `customer_returns` | R |
| [`ReceiveCustomerReturn`](../../backend/internal/repository/order_repo.go#L2075) | `customer_returns`, `return_items` | R |
| [`restockReceivedReturnTx`](../../backend/internal/repository/order_repo.go#L2300) | `product_variants` | R |
| [`recordReturnReversalsTx`](../../backend/internal/repository/order_repo.go#L2417) | delegates | R |
| [`insertReturnReversalTx`](../../backend/internal/repository/order_repo.go#L2497) | `customer_return_financial_events`, `ledger_entries` | R |
| [`RefundCustomerReturn`](../../backend/internal/repository/order_repo.go#L2536) | `customer_return_shipping_charges`, `customer_returns`, `customer_wallets` | R |

### pricing_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SetProductMarginCeiling`](../../backend/internal/repository/pricing_repo.go#L184) | `product_margin_ceilings` | C |
| [`DeleteProductMarginCeiling`](../../backend/internal/repository/pricing_repo.go#L198) | `product_margin_ceilings` | C |
| [`SetProductPlatformPct`](../../backend/internal/repository/pricing_repo.go#L244) | `platform_pct_products` | C |
| [`SetCategoryPlatformPct`](../../backend/internal/repository/pricing_repo.go#L254) | `platform_pct_categories` | C |
| [`DeleteProductPlatformPct`](../../backend/internal/repository/pricing_repo.go#L264) | `platform_pct_products` | C |
| [`DeleteCategoryPlatformPct`](../../backend/internal/repository/pricing_repo.go#L270) | `platform_pct_categories` | C |
| [`UpdatePlatformPricingSettings`](../../backend/internal/repository/pricing_repo.go#L279) | `platform_settings` | C |
| [`SetStoreMarginLock`](../../backend/internal/repository/pricing_repo.go#L385) | `store_margin_locks` | C |
| [`snapshotLockTargetTx`](../../backend/internal/repository/pricing_repo.go#L420) | `store_margin_lock_snapshots` | C |
| [`DeleteStoreMarginLock`](../../backend/internal/repository/pricing_repo.go#L461) | `store_margin_lock_snapshots`, `store_margin_locks`, `store_variant_overrides` | C |
| [`SetStoreVariantOverride`](../../backend/internal/repository/pricing_repo.go#L592) | `store_variant_overrides` | C |
| [`DeleteStoreVariantOverride`](../../backend/internal/repository/pricing_repo.go#L635) | `store_variant_overrides` | C |
| [`insertPriceIncreaseTx`](../../backend/internal/repository/pricing_repo.go#L778) | `supplier_price_increases` | C |
| [`CreatePriceIncreaseRow`](../../backend/internal/repository/pricing_repo.go#L803) | delegates | C |
| [`ApplyGovernedPriceIncrease`](../../backend/internal/repository/pricing_repo.go#L874) | `products` | C |
| [`ApplyGovernedVariantPriceIncrease`](../../backend/internal/repository/pricing_repo.go#L1020) | delegates | C |
| [`ApplyGovernedVariantClear`](../../backend/internal/repository/pricing_repo.go#L1036) | delegates | C |
| [`applyGovernedVariantIncrease`](../../backend/internal/repository/pricing_repo.go#L1040) | `product_variants` | C |
| [`insertPricingAuditTx`](../../backend/internal/repository/pricing_repo.go#L1218) | `audit_logs` | N |
| [`ReviewPriceIncrease`](../../backend/internal/repository/pricing_repo.go#L1242) | `product_variants`, `products`, `supplier_price_increases` | C |
| [`UpsertProductPriceException`](../../backend/internal/repository/pricing_repo.go#L1407) | `product_price_exceptions` | C |
| [`DeleteProductPriceException`](../../backend/internal/repository/pricing_repo.go#L1441) | `product_price_exceptions` | C |

### product_publication_v2.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`insertProductV2VariantsTx`](../../backend/internal/repository/product_publication_v2.go#L30) | `product_variant_images`, `product_variants` | C |

### product_registry_v2.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`publishProductModelV2Tx`](../../backend/internal/repository/product_registry_v2.go#L403) | `category_local_product_attribute_values`, `category_local_product_attributes`, `category_product_attribute_option_extensions`, `local_product_attribute_values`, `local_product_attributes`, `product_attribute_option_extensions`, `product_attribute_values`, `product_canonical_attribute_values`, `products` | C |
| [`SyncProductDraftSKUsTx`](../../backend/internal/repository/product_registry_v2.go#L541) | `product_draft_sku_reservations` | C |

### product_variant_mutation_v2.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateProductVariantV2`](../../backend/internal/repository/product_variant_mutation_v2.go#L10) | delegates | C |
| [`createProductVariantsV2`](../../backend/internal/repository/product_variant_mutation_v2.go#L26) | delegates | C |
| [`syncProductCanonicalFacetsV2Tx`](../../backend/internal/repository/product_variant_mutation_v2.go#L115) | `product_canonical_attribute_values` | C |

### return_shipping_settlement.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`insertSupplierReturnShippingCharge`](../../backend/internal/repository/return_shipping_settlement.go#L447) | `customer_return_shipping_charges`, `ledger_entries` | R |
| [`insertCustomerReturnShippingCharge`](../../backend/internal/repository/return_shipping_settlement.go#L475) | `customer_return_shipping_charges` | R |

### returns_operations_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateReturnParcel`](../../backend/internal/repository/returns_operations_repo.go#L115) | `supplier_return_parcel_items`, `supplier_return_parcels`, `supplier_return_requests` | R |
| [`ConfirmReturnParcelCharge`](../../backend/internal/repository/returns_operations_repo.go#L193) | `ledger_entries`, `supplier_return_financial_events`, `supplier_return_parcels` | R |
| [`RecordProductMeasurement`](../../backend/internal/repository/returns_operations_repo.go#L245) | `product_measurement_events`, `products` | R |

### seller_campaign_edit_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`UpdateSellerCampaign`](../../backend/internal/repository/seller_campaign_edit_repo.go#L11) | `seller_campaigns` | K |

### seller_campaign_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateSellerCampaign`](../../backend/internal/repository/seller_campaign_repo.go#L98) | `seller_campaigns` | K |
| [`SaveOwnedWidgetConfig`](../../backend/internal/repository/seller_campaign_repo.go#L146) | `seller_campaigns`, `stores` | K |
| [`SetSellerCampaignStatus`](../../backend/internal/repository/seller_campaign_repo.go#L182) | `seller_campaigns` | K |
| [`RecordSellerCampaignEvent`](../../backend/internal/repository/seller_campaign_repo.go#L222) | `ad_fraud_keys`, `seller_campaign_events` | K |
| [`RejectAdEvent`](../../backend/internal/repository/seller_campaign_repo.go#L287) | `ad_event_rejections` | K |

### seller_panel_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SaveStoreProducts`](../../backend/internal/repository/seller_panel_repo.go#L113) | `store_products`, `store_variant_defaults`, `store_variant_visibility` | C |
| [`SaveStoreSettings`](../../backend/internal/repository/seller_panel_repo.go#L208) | `stores` | M |
| [`SaveOwnedPricing`](../../backend/internal/repository/seller_panel_repo.go#L300) | `store_niche_overrides`, `store_product_overrides`, `stores` | M |
| [`SetStoreVariantVisibility`](../../backend/internal/repository/seller_panel_repo.go#L419) | `store_variant_visibility` | C |
| [`SetStoreVariantDefault`](../../backend/internal/repository/seller_panel_repo.go#L463) | `store_variant_defaults` | C |
| [`ClearStoreVariantDefault`](../../backend/internal/repository/seller_panel_repo.go#L471) | `store_variant_defaults` | C |
| [`CreateSellerTicket`](../../backend/internal/repository/seller_panel_repo.go#L524) | `support_ticket_messages`, `support_tickets` | M |
| [`RequestStoreChange`](../../backend/internal/repository/seller_panel_repo.go#L543) | `pending_approvals` | M |
| [`CreateStoreDiscountCampaign`](../../backend/internal/repository/seller_panel_repo.go#L654) | `seller_discount_campaigns` | M |
| [`UpdateStoreDiscountCampaign`](../../backend/internal/repository/seller_panel_repo.go#L681) | `seller_discount_campaigns` | M |
| [`DeleteStoreDiscountCampaign`](../../backend/internal/repository/seller_panel_repo.go#L732) | `seller_discount_campaigns` | M |

### seller_services_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`PurchaseSellerService`](../../backend/internal/repository/seller_services_repo.go#L22) | `seller_service_purchases`, `stores` | A |

### settlement_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`RequestEarlyWithdrawal`](../../backend/internal/repository/settlement_repo.go#L206) | `seller_early_withdrawals` | A |
| [`RunMonthlySettlement`](../../backend/internal/repository/settlement_repo.go#L373) | `monthly_settlement_cycles`, `monthly_settlement_items`, `payout_requests` | A |

### sku_inventory.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SeedTrackedSKUInventoryTx`](../../backend/internal/repository/sku_inventory.go#L26) | `inventory_balances` | I |
| [`creditSKUInventoryTx`](../../backend/internal/repository/sku_inventory.go#L61) | `inventory_balances`, `inventory_movements` | I |
| [`ReceiveTrackedSKUShipmentItemTx`](../../backend/internal/repository/sku_inventory.go#L87) | `putaway_tasks`, `shipment_items` | I |
| [`putawayTrackedSKUInventoryTx`](../../backend/internal/repository/sku_inventory.go#L159) | delegates | I |
| [`moveSKUInventoryTx`](../../backend/internal/repository/sku_inventory.go#L163) | `inventory_balances`, `inventory_movements` | I |
| [`restoreCapturedReturnTx`](../../backend/internal/repository/sku_inventory.go#L208) | `order_inventory_captures`, `order_inventory_returns`, `product_variants` | R |
| [`captureOrderInventoryTx`](../../backend/internal/repository/sku_inventory.go#L273) | `order_inventory_captures`, `product_variants`, `products` | I |
| [`consumeInventoryBalancesTx`](../../backend/internal/repository/sku_inventory.go#L378) | `inventory_balances`, `inventory_movements` | I |
| [`restoreOrderInventoryTx`](../../backend/internal/repository/sku_inventory.go#L430) | `inventory_balances`, `inventory_movements`, `order_inventory_captures`, `product_variants`, `products` | I |

### sku_stock.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SetTrackedSKUStockTx`](../../backend/internal/repository/sku_stock.go#L16) | `inventory_balances`, `inventory_movements` | I |

### storage_fee_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`RunStorageFeeAssessment`](../../backend/internal/repository/storage_fee_repo.go#L158) | `warehouse_storage_fee_charges`, `warehouse_storage_positions` | S |
| [`advanceStorageAssessed`](../../backend/internal/repository/storage_fee_repo.go#L295) | `warehouse_storage_positions` | S |

### storage_position_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`creditStoragePositionTx`](../../backend/internal/repository/storage_position_repo.go#L59) | `warehouse_storage_positions` | S |
| [`decrementStoragePositionsFIFO`](../../backend/internal/repository/storage_position_repo.go#L100) | `warehouse_storage_positions` | S |
| [`consumeShippedOrderPositionsTx`](../../backend/internal/repository/storage_position_repo.go#L197) | delegates | S |

### store_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SaveWidgetConfig`](../../backend/internal/repository/store_repo.go#L24) | `stores` | M |
| [`CreateStore`](../../backend/internal/repository/store_repo.go#L219) | `stores` | M |
| [`SetStoreProductOverride`](../../backend/internal/repository/store_repo.go#L234) | `store_product_overrides` | C |
| [`SetStoreNicheOverride`](../../backend/internal/repository/store_repo.go#L246) | `store_niche_overrides` | M |
| [`SetStoreMargin`](../../backend/internal/repository/store_repo.go#L258) | `stores` | M |
| [`CreateStoreWithBonus`](../../backend/internal/repository/store_repo.go#L313) | `referral_bonuses`, `stores` | A |
| [`RecordCustomerStoreLink`](../../backend/internal/repository/store_repo.go#L443) | `customer_store_links` | M |

### supplier_panel_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SetCampaignStatus`](../../backend/internal/repository/supplier_panel_repo.go#L38) | `supplier_campaigns` | M |

### supplier_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateMember`](../../backend/internal/repository/supplier_repo.go#L164) | `supplier_members`, `user_capabilities`, `users` | M |
| [`SetMemberActive`](../../backend/internal/repository/supplier_repo.go#L192) | `supplier_members` | M |
| [`UpdateMemberRole`](../../backend/internal/repository/supplier_repo.go#L207) | `supplier_members` | M |
| [`RegisterSupplier`](../../backend/internal/repository/supplier_repo.go#L315) | `suppliers`, `users` | M |
| [`ApproveSupplier`](../../backend/internal/repository/supplier_repo.go#L438) | `suppliers`, `users` | M |
| [`RejectSupplier`](../../backend/internal/repository/supplier_repo.go#L468) | `suppliers` | M |
| [`insertAdminSupplierAuditTx`](../../backend/internal/repository/supplier_repo.go#L531) | `audit_logs` | N |
| [`SuspendSupplier`](../../backend/internal/repository/supplier_repo.go#L543) | `suppliers` | M |
| [`ReactivateSupplier`](../../backend/internal/repository/supplier_repo.go#L578) | `suppliers` | M |
| [`RejectProposal`](../../backend/internal/repository/supplier_repo.go#L627) | `pending_approvals` | C |
| [`notifyProposalRejected`](../../backend/internal/repository/supplier_repo.go#L690) | delegates | N |
| [`UpdateSignedContractVersion`](../../backend/internal/repository/supplier_repo.go#L804) | `suppliers` | M |
| [`CreateDirectProduct`](../../backend/internal/repository/supplier_repo.go#L951) | `products` | C |
| [`CreateExclusivityChallenge`](../../backend/internal/repository/supplier_repo.go#L1038) | `exclusivity_challenges` | M |
| [`ProposeProduct`](../../backend/internal/repository/supplier_repo.go#L1077) | `pending_approvals` | C |
| [`ApproveProposal`](../../backend/internal/repository/supplier_repo.go#L1224) | `pending_approvals`, `product_attribute_values`, `product_images`, `product_niches`, `product_variants`, `products` | C |
| [`setSupplierSellableTotalTx`](../../backend/internal/repository/supplier_repo.go#L1873) | `inventory_balances` | I |
| [`UpdateProductStock`](../../backend/internal/repository/supplier_repo.go#L1901) | delegates | I |
| [`CreateReturnRequest`](../../backend/internal/repository/supplier_repo.go#L1958) | delegates | R |
| [`CreateReturnRequestWithIdempotency`](../../backend/internal/repository/supplier_repo.go#L1962) | `inventory_balances`, `inventory_movements`, `inventory_reservations`, `supplier_return_events`, `supplier_return_requests` | R |
| [`ReviewReturn`](../../backend/internal/repository/supplier_repo.go#L2151) | `supplier_return_events`, `supplier_return_requests` | R |
| [`releaseReturnReservations`](../../backend/internal/repository/supplier_repo.go#L2205) | `inventory_balances`, `inventory_movements`, `inventory_reservations` | R |
| [`consumeReturnReservations`](../../backend/internal/repository/supplier_repo.go#L2271) | `inventory_balances`, `inventory_movements`, `inventory_reservations` | R |
| [`CancelReturn`](../../backend/internal/repository/supplier_repo.go#L2342) | `supplier_return_events`, `supplier_return_requests` | R |
| [`HandoverReturn`](../../backend/internal/repository/supplier_repo.go#L2393) | `supplier_return_events`, `supplier_return_parcels`, `supplier_return_requests` | R |
| [`ReceiveReturn`](../../backend/internal/repository/supplier_repo.go#L2449) | `inventory_balances`, `inventory_movements`, `supplier_return_events`, `supplier_return_parcels`, `supplier_return_requests` | R |
| [`InspectReturn`](../../backend/internal/repository/supplier_repo.go#L2506) | `inventory_balances`, `inventory_movements`, `supplier_return_events`, `supplier_return_requests` | R |
| [`RequestWarrantyProperty`](../../backend/internal/repository/supplier_repo.go#L2611) | `pending_approvals`, `warranty_property_requests` | M |
| [`insertSupplierAuditTx`](../../backend/internal/repository/supplier_repo.go#L2635) | `audit_logs` | N |
| [`UpdateProductPricing`](../../backend/internal/repository/supplier_repo.go#L2644) | `products` | C |
| [`CreatePriceIncreaseExceptionTicket`](../../backend/internal/repository/supplier_repo.go#L2701) | `support_tickets` | M |
| [`CreateInbound`](../../backend/internal/repository/supplier_repo.go#L2740) | `inbound_shipments` | I |
| [`DispatchInbound`](../../backend/internal/repository/supplier_repo.go#L2933) | `inbound_shipments` | I |
| [`CancelInbound`](../../backend/internal/repository/supplier_repo.go#L2959) | `inbound_shipments` | I |
| [`CreateCartonShipment`](../../backend/internal/repository/supplier_repo.go#L3017) | `shipment_items`, `shipments` | I |
| [`ReceiveWarehouseShipment`](../../backend/internal/repository/supplier_repo.go#L3187) | `shipment_items`, `shipments` | I |
| [`AddProductImages`](../../backend/internal/repository/supplier_repo.go#L3433) | `product_images` | C |
| [`resequenceGalleryTx`](../../backend/internal/repository/supplier_repo.go#L3464) | `product_images` | M |
| [`DeleteProductImage`](../../backend/internal/repository/supplier_repo.go#L3485) | `product_images` | C |
| [`SetProductCoverImage`](../../backend/internal/repository/supplier_repo.go#L3516) | delegates | C |
| [`ReorderProductImages`](../../backend/internal/repository/supplier_repo.go#L3544) | delegates | C |
| [`AddVariantImages`](../../backend/internal/repository/supplier_repo.go#L3643) | `product_variant_images` | C |
| [`resequenceVariantGalleryTx`](../../backend/internal/repository/supplier_repo.go#L3686) | `product_variant_images` | C |
| [`DeleteVariantImage`](../../backend/internal/repository/supplier_repo.go#L3695) | `product_variant_images` | C |
| [`ReorderVariantImages`](../../backend/internal/repository/supplier_repo.go#L3726) | delegates | C |
| [`UpdateVariantPhysical`](../../backend/internal/repository/supplier_repo.go#L3758) | `product_variants` | C |
| [`UpdateVariantSale`](../../backend/internal/repository/supplier_repo.go#L3787) | `product_variants` | C |
| [`CreateProductVariant`](../../backend/internal/repository/supplier_repo.go#L3862) | `product_variants` | C |
| [`CreateProductVariantsBulk`](../../backend/internal/repository/supplier_repo.go#L3905) | `product_variants` | C |
| [`UpdateVariantCartLimits`](../../backend/internal/repository/supplier_repo.go#L3974) | `product_variants` | C |
| [`UpdateVariantPricing`](../../backend/internal/repository/supplier_repo.go#L4107) | `product_variants` | C |
| [`UpdateProductSupplyOption`](../../backend/internal/repository/supplier_repo.go#L4232) | `products` | C |
| [`CreateCampaign`](../../backend/internal/repository/supplier_repo.go#L4252) | `supplier_campaigns` | M |
| [`RespondDiscrepancy`](../../backend/internal/repository/supplier_repo.go#L4371) | `qc_discrepancies` | M |
| [`RequestSettlement`](../../backend/internal/repository/supplier_repo.go#L4396) | `payout_requests` | A |
| [`RequestSupplierShippingMethod`](../../backend/internal/repository/supplier_repo.go#L4661) | `supplier_shipping_methods` | M |
| [`DecideSupplierShippingMethod`](../../backend/internal/repository/supplier_repo.go#L4688) | `supplier_shipping_methods` | M |
| [`UpdateTapinShopID`](../../backend/internal/repository/supplier_repo.go#L4716) | `suppliers` | M |
| [`UpdateTapinToken`](../../backend/internal/repository/supplier_repo.go#L4749) | `suppliers` | M |
| [`CreateSupplierWarehouse`](../../backend/internal/repository/supplier_repo.go#L4824) | `supplier_warehouses` | M |
| [`UpdateSupplierWarehouse`](../../backend/internal/repository/supplier_repo.go#L4848) | `supplier_warehouses` | M |
| [`UpdateProductOrigin`](../../backend/internal/repository/supplier_repo.go#L4925) | `products` | C |
| [`CreateSupplierReturnBack`](../../backend/internal/repository/supplier_repo.go#L5397) | `supplier_return_back_events`, `supplier_return_back_requests` | R |
| [`transitionSupplierReturnBack`](../../backend/internal/repository/supplier_repo.go#L5519) | `supplier_return_back_events`, `supplier_return_back_requests` | R |
| [`CancelSupplierReturnBack`](../../backend/internal/repository/supplier_repo.go#L5567) | delegates | R |
| [`CompleteSupplierReturnBack`](../../backend/internal/repository/supplier_repo.go#L5571) | delegates | R |
| [`CreateSupplierAddon`](../../backend/internal/repository/supplier_repo.go#L5709) | `product_addons` | M |
| [`UpdateSupplierAddonInPlace`](../../backend/internal/repository/supplier_repo.go#L5738) | `product_addons` | M |
| [`StageSupplierAddonUpdate`](../../backend/internal/repository/supplier_repo.go#L5756) | `pending_approvals` | M |
| [`SetSupplierAddonActive`](../../backend/internal/repository/supplier_repo.go#L5772) | `product_addons` | M |
| [`SetSupplierAddonAttachments`](../../backend/internal/repository/supplier_repo.go#L5790) | `product_addon_attachments` | M |
| [`ResubmitCatalogReview`](../../backend/internal/repository/supplier_repo.go#L6114) | `pending_approvals`, `proposal_field_reviews`, `proposal_review_history` | M |
| [`UpdateSupplierProfile`](../../backend/internal/repository/supplier_repo.go#L6222) | `suppliers` | M |

### supplier_requested_attributes.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`approveRequestedAttributes`](../../backend/internal/repository/supplier_requested_attributes.go#L12) | `dk_attribute_values`, `dk_attributes`, `dk_category_attribute_values`, `dk_category_attributes`, `product_attribute_values` | C |

### supplier_return_sku.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateReturnRequestForVariantWithIdempotency`](../../backend/internal/repository/supplier_return_sku.go#L21) | `inventory_reservations`, `supplier_return_events`, `supplier_return_requests` | R |
| [`moveSupplierReturnReservationsSKUTx`](../../backend/internal/repository/supplier_return_sku.go#L189) | `inventory_reservations` | R |
| [`receiveSupplierReturnSKUTx`](../../backend/internal/repository/supplier_return_sku.go#L249) | delegates | R |
| [`inspectSupplierReturnSKUTx`](../../backend/internal/repository/supplier_return_sku.go#L257) | delegates | R |

### supplier_shipment_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`CreateShipment`](../../backend/internal/repository/supplier_shipment_repo.go#L24) | `supplier_shipments` | O |
| [`MarkShipped`](../../backend/internal/repository/supplier_shipment_repo.go#L306) | `supplier_shipments` | O |
| [`MarkShippedForSupplier`](../../backend/internal/repository/supplier_shipment_repo.go#L331) | `supplier_shipments` | O |
| [`UpdateShipmentStatus`](../../backend/internal/repository/supplier_shipment_repo.go#L361) | `supplier_shipments` | O |
| [`LogShipmentAudit`](../../backend/internal/repository/supplier_shipment_repo.go#L396) | `supplier_shipment_audit_logs` | N |

### supplier_shipping_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SubmitShipping`](../../backend/internal/repository/supplier_shipping_repo.go#L20) | `orders`, `supplier_shipment_audit_logs`, `supplier_shipments` | O |
| [`AcceptShipment`](../../backend/internal/repository/supplier_shipping_repo.go#L76) | `supplier_shipment_audit_logs`, `supplier_shipments` | O |

### supplier_stock_v2.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`UpdateProductStockForVariant`](../../backend/internal/repository/supplier_stock_v2.go#L8) | delegates | I |

### tapin_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`ReplaceBoxes`](../../backend/internal/repository/tapin_repo.go#L80) | `tapin_boxes` | O |
| [`MarkTapinRegisterAttempted`](../../backend/internal/repository/tapin_repo.go#L374) | `supplier_shipments` | O |
| [`ClearTapinRegisterAttempted`](../../backend/internal/repository/tapin_repo.go#L386) | `supplier_shipments` | O |
| [`RecordTapinRegistration`](../../backend/internal/repository/tapin_repo.go#L397) | `supplier_shipments` | O |

### user_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`RecordLoginFailure`](../../backend/internal/repository/user_repo.go#L67) | `users` | M |
| [`ResetLoginFailures`](../../backend/internal/repository/user_repo.go#L81) | `users` | M |
| [`UpsertUser`](../../backend/internal/repository/user_repo.go#L91) | `users` | M |

### warehouse_optimization_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`SetBinPosition`](../../backend/internal/repository/warehouse_optimization_repo.go#L212) | `audit_logs`, `warehouse_bins` | I |

### warehouse_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`StartReceiving`](../../backend/internal/repository/warehouse_repo.go#L137) | `inbound_receiving_logs`, `inbound_shipments` | I |
| [`ScanItem`](../../backend/internal/repository/warehouse_repo.go#L175) | `inbound_receiving_logs`, `inbound_shipments` | I |
| [`CompleteInbound`](../../backend/internal/repository/warehouse_repo.go#L259) | `inbound_receiving_logs`, `inbound_shipments` | I |
| [`debitSupplierSellableForQCReceiptTx`](../../backend/internal/repository/warehouse_repo.go#L371) | `inventory_balances`, `inventory_movements` | I |
| [`RecordQCPass`](../../backend/internal/repository/warehouse_repo.go#L428) | `putaway_tasks`, `qc_results` | I |
| [`CompleteQC`](../../backend/internal/repository/warehouse_repo.go#L505) | `putaway_tasks`, `qc_discrepancies`, `qc_results` | I |
| [`applyInboundInventoryTx`](../../backend/internal/repository/warehouse_repo.go#L574) | delegates | I |
| [`fileInboundShortageTx`](../../backend/internal/repository/warehouse_repo.go#L668) | `qc_discrepancies` | I |
| [`ensureCanonicalCoverageTx`](../../backend/internal/repository/warehouse_repo.go#L683) | `inventory_balances`, `inventory_movements` | I |
| [`ensureInboundSourceBalanceTx`](../../backend/internal/repository/warehouse_repo.go#L711) | `inventory_balances` | I |
| [`upsertInventoryMovementTx`](../../backend/internal/repository/warehouse_repo.go#L720) | `inventory_balances`, `inventory_movements` | I |
| [`moveInventoryTx`](../../backend/internal/repository/warehouse_repo.go#L737) | `inventory_balances`, `inventory_movements` | I |
| [`RecordQCFail`](../../backend/internal/repository/warehouse_repo.go#L772) | `qc_results` | I |
| [`CreateQCDiscrepancy`](../../backend/internal/repository/warehouse_repo.go#L790) | `qc_discrepancies` | I |
| [`AssignPutawayTask`](../../backend/internal/repository/warehouse_repo.go#L854) | `putaway_capacity_reservations`, `putaway_tasks` | I |
| [`AssignPutaway`](../../backend/internal/repository/warehouse_repo.go#L910) | `putaway_capacity_reservations`, `putaway_tasks` | I |
| [`ConfirmPutaway`](../../backend/internal/repository/warehouse_repo.go#L963) | `putaway_capacity_reservations`, `putaway_tasks`, `warehouse_bins` | I |
| [`StartPickingOrder`](../../backend/internal/repository/warehouse_repo.go#L1147) | `orders`, `pick_tasks` | O |
| [`PickItem`](../../backend/internal/repository/warehouse_repo.go#L1191) | `pick_tasks` | O |
| [`CompletePickingOrder`](../../backend/internal/repository/warehouse_repo.go#L1208) | `orders`, `pick_tasks` | O |
| [`PackOrder`](../../backend/internal/repository/warehouse_repo.go#L1244) | `orders` | O |
| [`ShipOrder`](../../backend/internal/repository/warehouse_repo.go#L1311) | `orders` | O |

### wms_repo.go

| Writer / helper | Direct tables | Profile |
| --- | --- | --- |
| [`UpdateBinCapacity`](../../backend/internal/repository/wms_repo.go#L133) | `audit_logs`, `warehouse_bins` | I |
| [`DispatchManifest`](../../backend/internal/repository/wms_repo.go#L200) | `shipping_manifests` | I |


## Redis mutation entry points

Redis is a separate external system and does not participate in a PostgreSQL row-lock transaction. These six writers in `backend/internal/repository/redis.go` supplement the SQL inventory above; they must not be called while PostgreSQL business row locks are held.

| Writer | Invariant / event identity | Acquisition, retry and side effect | Owner |
| --- | --- | --- | --- |
| `SetAdToken` | One token claim during its TTL; identity ad token | Atomic Redis SET NX with expiry, bounded caller context; never financial authorization | K; FIN-007/012 when attached to a paid event |
| `SetJSON` | Cache value belongs to its key/version; identity cache key | Bounded SET with TTL after business commit; cache failure does not roll back a committed business event | M; owning cache/domain caller |
| `HoldInventory` | Temporary available hold cannot exceed the supplied stock snapshot; identity `(order, product)` | Atomic Lua, expiry cleanup and quantity replacement under caller context; PostgreSQL capture still revalidates actual stock. No SQL locks spanning Redis I/O | I/P; INV-05/12 |
| `ReleaseInventory` | Release a temporary `(order, product)` hold once | Atomic Lua DEL/ZREM, bounded retry after committed capture/cancel/expiry; TTL is fallback, not proof of stock restoration | I/O; INV-01/05, JOB-01 |
| `HasInventoryHold` | A matching unexpired hold is required; identity `(order, product)` | Lua may delete an expired hold, so this read-named method is also a writer; bounded caller context, outside SQL locks | I/P; INV-05/12 |
| `ReleaseOrderInventory` | Release each known product hold for the order | Delegates per-product release after the database event; partial external failure is observable/retryable under the same order/product identities | I/O/J; INV-01/05, JOB-01 |

## Independent SQL writers in HTTP/startup code

Pure delegating HTTP routes inherit the repository entry point contract above. The following runtime functions also execute SQL directly and must not bypass those guards. Worker entries here complement the explicit worker adoption table.

| Source / writer | Direct tables | Profile / owner |
| --- | --- | --- |
| [admin_exclusivity_handlers.go `wr69AdminChallengeReview`](../../backend/cmd/server/admin_exclusivity_handlers.go#L167) | `exclusivity_challenges` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminBanners`](../../backend/cmd/server/admin_ops_handlers.go#L30) | `banners` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminBannerByID`](../../backend/cmd/server/admin_ops_handlers.go#L112) | `banners` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminHomepageSections`](../../backend/cmd/server/admin_ops_handlers.go#L196) | `homepage_sections` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminHomepageSectionByID`](../../backend/cmd/server/admin_ops_handlers.go#L246) | `homepage_sections` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminHomepageSectionsReorder`](../../backend/cmd/server/admin_ops_handlers.go#L316) | `homepage_sections` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminSiteContent`](../../backend/cmd/server/admin_ops_handlers.go#L388) | `site_content` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminStaticPages`](../../backend/cmd/server/admin_ops_handlers.go#L448) | `static_pages` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminStaticPageByKey`](../../backend/cmd/server/admin_ops_handlers.go#L493) | `static_pages` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminCategories`](../../backend/cmd/server/admin_ops_handlers.go#L586) | `categories` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminWarehouses`](../../backend/cmd/server/admin_ops_handlers.go#L628) | `warehouses` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminTicketReply`](../../backend/cmd/server/admin_ops_handlers.go#L700) | `support_ticket_messages`, `support_tickets` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminReviewByID`](../../backend/cmd/server/admin_ops_handlers.go#L734) | `product_reviews` | M; ancillary owner; core profile required before money/stock |
| [admin_ops_handlers.go `handleT01AdminCustomerWithdrawalReview`](../../backend/cmd/server/admin_ops_handlers.go#L814) | `customer_wallets`, `customer_withdrawal_requests` | A; FIN-001/008/012 |
| [admin_ops_handlers.go `handleT01AdminSupplierPromotions`](../../backend/cmd/server/admin_ops_handlers.go#L935) | `supplier_promotions` | M; ancillary owner; core profile required before money/stock |
| [admin_payouts_handlers.go `wr69AdminPaymentRefund`](../../backend/cmd/server/admin_payouts_handlers.go#L189) | `ledger_entries`, `orders`, `payment_transactions` | P; FIN-003/010 |
| [admin_products_handlers.go `wr69AdminProductBrand`](../../backend/cmd/server/admin_products_handlers.go#L79) | `products` | C; INV-08, FIN-012 |
| [admin_products_handlers.go `wr69AdminProductOriginality`](../../backend/cmd/server/admin_products_handlers.go#L104) | `products` | C; INV-08, FIN-012 |
| [admin_products_handlers.go `wr69AdminProductPricingPut`](../../backend/cmd/server/admin_products_handlers.go#L160) | `product_margin_ceilings`, `products` | C; INV-08, FIN-012 |
| [admin_settings_handlers.go `t01SettingsPut`](../../backend/cmd/server/admin_settings_handlers.go#L66) | `platform_settings` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01Wishlist`](../../backend/cmd/server/customer_handlers.go#L45) | `customer_wishlists` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01Addresses`](../../backend/cmd/server/customer_handlers.go#L214) | `user_addresses` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01AddressByID`](../../backend/cmd/server/customer_handlers.go#L286) | `user_addresses` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01CustomerReviews`](../../backend/cmd/server/customer_handlers.go#L386) | `product_reviews` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01ViewEvent`](../../backend/cmd/server/customer_handlers.go#L532) | `product_view_daily`, `store_view_daily`, `view_dedupe_daily` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01NewsletterSubscribe`](../../backend/cmd/server/customer_handlers.go#L665) | `newsletter_subscribers` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01NewsletterUnsubscribe`](../../backend/cmd/server/customer_handlers.go#L721) | `newsletter_subscribers` | M; ancillary owner; core profile required before money/stock |
| [customer_handlers.go `handleT01CustomerWithdrawals`](../../backend/cmd/server/customer_handlers.go#L1043) | `customer_wallets`, `customer_withdrawal_requests` | A; FIN-001/008/012 |
| [finance_adjustments_handlers.go `wr69FinanceAdjustmentCreate`](../../backend/cmd/server/finance_adjustments_handlers.go#L73) | `finance_adjustments`, `ledger_entries` | A; FIN-001/008/012 |
| [finance_parity_handlers.go `handleT01MonthlyRun`](../../backend/cmd/server/finance_parity_handlers.go#L586) | `monthly_settlement_cycles`, `monthly_settlement_items`, `payout_requests` | A; FIN-001/002, JOB-01 |
| [finance_parity_handlers.go `handleT01EarlyWithdrawalReview`](../../backend/cmd/server/finance_parity_handlers.go#L865) | `payout_requests`, `seller_early_withdrawals` | A; FIN-001/002 |
| [ippanel_handlers.go `handleIPPanelSettings`](../../backend/cmd/server/ippanel_handlers.go#L62) | `platform_settings` | M; ancillary owner; core profile required before money/stock |
| [ippanel_handlers.go `logIPPanelTestSMS`](../../backend/cmd/server/ippanel_handlers.go#L167) | `sms_logs` | N; JOB-01 / event owner |
| [main.go `main`](../../backend/cmd/server/main.go#L91) | `sms_logs` | N; JOB-01 / event owner |
| [operations_handlers.go `handleResolveOverdueQC`](../../backend/cmd/server/operations_handlers.go#L68) | `audit_logs`, `qc_discrepancies` | J; JOB-01, INV-04 |
| [order_handlers.go `handleCreateOrder`](../../backend/cmd/server/order_handlers.go#L127) | `user_addresses` | O/K; INV-01/05, FIN-007/012 |
| [store_template_handlers.go `handleT01SellerStoreTemplate`](../../backend/cmd/server/store_template_handlers.go#L103) | `stores` | M; ancillary owner; core profile required before money/stock |
| [store_template_handlers.go `handleT01SellerStoreTemplatePublish`](../../backend/cmd/server/store_template_handlers.go#L183) | `stores` | M; ancillary owner; core profile required before money/stock |
| [supplier_notifications_handlers.go `markNotificationsReadForUser`](../../backend/cmd/server/supplier_notifications_handlers.go#L78) | `notification_outbox` | N; JOB-01 / event owner |
| [team01_helpers.go `t01SetPlatformColumn`](../../backend/cmd/server/team01_helpers.go#L197) | `platform_settings` | M; ancillary owner; core profile required before money/stock |
| [operations.go `RunOperationsCycle`](../../backend/internal/worker/operations.go#L12) | `ad_fraud_keys`, `ad_request_limits` | J; JOB-01 |
| [scheduler.go `runDeliveryLockReleaserCycle`](../../backend/internal/worker/scheduler.go#L156) | `orders` | J; JOB-01 |
| [scheduler.go `recordJobRun`](../../backend/internal/worker/scheduler.go#L269) | `job_runs` | J; JOB-01 |

`wr69AdminPaymentRefund` currently locks the payment before discovering/locking its order; FIN-003/FIN-010 must adopt order → payment → principals before recording refund effects. `wr69FinanceAdjustmentCreate` currently checks a recent-payload duplicate through `*sql.DB` before its transaction; FIN-001/FIN-012 must guard the typed principal, bind an immutable fingerprint and enforce a durable event identity independently of that heuristic. Customer-wallet and referral direct writers must use the same account guards as repository callers.

## Direct worker writers outside the repository package

| Entry point | Mutation / event | Profile and adoption |
| --- | --- | --- |
| `worker.BackgroundWorker.runDeliveryLockReleaserCycle` (`worker/scheduler.go`) | Bulk auto-confirm `orders`; delegates release or flag-only fallback | J/O/A; JOB-01, FIN-005/011. Discover sorted order IDs and account identities; remove financial fallback under owning task. |
| `worker.BackgroundWorker.recordJobRun` (`worker/scheduler.go`) | Ensures `job_runs` DDL and inserts run observation | J/N; JOB-01. Migration owns DDL; bounded independent observation after business result. |
| `worker.BackgroundWorker.RunOperationsCycle` (`worker/operations.go`) | Deletes expired ad limits/fraud keys, delegates expiry/QC/SLA, releases Redis holds | J/K/O; JOB-01, INV-01. Per-job contexts and external Redis release after database event. |

Migration runner and explicit development seeders are administrative schema/bootstrap writers, not concurrent application business mutation entry points. They must not run inside request/worker transactions. Paused `backend/internal/networkreward/` is outside this checkpoint and must remain excluded from commits and deployment; no adoption claim is made for it.

## Executable evidence and remaining gates

Run from repository root:

```powershell
python scripts/test-isolated.py -- go test ./internal/repository -run '^TestTransaction(LockOrder|Contract)' -count=1 -v
```

The runner provisions a unique disposable PostgreSQL database, migrates through the canonical runner, injects the explicit guarded test URL and verifies cleanup. `testdb.Open` validates the `sellersho_test_*` name and matching `public.sellersho_test_guard` marker before fixture writes. An absent explicit URL skips integrations; an unsafe/unmarked/unreachable explicit target fails. No production credentials or environment dump is required.

`TestTransactionContractTwoConnectionFlows` runs dispatch/cancel, payment/return, transfer/capture, payout/refund, storage assessment/departure and parcel charge/adjustment. Each pins two distinct PostgreSQL backend PIDs, discovers plans in opposite orders, uses the real shared helper, observes the second backend waiting on a row guard via `pg_stat_activity`, requires SQLSTATE `55P03` within the bounded interval, rolls back both owner and waiter writes, verifies original quantity and absence of failed-attempt effects, then reacquires and commits one fresh retry effect. The fixture tables live in unique schemas; they represent guard classes, not full domain schemas/flows.

`TestTransactionContractAllowlistMatchesMigratedSchema` checks every executable guard relation/key against the real migrated public catalog and requires a single-column primary key. Its initial RED run caught the reserved nonexistent placement relation; that name is now rejected until INV-06 implements it. `TestTransactionContractIndependentAccounts` proves seller A's held guard does not block seller B, supplier A or customer A and commits independent writes. `TestTransactionContractRejectsLateEarlierLocks` proves backward acquisition rejection, permitted reuse of a preheld guard, deadline requirement, rejection of cross-transaction lock-set reuse, missing-row failure and poisoned-state rejection. Unit tests prove whole-plan sorting/deduplication and fail-closed identifiers, including the future placement relation. These helper fixtures do not validate production authorization, tariff/account arithmetic, exact stock origins, provider delivery, duplicate business-event constraints or throughput. Those remain required under INV-12, FIN-013 and CAP-02 after domain adoption; document-only or skipped-fixture evidence cannot close CORE-01.

Verified 2026-11-14 on the guarded disposable runner: migrated-catalog validation, all six flow fixtures, independent-account and invalid-plan fixtures, and both unit tests passed **three consecutive runs** with no skips (`-count=3`, `ok .../internal/repository 14.865s`); container cleanup verified. The full backend suite was also executed: repository tests passed, while existing server measurement-validation and pricing-discount expectations failed. A concurrent intermediate migration-parser RED test was observed during that run; its owner subsequently reported the parser fix and reran its gate. The release owner records the final combined-tree results separately, since this helper gate does not stand in for the domain gates.


FIN-001 review corrections: service receipts fingerprint the immutable caller intention (owner/service/version), persist the original entitlement acknowledgment with accepted price, duration and expiration, and replay it before consulting current offers. The service HTTP response is that immutable acknowledgment, not a fresh mutable store read. Missing legacy acknowledgment evidence is reconciliation-required; it is never recharged automatically.

Seller statements use one read-only repeatable snapshot. Posted flows include released profit/bonuses/commissions, signed return reversals/adjustments, existing money-out payout states, service purchases and approved early fees. Pending/approved payouts and pending early gross remain reservations. Explicit opening/closing availability and reservation totals reconcile as `closingAvailable - openingAvailable = postedNet - (closingReserved - openingReserved)`. A separate current canonical available amount avoids presenting a historical cutoff as today's balance. Posting dates follow existing event dates (commission release shares the recorded order profit-release event); absent nonzero posting dates require reconciliation. This is a dated projection of accepted current records, not an invented complete historical state-transition journal; FIN-009 retains the full audit contract.

Legacy FIN fixtures now use guarded private schema-only DSNs for every pooled/reconnected session. Optional relation probes resolve the same unqualified relation as the associated SQL; default-public application behavior is unchanged. Test marker verification remains explicitly public-qualified. Fixtures clean up only their quoted unique schema, and verify public capture primary/foreign constraints survive setup from two independent pooled connections.

## INV-02 gated provenance adoption

The transfer interfaces previously marked planned are implemented for booking, dispatch, cumulative quarantine staging and undispatched cancellation. Existing shipment/inbound rows remain the work guards; `inbound_inventory_lines` is immutable subordinate metadata with database business identity, never an independently acquired late guard. The caller preholds supplier, all product/SKU parents and header before canonically locked physical balances. New ASN/carton booking reserves inside the header/line transaction. Receipt uses persisted allocations, never creates a guessed source, and creates no sellable/putaway entitlement. QC activation is explicitly closed until INV-04/09 replace the old second debit and pass retained positive integration gates; this is not completed cross-domain adoption. See [the checkpoint contract](inbound-transfers.md).


## FIN-007 order-owned coupon adoption

`orders.coupon_discount_amount` records the exact code layer independently of campaign/widget/sale amounts. Expected coupon ID/revision are trusted server-only fields. Historical attribution remains nullable; migration copies accepted prices and records uncertain identities/money/counter discrepancies rather than repricing. Occupancy is the count of reserved, consumed and review-held order ledger rows; only eligible unpaid cancellation releases it. Paid refund/return does not return capacity.

Existing lifecycle transactions retain order100 → payment200 → stores300/suppliers301/customer302 `FOR UPDATE`, now followed by coupon350 `FOR UPDATE` **before products400**. `lockOrderFinanceTx` performs this discovery and sets the transaction-local FIN007 marker for coupon-backed orders. Reservation/event rows are subordinate evidence, not late independent guards. Standalone coupon terms writers lock only their coupon and never acquire an order afterwards; revision changes only with actual terms changes.

Creation has a separate compatible FK-parent protocol. After a checkout-attempt guard (when keyed), discover all existing parents from the complete order/items/origins/shipments payload. Hold all stores (including referrer), suppliers and customer users with `FOR KEY SHARE` in canonical table/ID order **before** inserting the private new order. A duplicate private order identity aborts. Acquire coupon350 `FOR UPDATE`, then products400, variants500, warehouses550 and supplier_warehouses551 with `FOR KEY SHARE` before subordinate inserts. No creation guard is upgraded; ownership disagreement aborts rather than acquiring an earlier omitted parent. The attempt seals last. Replay returns the original committed order before consulting mutable terms or capacity.

Both public creation wrappers establish a bounded context before BeginTx/attempt/parent acquisition. A caller's deadline is preserved; legacy callers without a deadline receive a five-second fallback. The existing HTTP API budget remains eight seconds and existing lifecycle financial acquisition remains five seconds. PostgreSQL serialization/deadlock/lock-timeout errors (`40001`, `40P01`, `55P03`) retry the complete creation transaction at most three times with cancellation-aware bounded jitter within the same caller budget. Wrapping preserves SQLSTATE. Cancellation and domain/evidence/lock-contract errors never retry. Coupon exhaustion, terms/identity changes and evidence errors never reprice or silently create an undiscounted order. Repository concurrency tests under their stated thirty-second budget are not a claim of 100 simultaneous HTTP completions within the eight-second API budget.

| Writer or wrapper | FIN-007 adoption |
| --- | --- |
| `handleCreateOrder`; `CreateOrder`; `CreateOrderWithShipments`; `CreateCheckoutOrderWithShipments`; shared `insertOrderWithShipmentsTx` | Exact positive code contribution and expected revision; atomic reservation/count/event; shared private insertion and early keyed replay; typed 409 branches for keyed/unkeyed and advisory exhaustion. |
| `CreatePaymentTransaction`; `BeginPaymentVerification`; `RecordPaymentVerificationUnknown`; `RecordPaymentFailure` | Order-first finance/coupon prefix and local writer marker; token/failure/inquiry alone cannot consume or release. |
| `RecordPaymentVerification`; `MarkPaymentPaid`; `synchronizeVerifiedPaymentFactsTx`; `reserveExtraVerifiedCaptureTx` | Matching durable structured receipt consumes held capacity before inventory admission; legacy paid compatibility is review-held; duplicate receipt is idempotent. Released late money stays released and commits cancellation/FIN-003 compensation. |
| `CancelOrder`; `CancelOrderWithRefund`; `cancelOrder`; `RejectVerifiedPaymentAdmission`; `recordLatePaidCancellationTx`; `ExpireUnpaidOrders`/worker `expireOrders` | Same owning cancellation transaction classifies capacity. No receipt/booked payment/capture/unresolved inquiry permits eligible release; unknown money holds. Already-cancelled retries check evidence. Expiry selection remains pending, uncaptured and due. |
| `SetPaymentStatus`; `UpdateOrderFulfillment` | Classify under the locked order. Coupon-backed financial/cancellation bypasses return lifecycle-required. Plain-order setter/fulfillment behavior and FIN-005 dispatch checks remain. |
| Refund execution/admin refund/dispute/return/delivery/warehouse/supplier dispatch | Existing financial prefixes prehold the coupon where applicable. Consumed use remains occupied; unrelated metadata/refund bookkeeping does not require a new reservation or release capacity. |
| `CreateDiscountCode`; `SetDiscountCodeActive`; `IncrementDiscountCodeUse` | Terms revision is database maintained; standalone increment is a typed lifecycle-required error with no SQL mutation. Active order evidence survives deactivation. |

The additive `202611170001` migration drains orders/attempts/payments/inquiries/verification events/receipts/coupons with whole-transaction NOWAIT refusal before backfill and fences. Narrow BEFORE fences reject unmarked coupon-backed money/admission/cancellation changes without acquiring parent locks from child triggers. Immutable snapshot/terminal-state rules and deferred order/receipt/event/count consistency apply even to marked writers. Referenced order/coupon/store evidence cannot cascade away. Populated down migrations refuse evidence loss. Old code rollback retains restricted coupon availability; fences must remain installed.

Writer audit: production SQL searches cover `backend/cmd`, `backend/internal/repository` and `backend/internal/worker`, including multiline order/payment projections and the existing refund helpers. There is one shared production order INSERT, two guarded coupon count deltas, and no production independent-increment caller. Protected Persian documents and paused network-reward sources are excluded from the inventory and validation archives. Full release validation and actual prior-binary rehearsal receipts are recorded in the FIN-007 task report; this adoption map alone is not execution evidence.


Migration-origin unresolved identity is a narrow financial-fact exception: an immutable issue must match locked order/store/normalized code and its original snapshot. No concrete coupon is guessed or counted. Canonical receipt/refund/return/cancel writers may preserve accepted legacy money; new capture and dispatch remain denied. Captured/shipped history and paid replay remain intact. Uncaptured eligible receipts cancel and compensate through FIN-003; a legally blocked cancellation retains the receipt and review claim. The issue trigger rejects every runtime INSERT/UPDATE/DELETE, including marked writers, because review/import is outside this task. Missing schema or an unexplained absent reservation remains an error.

Only migration backfill may insert `legacy_snapshot` reservations; the post-backfill ledger fence rejects that origin for every runtime writer. Admission SQL constraints require a nonnull positive coupon amount and revision. A supplier-shipment BEFORE fence rejects new physical dispatch for unmarked coupon writers or absent/released/unconfirmed reservations without locking a parent from the child trigger. Adopted new dispatch uses the order/finance/coupon prefix before the shipment and requires a paid, noncancelled order. Prior custody is classified per shipment under the order guard, so existing shipped/in-transit/delivered/failure tracking and same-dispatch replay preserve FIN-005 physical history and carrier holds even for unresolved historical coupon identity. Another pending leg remains a new dispatch. The migration NOWAIT drain includes `supplier_shipments` before installing the child fence.

The runtime reservation fence takes/reuses its exact coupon owner `FOR UPDATE` guard at rank350 before the subordinate insert/products, then assigns admission `created_at` from the database clock and validates the window. Canonical creators already prehold that guard; an unmarked/origin-forged writer is rejected before guard acquisition. Supplier/payment child fences remain read-only with respect to parent locks. Caller timestamps and marked insert-first/counter-later waits cannot backdate or postdate admission. Deferred validation uses that fixed instant, so committing after expiry does not revoke a slot validly admitted before expiry. The same unpaid-evidence proof applies to runtime release transitions of historical and new reservations; unresolved inquiry, receipt or booked/unknown payment evidence cannot release capacity. A certified late receipt on an already released historical reservation remains a financial fact and leaves that reservation released. For unresolved historical aggregate orders, locked shipped-to-delivered progression and identical replay preserve existing physical facts; new or reverse dispatch still requires ordinary coupon evidence.
