# Physical bin occupancy — INV-06 core checkpoint

The warehouse records immutable measured placement lots (INV-08) and immutable
departure events/releases. Paid capture removes sellable availability while the
goods remain physically held. Picking and pre-departure cancellation do not free
capacity. Verified `ShipOrderWithActor` and supplier-return handover release the
actual source-bin allocations in the same transaction as custody transfer.

Every event retains its normalized source balances, product/SKU/supplier/warehouse,
bin and quantity, a SHA-256 payload identity and actor. SQL guards check the
actual shipped paid order or consumed return reservations and exact allocations.
Changed replay input is refused. A release cannot exceed either its placement or
its business event, including across multiple placements in one bin. The SQL
trigger changes bin counters atomically; the application does not subtract again.

Release uses the placement's archived measurement revision. It subtracts the
difference between rounded remaining lot totals before and after departure,
preserving cumulative rounding even when a small partial release frees0cm³.
Mixed revisions and different products share the bin under one ordered bin lock.
Supplier/product/SKU/work and balance guards precede bins702, placements704 and
storage705. Return creation, review, parcel preparation and handover share the
supplier-first prefix, including idempotent legacy replay.

All product/SKU allocation coverage in a bin is checked before any release. A
missing measured source keeps the entire bin conservative. Legacy raw bin keys
and incomplete warehouse metadata retain the custody transfer; their quantities,
volumes and weights are not guessed. Known bins become disputed; immutable source
exceptions retain unresolved identities. A missing bin row is reported without
creating a fictional bin. Re-verifying dimensions cannot silently clear an
unresolved physical-count exception. These bins need separately reviewed counted
reconciliation before capacity is made available.

Migration202611150001 does not backfill releases or rewrite previous physical
counters. Its audit includes balance-held and paid/unshipped units, measured lots,
historical handovers/dispatches without release evidence, partial restorations and
incomplete events. A populated downgrade refuses to discard the evidence.
Upgrade rehearsals use owned schemas with faithful pre-upgrade columns, preserving
the canonical fixture's already-retained exceptions. Full canonical migration and
domain tests run separately.

Read-only reports:

- `scripts/audit-warehouse-occupancy.sql`:500-bin keyset pages with optional
  warehouse filter and complete recorded/measured/custody evidence.
- `scripts/audit-warehouse-occupancy-orphans.sql`: bounded missing capture-source
  and unresolved departure identity evidence.

Use a read-only transaction with a statement timeout. Neither report authorizes
numerical repair. Their result is evidence for physical counts, never a sum of
current sellable stock used as physical occupancy.

This checkpoint does not close INV-06. The supplied acceptance also requires a
physical return into a bin to create a fresh measured placement. Existing return
receipt destinations are warehouse/supplier floors; a separate return-bin entry
substage remains before task closure. Normal ASN/carton refill through exact
putaway already creates a fresh measured placement and is covered by tests.
