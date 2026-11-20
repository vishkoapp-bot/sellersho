# Inventory reconciliation — operations guide (INV-12)

- **Scope:** the PostgreSQL inventory ledger (`inventory_balances`,
  `inventory_movements`, `order_inventory_captures`, `inventory_reservations`,
  warehouse storage lots/positions/departures) and the write paths that move
  units through it. Redis holds are advisory only and never appear in a
  reconciliation total.
- **Status:** Phase A (regression promotion) implemented in INV-12. Phase B
  (100-RPS probes, soak, migration rehearsal) is **blocked on owner
  decisions** — see "Blocked items" below. The dry-run reconciliation totals
  procedure in this doc is **blocked on a scrubbed representative snapshot
  that does not exist**; do not invent totals from production data.
- **Companion artifacts:** `~/workspace/inv12-audit/INV-12-audit.md` (audit,
  measurements, §2 write-path map, §4 EXPLAIN set) and
  `~/workspace/inv12-audit/explain-seeded.txt` (seeded EXPLAIN output).
  The load workload is `load-tests/inventory-100rps.js` (gated — do not run
  until the owner decisions below are answered).

## 1. Write-path / lock map (condensed from audit §2)

All paths are PostgreSQL-transactional, `FOR UPDATE` row-lock based. No
advisory locks exist in any inventory path. Lock order is the CORE-01
contract: parents first (`products`, then `product_variants`, both in
sorted-ID order via `lockInventoryParentsTx`), then balances in
`ORDER BY id COLLATE "C"`.

| Path | Entry point | Locks (in order) | Exactly-once mechanism |
|---|---|---|---|
| Capture | `MarkPaymentPaid` → `captureOrderInventoryTx` (`sku_inventory.go`) | parents; guard locks all sellable balances of the order's products; per-line origin balances | `order_inventory_captures` rows are immutable; replay returns success with no debit (count must equal line count or it errors `incomplete inventory capture`); movement idempotency key `sku-capture:<item>:<origin>:<balance>` |
| Restore / cancel | `CancelOrder` → `restoreOrderInventoryTx` | parents; capture rows `ORDER BY order_item_id`; balance rows for the parsed allocation IDs, sorted (INV-12 fix — see below) | movement key `sku-cancel:<item>:<origin>:<balance>`, `ON CONFLICT DO NOTHING` + `RowsAffected` check → `already restored` error; already-restored captures are a silent no-op; partial restores are refused |
| Origin | `consumeInventoryForOriginTx` (`inventory_origin.go`) | origin balances `ORDER BY id` | persisted `order_item_origins` split; warehouse origin consumes warehouse balances only, supplier origin supplier balances only; split must sum exactly to the line quantity |
| Transfer / receipt | `reserveInboundTransferTx`, `inboundCreditTx`, `receiveInboundLineTx` (`inbound_inventory.go`) | per-shipment work guard, then per-balance | cumulative `(line, total disposed)` is the durable identity; idempotent movement keys; receipt credit is `ON CONFLICT DO NOTHING` on the movement key |
| Departure (physical) | `consumeStorageBusinessEventTx` (`storage_physical_departure.go`) | positions via `LockTransactionRowsTx` | recorded-sum pre-check + unique `(position_id, event_type, event_id, source_balance_id, placement_id)`; a second consume of the same event skips; incomplete coverage writes a `warehouse_storage_history_exceptions` row, never inventory credit |
| Receipt | `ReceiveTrackedSKUShipmentItemTx` / `creditSKUInventoryTx` | product + variant | idempotent movement key; QC-failed units stay `quarantine`/`damaged`, never sellable before exact putaway |
| Supplier return | `CreateReturnRequestForVariantWithIdempotency` (`supplier_return_sku.go`) | product (supplier-scoped predicate) → variant → idempotency row → sellable balances sorted | idempotency-key row locked `FOR UPDATE` (replay returns the stored outcome); reservations move `sellable` → `return_requested` |
| Capacity / putaway | `AssignPutawayTask` / `ConfirmPutaway` (`warehouse_repo.go`) | task + bin (`FOR UPDATE`) | capacity reservation per task (`putaway_capacity_reservations`, one active per task); occupancy release on departure is once-only |

**Serialization:** per-product (and per-variant). Two orders sharing one SKU
serialize fully; orders on disjoint SKUs share no locks. The measured
bottleneck under contention is lock queueing + CPU/pool saturation, not
per-query cost (audit §3).

**INV-12 restore-guard fix.** The cancel-restore balance guard used to be an
`EXISTS … jsonb_array_elements` query over `order_inventory_captures`, which
forced a full sequential scan of `inventory_balances` inside the cancel
transaction while product locks were held. It is replaced by a Go-side parsed
allocation-ID list locked in sorted order (`restoreOrderInventoryTx`,
`sku_inventory.go`). Measured on the same seeded disposable DB (20k balances):
old 6.57 / 6.70 / 6.68 ms (Hash Semi Join + seq scan of 20,000 rows) → new
0.036 / 0.037 / 0.075 ms (PK index scan). At 100k balances the old query's
plan flips to a nested-loop PK lookup (0.59–0.75 ms) — the old cost was
plan-dependent, the new one is structurally a PK lookup (0.05 ms at 100k,
planning 0.09 ms vs 2.05 ms). No other index was added; no other index is
justified by measurement. The `RowsAffected` already-restored error is kept.

**§2.7 caveat — verified in the HTTP flow (INV-12).** Hold admission
(`reserveOrderInventory`, `payment_handlers.go`) reads `products.stock` (and,
for a tracked variant, `product_variants.stock` via `VariantInventory`).
Both columns are maintained by the `trg_inventory_balances_sync_stock`
trigger (`AFTER INSERT OR UPDATE OR DELETE ON inventory_balances`), which
fires synchronously inside the same transaction as the balance change — the
projection cannot lag behind a committed balance change. No migration and no
application code disables that trigger (verified by search); only a
privileged manual `ALTER TABLE … DISABLE TRIGGER` could stale it. If it ever
were stale, holds could admit against a higher-than-real stock — but the
PostgreSQL capture in `MarkPaymentPaid` still enforces the true balance
under `FOR UPDATE`, and the payment callback re-checks `HasInventoryHold`.
The failure mode is therefore a **late 409 `insufficient_stock`, not an
oversell**. No oversell path was found; no behaviour was changed.

**Redis is never authority.** Holds are admitted only if
`heldQty - ownQty + qty <= availableStock` where `availableStock` comes from
PostgreSQL. The Redis aggregate can only over-count (TTL-reaped quantity
keys); a rejection is re-checked against an exact full scan, which heals the
aggregate. Every quantity that moves money or stock is re-read from
PostgreSQL under `FOR UPDATE`.

## 2. Test file mapping (audit §1, Finding 1)

The spec's `audit_supply_test.go` / `audit_order_test.go` do not exist. The
six audit reproductions live here:

| Test | File | INV |
|---|---|---|
| `TestAuditSupplyReceiptDoesNotRecreateSoldTransferredUnits` | `backend/internal/repository/inbound_inventory_integration_test.go` | INV-02 |
| `TestAuditSupplyFailedLegacyQCRemovesDeclaredSellableStock` | `backend/internal/repository/inv04_disposition_integration_test.go` | INV-04 |
| `TestAuditSupplyWarehouseOriginCannotCaptureSupplierOnlyLot` | `backend/internal/repository/inventory_origin_integration_test.go` | INV-05 |
| `TestAuditSupplyLegacyAggregateEditDoesNotAddWarehouseStockTwice` | `backend/internal/repository/legacy_stock_adjustment_integration_test.go` | INV-03 |
| `TestAuditOrderPartiallyShippedCannotRestoreShippedStock` | `backend/internal/repository/order_cancellation_dispatch_test.go` | INV-01 |
| `TestAuditSupplyShippingReleasesActualPutawayBinCapacity` | `backend/internal/repository/warehouse_occupancy_integration_test.go` | INV-06 |

INV-12 adds `backend/internal/repository/inventory_workflow_integration_test.go`:
receipt-vs-capture, supplier-return-vs-capture, capacity-assignment-vs-departure
(three named races), double-departure concurrency, mixed-workload
conservation oracle, and the disposable-DB guard test. Baseline at INV-12:
35/35 §1 tests + the 6 new tests + `TestInboundTransferBookingPaymentRace`
+ the INV-04/origin suites, all green on disposable DBs, sequential runs via
`~/workspace/.dev/test-isolated-native.sh`.

## 3. Reconciliation queries

Run against a disposable copy / scrubbed snapshot, never against production
directly. All queries are read-only. `$PRODUCT` is one product id; drop the
filter for a whole-catalog run (expect it to be slow on large tables — that is
why the dry-run procedure below exists).

### 3.1 Per-lot movement sums vs balances

A full per-lot reconstruction (every balance derived from its movement
history alone) needs the opening snapshot — seeded balances have no
corresponding movement row — which is exactly why the dry-run procedure (§5)
is blocked on the snapshot. What is checkable without it:

```sql
-- Capture/restore movement totals vs the capture rows, per variant.
-- Clean when captured = capture_moves AND restored = cancel_moves.
SELECT c.variant_id,
       SUM(c.quantity) AS captured,
       SUM(c.restored_quantity) AS restored,
       (SELECT COALESCE(SUM(quantity),0) FROM inventory_movements m
         WHERE m.variant_id = c.variant_id
           AND m.reference_type = 'order_capture') AS capture_moves,
       (SELECT COALESCE(SUM(quantity),0) FROM inventory_movements m
         WHERE m.variant_id = c.variant_id
           AND m.reference_type = 'order_cancel') AS cancel_moves
FROM order_inventory_captures c
WHERE c.product_id = $PRODUCT
GROUP BY c.variant_id;
```

```sql
-- Aggregate-vs-lot drift: the product/variant projections must equal the
-- sellable balance sums (the sync_product_stock_projection trigger maintains
-- these; any drift means the trigger was bypassed or disabled).
SELECT p.id,
       p.stock AS product_projection,
       (SELECT COALESCE(SUM(quantity),0) FROM inventory_balances
         WHERE product_id = p.id AND status = 'sellable') AS balance_sum
FROM products p WHERE p.id = $PRODUCT
  AND p.stock <> (SELECT COALESCE(SUM(quantity),0) FROM inventory_balances
                    WHERE product_id = p.id AND status = 'sellable');

SELECT v.id,
       v.stock AS variant_projection,
       (SELECT COALESCE(SUM(quantity),0) FROM inventory_balances
         WHERE variant_id = v.id AND status = 'sellable') AS balance_sum
FROM product_variants v WHERE v.product_id = $PRODUCT
  AND v.stock <> (SELECT COALESCE(SUM(quantity),0) FROM inventory_balances
                    WHERE variant_id = v.id AND status = 'sellable');
```

```sql
-- Per-variant conservation across the workload that created the snapshot:
-- sellable + return_requested + captured - restored must equal what was
-- received into the variant. The received total comes from the snapshot's
-- opening balances (see §5); without them, assert the checkable part —
-- captured - restored never exceeds received — via the capture rows and
-- the receipt movements recorded after the snapshot was taken.
SELECT variant_id,
       COALESCE(SUM(quantity),0) AS receipt_moves_after_snapshot
FROM inventory_movements
WHERE product_id = $PRODUCT AND to_status = 'sellable'
  AND reference_type IN ('inbound_receipt', 'inbound_transfer')
GROUP BY variant_id;
```

### 3.2 Orphan-reservation check

```sql
-- Active reservations with no live return request behind them (0 rows = clean).
SELECT r.id, r.reference_type, r.reference_id, r.quantity, r.status
FROM inventory_reservations r
WHERE r.product_id = $PRODUCT AND r.status = 'active'
  AND NOT EXISTS (
    SELECT 1 FROM supplier_return_requests q
    WHERE q.id = r.reference_id AND q.status NOT IN ('cancelled', 'rejected'));

-- Reservation quantities must equal the return_requested balances they hold.
SELECT b.variant_id,
       (SELECT COALESCE(SUM(quantity),0) FROM inventory_balances
         WHERE variant_id = b.variant_id AND status = 'return_requested') AS requested_qty,
       (SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations
         WHERE variant_id = b.variant_id AND reference_type = 'supplier_return'
           AND status = 'active') AS reserved_qty
FROM inventory_balances b WHERE b.product_id = $PRODUCT
GROUP BY b.variant_id
HAVING (SELECT COALESCE(SUM(quantity),0) FROM inventory_balances
         WHERE variant_id = b.variant_id AND status = 'return_requested')
    <> (SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations
         WHERE variant_id = b.variant_id AND reference_type = 'supplier_return'
           AND status = 'active');
```

### 3.3 Origin-mismatch check

```sql
-- A capture allocation whose balance location disagrees with the origin leg
-- it was consumed for (0 rows = clean).
SELECT c.order_id, c.order_item_id, a->>'balanceId' AS balance_id,
       o.origin_type, b.location_type
FROM order_inventory_captures c
CROSS JOIN LATERAL jsonb_array_elements(c.allocations) a
JOIN inventory_balances b ON b.id = a->>'balanceId'
JOIN order_item_origins o
  ON o.order_item_id::text = c.order_item_id AND o.id = a->>'originId'
WHERE c.product_id = $PRODUCT
  AND ((o.origin_type = 'warehouse' AND b.location_type NOT IN ('warehouse','bin'))
    OR (o.origin_type = 'supplier' AND b.location_type <> 'supplier'));
```

### 3.4 Structural invariants (cheap, run first)

```sql
-- Negative balances are impossible at the DB level (CHECK quantity >= 0),
-- but assert it anyway: 0 rows = clean.
SELECT COUNT(*) FROM inventory_balances WHERE quantity < 0;

-- Duplicate capture movements (same idempotency key twice) — the unique key
-- prevents this; 0 rows = clean.
SELECT idempotency_key, COUNT(*) FROM inventory_movements
GROUP BY idempotency_key HAVING COUNT(*) > 1;

-- Captures whose allocations do not sum to the captured quantity.
SELECT order_item_id, quantity,
       (SELECT COALESCE(SUM((a->>'quantity')::int),0)
          FROM jsonb_array_elements(allocations) a) AS allocated
FROM order_inventory_captures
WHERE jsonb_array_length(allocations) > 0
  AND quantity <> (SELECT COALESCE(SUM((a->>'quantity')::int),0)
                     FROM jsonb_array_elements(allocations) a);

-- Departures that exceed their event's expected source quantity.
SELECT event_type, event_id, source_balance_id, SUM(quantity) AS departed
FROM warehouse_storage_lot_departures
GROUP BY event_type, event_id, source_balance_id
HAVING SUM(quantity) > (
  SELECT quantity FROM expected_storage_departure_sources(event_type, event_id)
  WHERE balance_id = source_balance_id);
```

## 4. Rollback note (inventory-specific)

- **Never drop provenance/placement data after writes.** A rollback must
  never delete `order_item_origins`, `order_inventory_captures` (including
  `allocations`), `inventory_movements`, `warehouse_storage_receipt_lots`,
  `warehouse_storage_positions`, `warehouse_placements`,
  `warehouse_storage_lot_departures`, `warehouse_storage_history_exceptions`,
  or `inbound_inventory_lines` / QC dispositions. The rollback test in
  `TestINV09PartialAcceptanceSourceGuardAndConcurrentReplay` proves the
  allocation-identity trigger guards and concurrent-replay exactly-once
  (it runs no down-script); the rule that a rollback must never erase
  used physical provenance must hold for every inventory migration.
- **Disable affected entry points first.** If a bad inventory release must be
  backed out, stop the entry points that write the new state (payment
  capture, receipt, putaway confirm, ship) before touching data, then decide
  between rolling back the binary and rolling forward a fix.
- **Roll forward if the old binary cannot read new states.** If the new
  release wrote states the old binary does not understand (new capture
  modes, new allocation shapes, new departure event types), do not roll the
  binary back — ship a forward fix. The generic procedure is in
  `docs/operations/rollback-runbook.md`; this section is the inventory
  addendum to it.
- **Restore is not a rollback.** `restoreOrderInventoryTx` (cancel) is a
  business operation with its own exactly-once rules (see §1); it is never
  a substitute for a deployment rollback.

## 5. Dry-run reconciliation totals procedure

**BLOCKED — do not run this procedure yet.** It requires a scrubbed
representative snapshot that does not exist. The missing input is owner
question (d): who produces the snapshot, and from what source? (FIN-014
territory.) No totals below are real; do not invent a snapshot from
production data.

When the snapshot exists, the procedure is:

1. Restore the snapshot to a disposable database; run all 212 migrations
   against it (the migration rehearsal) and confirm the schema verifies.
2. Run every query in §3 against the snapshot; record the totals (per-lot
   movement sums, orphan-reservation count, aggregate-vs-lot drift,
   origin-mismatch count, negative count, duplicate-movement count,
   allocation-sum mismatches, over-departures). Zero-defect target: every
   check returns 0 rows / 0 drift. Any non-zero result blocks enablement
   and goes to the reconciliation decision history (FIN-013's
   `reconciliation_decision_history` table) with a named owner.
3. Run the migration rehearsal a second time against the same snapshot
   (retry safety) and confirm identical totals.
4. Rehearse the rollback (§4): prove provenance/placement data survives and
   the old binary still reads the states the rehearsal wrote; if it cannot,
   the release rolls forward instead.
5. Save the workload, raw results, EXPLAIN outputs, and reconciliation
   totals as deployment-review artifacts (same artifact set as the
   Phase B probe runs).

## 6. Blocked items (Phase B — owner decisions still pending)

- **(a) Logging:** may the 100-RPS probe run before the REL-02 Q4 log fix,
  or must it wait? (100 RPS × 120 s with per-query SQL success lines risks
  ~24 GB/day-equivalent disk pressure on the target host; running with
  logging suppressed measures a different system than prod.)
- **(b) Hardware:** this VM saturates at ~35 captures/s under contention
  (audit §3.1) and ~1 s p50 even for disjoint SKUs (§3.2). Where does the
  probe run — a larger staging host, or deferred to CAP-02's prod-shaped
  hardware? No number from this VM can be quoted as a capacity result.
- **(c) Cancel admission:** FIN-013's per-account admission/queueing for
  `CancelOrder` is an open design decision (known 58–64/100 success rate
  under the current 5 s lock budget). Do dispatch/cancel races run under
  that budget, or wait for the decision? State the budget the probe is
  judged against.
- **(d) Snapshot:** who produces the scrubbed representative snapshot, and
  from what source? Blocks the migration rehearsal, the dry-run
  reconciliation totals (§5), and the inventory-specific rollback rehearsal.
- **Soak run:** needs the probes + fixed logging; CAP-02 owns final
  qualification. No capacity/SLO verdict comes out of INV-12 Phase A.
