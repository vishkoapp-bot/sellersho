# Storage quantity and time implementation plan

> Execute inline using the standing validate, commit, push, deploy.ps1 workflow.
> Start product changes after the INV-06 return-bin release is healthy. The source
> contract remains the supplied FIN-006 and INV-10 tasks, without modification.

**Goal:** Separate receipt ages and retain departed units' unpaid accrued storage;
both supplied probes must charge34800 rather than348000 or0. Wire canonical QC,
putaway, actual departures and customer-return receipt to the same receipt lots.

**Architecture:** Each accepted physical receipt event owns a distinct position
with immutable source, SKU, warehouse, timestamps, verified physical measurement,
tariff/free-window snapshot and policy version. Immutable quantity events preserve
the position's complete known history. Existing qty becomes a checked projection.
Assessment includes closed lots with outstanding completed days. Unknown historical
merged positions remain explicitly unreconciled and cannot acquire invented debt.

**Stack:** Go, PostgreSQL, existing integer money helpers and warehouse/finance APIs.

## Constraints and policy

- Preserve all existing charges and their supplier-balance deductions. Never
  recompute them from today's product measurement, tariff or current quantity.
- Production storage_fee_enabled was read-only verified false on2026-11-15. Keep
  it disabled; this implementation does not authorize enabling production fees.
- Physical arrival and billable QC acceptance remain different timestamps. Normal
  QC-approved staging and shelved units are physically held; failed QC and supplier
  premises are outside the existing central sellable-goods fee policy.
- Define the new version explicitly: a completed Tehran calendar day's quantity
  is the held quantity at its00:00 boundary, after the receipt's free window. A
  departure after that boundary retains that day's pending quantity until the day
  completes. Boundary-time departures precede that day's charge; a receipt after
  the boundary cannot be charged as though present at its start. Preserve the
  current default30-calendar-day free window. Test and document freeDays0/30 and
  exact-midnight ordering. Never apply this new version to unknown old history.
- Compute unit-day price with checked integer/rational arithmetic, existing money
  bounds and explicit rounding. Snapshot policy and physical revision at receipt;
  later edits apply to new eligible receipts, not old completed days.
- Internal warehouse transfer preserves receipt lineage, original billing age and
  policy. The day-boundary quantity belongs to exactly one source/destination;
  moving stock cannot reset the free window or create two charges for one unit.
- External customer-return re-entry starts a new QC receipt, physical measurement
  and fee cohort. Original departure stays closed. No credit at both QC and bin
  entry. Existing INV-06 return-bin storage insertion must be adopted atomically.
- Unknown measurement at QC records custody without inventing a billable snapshot.
  Explicit unresolved evidence blocks retrobilling until reviewed; a later catalog
  edit alone cannot certify historical physical dimensions or debt.
- Follow CORE-01 supplier/product/SKU/work/balance/bin/placement/storage ordering.
  Use actual event timestamps after acquiring guards for new departures; transaction
  start times must not backdate a later physical event across a fee checkpoint.
- Exclude docs/fa and paused networkreward. Do not change unrelated server services.

## Source facts to retain

- Canonical ASN and carton receipts already converge on inbound receipt lines and
  immutable QC dispositions (INV-02/04). Partial QC can accept the same receipt
  line in separate events; each event needs its own age/quantity cohort.
- createInboundPutawayTx receives that exact QC disposition. Current fee-position
  credit instead occurs inside consumeExactPutawayTx and merges same-product lots.
- Current assessment reads current product dimensions and qty, excludes closed
  positions, and therefore cannot recover the required historical quantity-days.
- Current shipment consumes product/warehouse FIFO; supplier-return handover uses
  product-level FIFO. Replace these with exact physical-source/placement lineage
  when consuming known lots, including mixed SKUs and multiple warehouses.
- Customer-return tracked floor source is per return item. Legacy merged return
  pools require unique attribution or explicit reconciliation; do not guess SKU.
- INV-06 departure releases identify exact measured placements; paid capture and
  pre-departure cancellation leave physical occupancy unchanged.

## Task1 — reproduce and define receipt/history schema

Files: storage_position_repo.go, storage_fee_repo.go, new focused storage-history
repository/test files, next unused migration003 (recheck numbering before writing).

- [ ] RED both34800 canonical probes before changing accounting. Retain original
  failure evidence. Add deterministic cutoff entry point for assessment tests;
  production wrapper still obtains its real current clock.
- [ ] Create immutable receipt/event identity, physicalReceivedAt, billingStartAt,
  exact SKU/source/warehouse, initial units, verified measurement and policy/rate
  snapshots. Add immutable quantity events and checked remaining-qty projection.
- [ ] Finalize creditStorageReceiptLotTx with the supplied line/product/supplier/
  SKU/warehouse/qty/physical/billing interface. Resolve the actual QC disposition
  unambiguously; retain its separate event ID. An internal explicit-event variant
  may support multiple accepted batches of one line. Never disguise a disposition
  ID as a receipt-line ID or merge late units into the first acceptance's age.
- [ ] Same-event replay validates payload before returning the existing receipt;
  changed quantity/source/SKU/time/warehouse cannot silently top up a prior lot.
- [ ] Migrate without inventing history: preserve old position IDs/qty/timestamps,
  charges and financial links; mark unproven old history explicitly unresolved.
  No automatic historic debt or old-age multiplication of today's quantity.
- [ ] Empty up/down/up preserves prior records; populated downgrade refuses loss
  of retained receipt/quantity/charge evidence. Keep canonical fixture intact.

## Task2 — assess historical quantities and retain closed accrual

- [ ] Replace current-qty historical multiplication with known per-receipt daily
  quantity history, frozen rate/measurement and free-window policy. Closed lots
  retain every completed unpaid eligible day, including pending closure-day work.
- [ ] No float intermediates, unchecked aggregate overflow or silent truncation.
  Keep existing supplier financial principal/account validation and atomicity.
- [ ] Unique receipt/day fee identity plus transactional checkpoints prevents two
  workers, replay, interruption and concurrent departure from charging twice.
- [ ] Discover/account/product/SKU/storage guards before child writes; reassess
  immutable identity/current history through the same transaction after locking.
- [ ] Bound the work per pass and expose remaining work honestly. Do not scan or
  lock the entire historical warehouse during a request, or erase pending accrual
  by advancing a checkpoint over unresolved/uncharged eligible history.
- [ ] Test both34800 probes, partial departure, mixed-age top-ups, closed lots,
  midnight/Tehran boundaries, zero/default free days, changing tariff/measurement,
  two workers, crash rollback and assessment/departure races.

## Task3 — adopt QC receipt and exact departure sources (INV-10 coupled gate)

- [ ] Emit one position per accepted QC event for both canonical legacy ASN and
  tracked carton paths, at QC acceptance with true immutable warehouse/SKU and
  original physical arrival. Partial QC creates distinct known cohorts.
- [ ] Putaway only links placement to its existing receipt cohort. Receipt/QC/
  assignment/confirmation retry creates no additional position or fee start.
- [ ] Physical customer-return QC creates one fresh attributable returned cohort;
  remove INV-06 bin-entry's separate credit when adopting that source. Unplaced
  returned floor stock, paid capture and later shelving keep one position.
- [ ] Consume actual departed source allocations/placement releases, including
  partial/multi-bin/multi-SKU/multi-warehouse shipments and supplier-return handover.
  Reserving or paying retains units; cancellation before departure changes none.
  Do not charge or debit a sibling SKU's cohort through product-level FIFO.
- [ ] Define/test transfer lineage and external re-entry. Wire any supported
  physical transfer writer; report unsupported/unproven legacy movement explicitly.
- [ ] Reconcile known receipt positions with held staging/bin/reserved/paid units
  through read-only bounded reports. Unresolved historic rows stay visible and
  excluded from new debt until a separately reviewed operational reconciliation.
- [ ] Upgrade meaningful old lifecycle tests that assert merge behavior; retain
  their conservation/replay coverage. Do not replace actual flows with skipped
  branches, fake source IDs or permissive runtime schema fallbacks.

## Final gates and delivery

- [ ] Independent finance/inventory review of sources, daily policy, rounding,
  closed accrual, migration preservation, replay and concurrent lock plans.
- [ ] Exact source freeze; full isolated PostgreSQL/Redis suite and affected
  frontend/API tests/build/vet. Correct every observed failure before release.
- [ ] Mark FIN-006/INV-10 DONE only after their complete coupled local acceptance;
  retain historical reconciliation, activation and intended-hardware qualifications.
- [ ] Explicit commit and push, then production deploy.ps1, backup/migration/health.

This plan is prepared while the preceding INV-06 release completes its final gate.
It is not included in that release and does not claim storage-history implementation.


## Implementation ledger (in progress; not a release)

BASE00b34da86f7446055b289223710b7b600dae5d79. INV-06 was pushed and deployed with
backup/migration/health success before this stage began. The production feature
storage_fee_enabled remains false. No storage-history commit/push/deploy yet.

Builder check: independent read-only review_storage_design found nothing
important left; current per-unit nearest-Toman rounding is retained with exact
rational arithmetic before quantity multiplication. Daily quantity at Tehran00
for completed days is the documented new-cohort policy, not a historical reprice.
Ruling: retain unknown old positions/charges without certifying receipt ages or
retroactive tariff/physical snapshots. New policy has an explicit cutover instant.

- Original two supplied failure probes were reproduced in guarded canonical PG:
  top-up charged348000 instead of34800; full departure charged0 instead of34800.
  logs/fin006-original-probes-before.log. Their current baseline fixture uses the
  old un-attributed helper and backdates an old position; when switching to the
  protected receipt interface, upgrade fixture provenance to known immutable
  receipt/QC/measurement/policy facts, preserving both exact34800 assertions.
  Never make legacy unknown history billable merely to satisfy these probes.
- Integer rate boundary reproduced9007199254740990 instead of9007199254740991.
  RED logs/fin006-unit-rate-before.log; GREEN fin006-unit-rate-after.log.
  storageFeeUnitDayToman now returns(int64,error), uses big.Int, validates physical/
  money bounds, rounds combined weight+volume once, and checks daily/run sums.
- Pure storage_history_policy.go models immutable departures, day-boundary quantity,
  closed accrual and transfer age. Includes Go time/tzdata so Tehran cannot silently
  fall back to UTC in minimal containers. Math/policy tests pass (5 top-level).
- Migration003 foundation preserves old qty/date/charges without inventing lots:
  warehouse_storage_history_policy cutover; warehouse_storage_receipt_lots;
  warehouse_storage_lot_departures; warehouse_storage_placement_lots; audit view.
  Populated down refuses retained evidence. Initial foundation6/6 passed.
- Actual QC receipt test first failed with zero positions for legacy and tracked.
  Source receipt helper now credits each accepted QC event, splitting units by
  physical receipt movement FIFO. Passed units precede failed units within a QC
  disposition; prior cumulative disposed units cannot be reused. Each lot records
  its actual receipt movement time and separate QC time, supplier/warehouse/physical
  SKU and receipt-time verified physical/rate snapshots. Unverified measurement
  creates an explicitly unbillable unresolved lot, with no invented dimensions.
- Ruling: credit after the immutable inbound_qc_dispositions INSERT, not while
  creating the earlier task/allocation. The first attempted hook read the QC event
  before it existed and failed; moving the hook after the event fixed the cause.
  New QC events use clock_timestamp() after guards, not transaction-begin NOW().
- Putaway skips credit when the QC cohort already exists; SQL placement AFTER
  trigger maps exact receipt cohorts without another position. Legacy fallback
  credit now creates a separate unresolved position and never merges into a
  certified position. All-known-date source helper wrappers retain true line IDs.
- Direct SQL forged event reproduced as accepted, then fixed with source insert
  guard checking actual post-cutover QC/receipt movement, FIFO unit intersection,
  source balance, account/SKU/warehouse, old position identity, verified physical
  revision at QC and exact current integer tariff snapshot. No free-form age/debt.
  Final focused3/3: migration preservation, actual ASN/SKU QC->putaway->replay,
  forged SQL source refusal. logs/fin006-source-guard-after.log.

Still REQUIRED before release:
- Source guard currently supports inbound_qc only; customer-return and physical
  transfer receipt lineage are not yet implemented. Do not claim completion.
- The departure table currently has immutability/schema only, no business-source
  insert guard/projection update yet. Existing shipment/return FIFO remains and
  must be replaced with exact allocations, including legacy unresolved treatment.
- RunStorageFeeAssessment still reads current product/current qty and excludes
  closed positions. Replace it with bounded known-lot snapshot/history assessment,
  unique daily charges, closed pending accrual, account guards and safe checkpoints.
- Need SQL charge/quantity-projection guards; known placement/floor unit caps;
  no retrobilling legacy positions; immutable source/measurement/policy links.
- Adopt customer-return QC before bin entry, remove duplicate INV-06 SQL credit
  for known new receipts while preserving safe unresolved old-entry custody.
- Update meaningful old lifecycle/fee fixtures to this canonical source contract;
  no permissive production schema fallback or skipped positive scenarios.
- Complete mixed-age34800 probes using known source fixtures, partial/multi-source,
  dates/rates/measurements, replay/concurrency/crash, parent-before-child locking,
  read-only reconciliation, independent final review and exact full-suite gate.

Known engineering details for continuation:
- Receipt movements are inventory_movements(reference_type=inbound_transfer,
  reference_id=lineID,to_status=quarantine,to_location_key=inbound-line:lineID).
  Partial arrivals have separate movement quantity/created_at; QC disposition
  prior offset=cumulative_disposed-passed_delta-failed_delta; passed interval is
  [prior,prior+passed_delta). This is the physical batch split used by the helper.
- Known lots link source_balance_id to qc-accepted:disposition or customer-return
  floor. Bin balances merge physical lots, so use warehouse_storage_placement_lots
  plus INV-06 actual occupancy releases to consume the right positions. Paid
  capture/reservation must not decrease held units before physical handover.
- Supplier-return reserved balances use a different row ID/status at the same
  physical location. Match proven pool identity and exact actual business caps,
  not supplier-wide/product-wide FIFO or a sibling SKU.
- Floor consumption cap is initial units minus all placement-mapped units minus
  already departed floor units. Mapping and departures must never reuse units.
- Legacy positions lack exact age/SKU/quantity history. Preserve primary shipping
  behavior and record unresolved evidence rather than invent fee debt/lineage.
- Parent/current measurement may lack dimensions. QC must still record custody
  as unresolved, rather than invent measurement or silently bill later from a
  catalog edit. Configuration bounds/invalid policy also need fail-closed handling.
- Existing apply_customer_return_bin_entry SQL (migration002) inserts WSP-<entry>;
  adoption must avoid that second credit once QC owns a known returned cohort.
  Old pre-cutover returns without a known QC fee lot still need conservative
  unresolved position coverage, never automatic retrospective certification.


## Current implementation and review (2026-11-15; awaiting final release gates)

The earlier REQUIRED list above is a chronological ledger, superseded by this
section. No storage-history release has been committed or deployed yet.

- Certified daily assessment now uses frozen receipt snapshots and indexed
  immutable remaining-quantity events; completed Tehran midnight quantities are
  retained after full/partial physical closure. It never reads today's product
  weight/rate to reprice an earlier receipt. Both supplied34800 probes pass.
- SQL guards derive actual shipment captures or consumed supplier-return sources,
  require exact SKU/warehouse/pool, actual measured placement releases, complete
  business-source quantities at deferred COMMIT, and unplaced floor caps. Quantity
  and closure project from immutable events; reservation/payment/cancellation
  retain custody. Missing legacy physical coverage records a complete immutable
  exception and blocks affected new accrual for separately reviewed reconciliation.
- Receipt-time tariffs and integer calculations are bounded. Invalid policy or
  overflowing derived rates preserve custody as unresolved_policy, retain the raw
  policy JSON, and cannot create debt. Unverified measurement remains unresolved;
  later catalog edits do not retrospectively certify it.
- Certified charge rows are immutable financial facts, with bounded cumulative
  certificates. SQL rejects wrong rates/amounts, unsourced quantity edits and any
  assessment checkpoint that would skip an unrecorded positive quantity-day.
  Existing old charges and balance deductions are preserved; no cash is moved.
- Central returned goods with immutable order-capture and receipt/QC evidence own
  a new customer_return cohort before shelving. Bin entry maps that same cohort;
  its old independent WSP credit is suppressed only for known returns. External
  re-entry restarts age at fresh QC. Failed/supplier-destination returns are not
  billable central sellable stock. Unallocated old orders retain existing custody
  behavior and remain uncertified; SQL refuses inventing their financial lineage.
- Positive old fee/lifecycle fixtures now use actual canonical known receipt facts,
  not backdated attributable positions or disabled guards. Their exact money,
  conservation and replay assertions remain. INV04 QC integration now runs in a
  fully migrated private schema, with only a uuid-ossp forwarding function for its
  isolated search_path. Old DTO-only stock-position tests explicitly have an empty
  copy of the receipt-lot relation and exercise their unresolved pre-cutover path.
- Multi-SKU/multi-warehouse actual QC/putaway/capture/shipment test proves separate
  frozen rates208/1008, held totals until physical handover and sibling quantities.
- Receipt policy migration preserves populated old rows and charges unchanged;
  down refuses retained accounting evidence. Applied001/002 are not modified.

Ruling: no new central-to-central transfer feature is invented in this release.
The existing inbound API accepts supplier premises -> central warehouse, or
explicit external replenishment, never a central source. Those existing writers
use one QC receipt contract. The pure transfer policy preserves root age/rate
and exactly one midnight custodian; unsupported persisted transfer inserts fail
closed. Any future central-transfer writer must implement its physical lineage
before billing it. Cost: that future feature cannot claim readiness from the
current pure-policy tests; production fees remain OFF.

Ruling: historical unallocated customer returns remain uncertified even when
legacy restock preserves physical custody. New billable cohorts require actual
order_inventory_captures in addition to QC/movement evidence. Cost: operational
review is needed for such old goods; no guessed supplier/age creates a debit.

Independent read-only review_storage_release found one important issue: LIMIT101
alone did not bound assessment discovery and the old index could not satisfy
NULLS FIRST/C-collated ordering. Fixed with a persistent transaction-owned keyset
frontier over id COLLATE C and matching index. Each pass examines <=100 receipt
identities before eligibility filtering, <=32 days per eligible lot, and retains
cycle pending work atomically with charges/checkpoints. Unresolved histories cannot
starve a later verified lot. The platform-settings guard serializes the frontier.
RED plan test fin006-frontier-before.log showed full-history sorting; GREEN
index-eligibility test uses EXPLAIN with plain indexed access enabled. It proves
an ordered index is available, not production-scale plan/throughput certification.
Actual102-receipt mixed unresolved/verified test completed its bounded cycle with
exact34800 and honest limited sample. fin006-frontier-backlog-after.log.

Other focused evidence:
- fin006-quantity-debt-guard-after.log:4/4.
- fin006-workers-recovery.log:3/3 (two workers, interruption rollback/recovery,
  concurrent partial physical departure and assessment; exact quantity-day sum).
- fin006-invalid-policy-after.log:4/4, including real legacy/V2 QC and immutable
  receipt source guards, invalid/overflowing policy custody and migration rehearsal.
- fin006-upgraded-lifecycle.log:13/13, including settlement balance deduction6960,
  day31 uniqueness, snapshots, free window and later-measurement noncertification.
- fin006-last-regressions-after.log:6/6, including corrected canonical INV04 fixtures,
  exact multi-warehouse/SKU custody, indexed frontier and old V2 return lifecycle.
- fin006-unallocated-return-before.log reproduced raw SQL certifying an old order
  without immutable capture; fin006-unallocated-return-after.log:2/2 after source
  guard fix, retaining successful actual legacy and tracked return cohorts.

First full discovery used an earlier migrated schema template while development
continued:635 tests/41 failures (fin006-full-discovery.log). This is not a release
gate. Its recheck41/37 isolated old-schema/canonical-fixture issues; the remaining
four were fixed and their focused checks passed. Final frozen full gate remains
required. No skips or permissive financial assessment/schema fallback were added.

Still OPEN regardless of local gates: production fee activation, evidence-based
legacy reconciliation, supported persisted central transfers if later added,
production-scale execution plans and intended-hardware1000-user/100RPS approval.


First frozen tree9f8b6039a4da220d364724e56301f511cc654d4c found one further
INV04 historical-queue fixture missing mandatory supplier_name. Its own synthetic
unallocated receipt header now supplies the real required name/count fields; all
queue visibility/reconciliation assertions remain. The corrected test passed in
fin006-last-frozen-fixture-after.log. No runtime code changed after this freeze.
A corrected full frozen-source run is required before committing this release.
Frontend frozen288/288, production build and repository/server go vet passed.


## Closed accrual versus a later unrelated exception (final fix pass)

Root reproduced one further financial interaction using actual business writers:
known measuredA received40 days ago -> placed -> paid/captured -> shipped/closed;
then a pre-cutover unknown-age staging taskB -> same-bin placement -> paid/captured
-> shipped with explicit unresolved-source exception. The later exception blocked
A's provably completed unpaid days, yielding0 instead of34800. RED
logs/fin006-closed-exception-before.log. The preserved old source fixture has real
receipt/QC/allocation/movement facts but deliberately no certified receipt lot;
no old age is promoted into new debt.

Fixed scope: storage_receipt_is_blocked excludes positions already closed at or
before the exception's creation. A later unrelated missing-history event cannot
invalidate an earlier fully proved closed receipt. Active incomplete cohorts still
remain blocked for operational reconciliation; their pending days/checkpoints are
not discarded and no partial guessed departure is inserted. Source exception
created_at is now derived after guards with clock_timestamp, rejecting free-form
backdating as an accounting-history control. Separate SQL RED:
logs/fin006-exception-clock-before.log (accepted2000 date).

GREEN: fin006-closed-exception-after.log5/5 (closed34800, actual partial concurrent
shipment, no invented exception clock, migration preservation); extra actual
mixed known/unreconciled active-source test and closed test2/2 in
fin006-exception-boundaries-after.log. Its source holds two units plus one later
old unknown unit, consumes the known first unit, then physically ships both
remaining units. Exact uncertified whole-source exception is retained; no partial
known-lot debit, no fee and no assessment checkpoint can pretend certainty. A
separately counted reconciliation remains required before active uncertain debt.

Ruling: operational uncertainty blocks an active cohort without erasing its unpaid
history, but cannot block a cohort completely closed before that uncertainty.
Cost: active uncertain cohorts still wait for counted operational reconciliation;
future unrelated events cannot suppress provably closed pending accrual.

The corrected64ab source full run is an intermediate baseline, superseded by this
SQL guard fix and its three regressions. A new full frozen release gate is needed;
production remains00b and fees OFF. No code has been committed/pushed/deployed.


## Final local gate2026-11-15

Release tree7377809a62086863f5e5a62134b559c798087054 passed639 entries/0fail with
verified disposable PostgreSQL/Redis cleanup; only the intentional migration
process helper entry skips, and its parent tests run. Final go vet passed.
Frontend288/288/build and Linux server/migrator builds passed on byte-identical
frontend/Go runtime sources. Both independent-review discovery and root financial
interaction fixes have observed RED->GREEN evidence. No important finding remains
unaddressed in this local release gate. Evidence documentation alone is updated
after this exact source/test freeze. Delivery follows commit,push,deploy.ps1;
production fees remain OFF and the operational/hardware qualifications above stay
OPEN. See public54-task progress ledger for task status and continuation.
