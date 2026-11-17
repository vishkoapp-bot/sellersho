# Warehouse occupancy implementation plan

> **For agentic workers:** Use superpowers:executing-plans inline. The user has
> already authorized the supplied backlog and continuous validate/commit/push/
> production deployment workflow; retain that execution method.

**Goal:** Release measured bin occupancy once when goods physically depart,
retaining capacity throughout payment, picking and pre-departure cancellation.

**Architecture:** Extend INV-08's immutable placement snapshots with append-only
departure events and per-placement releases. Resolve actual capture/return
allocations to their exact product/SKU/bin, acquire product/SKU/work, balances,
bins, placements and storage rows in the CORE order, and release the archived
amounts rather than current measurements. Historical unknown occupancy stays
conservative and disputed pending counted reconciliation.

**Tech stack:** Existing Go/PostgreSQL monolith, guarded disposable test runner.

**Spec:** INV-06 in2026-11-14-correctness-capacity-tasks.txt; INV-08 contract in
../../architecture/sku-measurement-capacity.md.

## Global constraints

- Never release at payment or pre-departure cancellation; never infer physical
  occupancy from current sellable quantities.
- No history deletion or inferred historical placement/backfill.
- Keep docs/fa and paused backend/internal/networkreward outside source delivery.
- Use deploy.ps1 after validated committed/pushed stages; preserve services,
  credentials and server-only protected backups.

## Review focus

- Multiple placements in one balance/bin with different measurement revisions:
  exact cumulative rounding must drain to zero without negative counters.
- Multiple concurrent departures, duplicate events, alternate return handover:
  release only their units once.
- Legacy historical captures missing immutable placement evidence: retain
  counters and expose reconciliation, never silently rebuild from sellable stock.
- Physical returns and supplier transfers: a fresh measured placement is needed
  for a new bin entry; cancellation of goods still held does not re-place them.
- Shared bins across products: acquire bins bytewise before placement rows and
  storage positions to avoid cross-product deadlocks.

## Task1 — immutable departures and exact release helper

Files: new backend/internal/repository/warehouse_occupancy.go and next unused
migration202611150001_warehouse_occupancy.up/down.sql; transaction_contract.go.
Interface: releasePlacementOccupancyTx(ctx context.Context, tx *sql.Tx,
eventID,eventType,actor string, allocations []InventoryAllocation) error.
InventoryAllocation identifies the actual product, SKU, supplier, warehouse,
source balance/bin and quantity. Existing immutable placements stay unchanged.

- [x] Reproduce actual receipt/assign/confirm/payment/ShipOrder failure in
  TestAuditSupplyShippingReleasesActualPutawayBinCapacity. Payment retains
  quantity1/1000cm3/100g; shipment must leave0/0/0 despite parent remeasurement.
- [x] Implement append-only event+placement release identity, checked cumulative
  rounding and strict ownership/coverage, with no negative counters.
- [x] Test multi-unit rounding, concurrent unique events,
  duplicate payload mismatch, archived measurement and empty/unknown evidence.

## Task2 — physical departure callers and lock plan

Files: warehouse_repo.go, order_fulfillment_guard.go, supplier_repo.go,
supplier_return_sku.go, warehouse_occupancy_integration_test.go.

- [x] Resolve actual order capture allocations and supplier-return source lots.
  Extend discovery/prelocking before existing storage-position locks.
- [x] Integrate release at verified ShipOrder and supplier-return handover in
  the same transaction. Never attach release to capture, picking or cancellation.
- [x] Test real legacy/tracked shipping, partial multi-bin handover, return
  handover, retries, cancellation and refill; keep baseline tests passing.

## Task3 — fresh entries and historical reconciliation

Ruling: migration001 already preserves some legacy exceptions in the canonical
template. Its destructive downgrade must refuse those rows. Rehearse historical
up/down/up in an owned SQL schema with the pre-upgrade column contract and retain
the original public fixture's evidence unchanged; never delete exceptions to make
the rehearsal pass. Full canonical migration and domain tests remain separate gates.

Files: exact putaway/return entry paths, scripts/audit-warehouse-occupancy.sql,
migration and docs/architecture/warehouse-occupancy.md.

- [ ] Verify every physical bin entry either creates a fresh immutable measured
  placement or is refused until exact measured putaway; retain existing physical
  custody on pre-departure cancellation.
- [x] Add a read-only bounded report using placements, capture/departure history
  and counter evidence. Mark unresolved historical bins disputed without changing
  quantities/volumes/weights or assuming paid units have left.
- [x] Rehearse upgrade on historical fixtures; guard immutable/replayed events
  and destructive downgrade. Test historical unresolved rows remain visible.

## Final validation and delivery

- [ ] Run affected migrated tests, full release-eligible PostgreSQL/Redis suite,
  frontend tests/build if changed, and Go vet.
- [ ] Independent correctness review; fix Important findings and rerun gates.
- [ ] Update ledger only for acceptance actually achieved; commit explicit files,
  push and deploy with deploy.ps1. Verify health and retain rollback backup.


Core checkpoint final evidence: frozen tree8372ea16604998e2e0ceab7c107fe107386135df,
603 tests/0 failures, only subprocess helper skip, both containers cleaned;
repository/server vet passed. Final independent core review approved the fixes.
Fresh-return entry remains open, so final task closure checkboxes stay open.

Ruling for the next substage: use a separate explicit post-QC customer-return item
putaway, retaining floor receipt. Resolve exact surviving tracked per-item floor
lots; accept legacy sources only when exact item attribution can be established.
Never move already captured/reserved floor goods or invent an inbound allocation.
The warehouse return tab is currently display-only. New numerical bin entries
must have an explicit return-source placement branch and immutable measurement.
