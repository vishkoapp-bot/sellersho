# Customer return bin entry implementation plan

> **For agentic workers:** Use superpowers:executing-plans inline, with the user's
> standing validate/commit/push/deploy workflow. This implements the remaining
> supplied INV-06 acceptance; it does not replace floor receipt or its settlement.

**Goal:** Explicitly put received, QC-passed customer-return units into a bin once,
recording a fresh verified measured placement that later departs normally.

**Architecture:** Add a separate post-receipt action for an exact return item.
Consume only its proven surviving unreserved floor balance under ordered guards.
An immutable command links the return item, exact source, bin, actor, reason,
quantity, receipt date and measurement revision to a new return-source placement.
Never fabricate an inbound task or reopen the original shipped placement.

**Tech stack:** Existing Go/PostgreSQL and React warehouse UI/API client.

**Spec:** INV-06 in2026-11-14-correctness-capacity-tasks.txt;
../../architecture/warehouse-occupancy.md records the already-deployed core.

## Global constraints

- Preserve original return receipt/QC/shipping-settlement/refund behavior.
- Require central destination, received QC `ok`, exact item/source lineage,
  current verified effective SKU/product measurement and matching verified bin.
- Supplier-destined and defective floor returns remain outside this bin action.
- Never move already captured/reserved floor quantities. Do not reconstruct
  remaining stock from original return quantity or current catalog projections.
- Preserve immutable original placement, finance and historical counters.
- Standing excluded docs/fa and paused networkreward never enter delivery.
- Source/supplier/product/SKU/return/item guards precede balances700, bins702,
  placements704 and storage705. No upstream lock discovery after child locks.

## Review focus

- Lost response and duplicate requests: same payload returns original placement;
  a changed payload under one key409s without mutation.
- Partial bin entries and later paid floor captures: each entry consumes only
  the still-surviving exact source, within the return item's remaining quantity.
- Legacy merged sources: require provable unique item attribution; ambiguous
  same-product return lines or missing destination require reconciliation.
- Parent/SKU edits, other warehouse, insufficient reserved capacity and disputed
  counters fail closed before stock/placement changes.
- A returned unit may leave again: original release remains unchanged, fresh
  placement uses current verified revision and releases only its recorded amount.

## Task1 — source and placement command

Files: new backend/internal/repository/customer_return_putaway.go and integration
test; migration202611150002_customer_return_bin_entry.up/down.sql.

Interfaces:

- `WarehouseRepository.PlaceCustomerReturnItemInBin(ctx context.Context,
  returnID,returnItemID,bin string,quantity int,reason,actor,idempotencyKey string)
  (ReturnBinPlacementResult,error)`.
- `WarehouseRepository.ListCustomerReturnBinSources(ctx context.Context,
  returnID string) ([]ReturnBinSource,error)` returns exact item/product/physical
  SKU identity, current verified measurement, actual source remainder and reason
  when reconciliation is required; no fabricated availability.

- [x] RED actual receipt -> measured putaway -> paid capture -> shipment ->
  customer return approval/receipt -> explicit fresh bin entry -> another paid
  capture/shipment. Old placement stays released; new snapshot uses a separately
  verified measurement and both final counter totals reach zero.
- [x] Add immutable return-bin command, source/measurement/quantity/actor/reason,
  unique idempotency identity, exact item quantity cap and atomic no-orphan application.
- [x] Extend placements with explicit source kind and return-entry reference;
  inbound task identity stays mandatory for inbound placements, while return
  placements use no fake ASN task. Preserve the original insert/completion guards.
- [x] Validate and consume the exact floor source, insert transfer proof and new
  placement/counters atomically, honoring active putaway capacity reservations.
- [x] Test actual tracked and provable legacy sources, zero/fraction/overflow,
  wrong identities/warehouse, unverified override, disputed/insufficient capacity,
  captured/reserved quantity, partial entries, concurrent last-unit requests,
  changed replay and direct SQL forged/orphaned evidence. Rehearse upgrade safely.

## Task2 — warehouse API and form

Files: backend/cmd/server/main.go and focused warehouse return-putaway handlers /
HTTP tests; src/services/wmsApi.ts; WarehouseNewTabs.tsx (or a focused imported
component) and mounted browser test.

- [x] GET `/warehouse/customer-returns/{returnID}/putaway-sources` lists the source
  DTOs. POST `/warehouse/customer-returns/{returnID}/items/{itemID}/putaway`
  accepts `{binCode,quantity,reason}` and Idempotency-Key. Both require warehouse,
  logistics or admin capability and authenticated actor.
- [x] Use400 for malformed input,404 for missing/mismatched resources,409 for
  physical/source/measurement conflicts; never turn a conflict into success.
- [x] In the existing display-only customer-return tab, show exact item/SKU,
  QC/measurement/source remainder, matching bin choices and explicit quantity/
  reason. Use the existing measurement endpoint for warehouse verification.
- [x] Keep draft and idempotency key for an identical retry/lost response;
  changed item/bin/quantity/reason gets a distinct command. Refresh authoritative
  source/bin values after success, without optimistic fake availability.
- [x] Mounted test proves exact item selection, blank/invalid blocking, conflict
  draft retention, same-key retry and authoritative remainder.

## Final gate

- [x] Independent full INV-06 review, including every supported bin entry/counter
  writer, replay and historical data risks.
- [x] Freeze exact source; full migrated PostgreSQL/Redis suite, frontend tests/
  build, vet and migration preservation pass. Fix every observed failure.
- [x] Mark INV-06 DONE only when its complete local acceptance is met; retain
  historical counted reconciliation and intended-hardware qualification limits.
- [ ] Explicit commit/push and deploy.ps1 production release with verified health.

Rulings: a separate post-QC action is the smallest supported integration. The
existing warehouse customer-return tab is display-only, and the receipt API
already records QC and floor stock with financial settlement. Keep that behavior.
Tracked source keys are per return item; legacy custreturn pools can merge lines,
so ambiguous historical pools require counted reconciliation rather than guesses.
The reviewed runtime counter increment currently exists only in ConfirmPutaway;
WMS capacity/position edits do not reset physical counters.

Implementation ruling: BEFORE derives and validates the exact receipt source and
current physical/bin state under ordered locks. AFTER applies every conserved
effect in the same SQL statement, rather than retaining an independently inserted
header until a deferred check. This makes orphan headers impossible on late error.

Review regressions reproduced RED before corrections: identical concurrent full
quantity requests failing before uniqueness checking; downgrade column dependency;
and refunded return work disappearing from the queue. Final browser test also
proves changed requests get a new key and an identical retry survives a source
refresh after response loss. Focused14/14, browser1/1 and frontend288/288 passed.
First frozen tree f4aebca173db45ed6ffb2408911a98b2738247da ran615 with exactly2
legacy fixture failures (missing new source view). Queue filtering and COD refusal
now use canonical migrations and actual create/approve flows, preserving every
assertion. Corrected affected4/4 passed; independent review found no remaining
Critical/Important issue. Product runtime bytes are unchanged from that first
freeze. A second exact-source full gate precedes commit/push/deployment.

Final gate: exact tree eb66f58726b8df57dd195a74060ada7568c77664 passed615/615 with
only the intentional migration process-helper entry skipped and both disposable
containers removed. Archive vet passed; frontend288/288 and build passed. The
second source differs from the first only in the two reviewed fixture corrections
and this plan evidence. Commit/push/deploy follows this gate under the standing
workflow; the original backlog's capacity/historical reconciliation limits stay.
