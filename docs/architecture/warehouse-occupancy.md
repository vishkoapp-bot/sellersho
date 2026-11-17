# Physical bin occupancy — INV-06

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

Customer-return receipt/QC retains its existing warehouse/supplier floor stock,
shipping settlement and refund behavior. A separate post-QC action now moves a
proven surviving central-floor return item into a matching verified bin. The
warehouse customer-return tab shows the exact item/SKU, measured physical tuple,
actual available remainder and eligible destination bins. A completed refund does
not hide proven outstanding physical shelving work.

GET `/warehouse/customer-returns/{returnID}/putaway-sources` is advisory. POST
`/warehouse/customer-returns/{returnID}/items/{itemID}/putaway` requires warehouse,
logistics or admin capability, authenticated actor, positive integral quantity,
reason and Idempotency-Key. Malformed input returns400, missing/mismatched return
item404, source/measurement/capacity conflict409. A successful actor/key retains
its exact normalized request and result; altered replay fails. Concurrent exact
replays recover the committed result even when the first command exhausted the
source before the second command reached its uniqueness check. The form retains
draft/key across conflicts and response loss, including a later source refresh.

Migration202611150002 adds immutable `warehouse_return_bin_entries`. BEFORE
derives source ownership, exact product/physical SKU, warehouse, original receipt
time and current verified measurement from actual received items and receipt
movements; client-supplied physical snapshots cannot substitute for these facts.
The same order/account/product/SKU/return/item prefix precedes balances and the
bin. Surviving sellable floor quantity excludes already-captured or supplier-return
reserved units. Entry also caps cumulative per-item placement and accounts all
active inbound capacity reservations. Disputed or inconsistent bin counters refuse
new entries; historical totals are never reset.

AFTER applies source debit, destination credit, immutable transfer, fresh measured
placement, bin counters and a separate returned-goods storage position in one SQL
statement. Failure rolls back the header and every effect, so direct SQL cannot
commit an orphan command. `warehouse_placements.source_kind` distinguishes
inbound putaway (mandatory task) from customer return (mandatory return-entry,
NULL task). The original inbound insertion/completion guards remain intact; no
ASN or fake receipt task is fabricated. Subsequent departure consumes the new
placement's archived measurement; the original shipped lot remains released.
Returned-goods storage retains receipt time; FIN-006/INV-10 must adopt this source
when replacing the existing fee-position ledger.

Tracked receipt proof is per return item. A legacy `custreturn` balance may merge
same-product lines, so only a uniquely attributable QC-ok line with recorded
central destination is eligible. Ambiguous pools, unknown destinations, supplier
destinations and defective goods require their separate floor/reconciliation
flows; neither current catalog routing nor original return quantity invents stock.
Populated downgrade refuses to discard return entry evidence. Empty down/up keeps
all pre-existing inbound placements and numerical bin counters unchanged.

Local gates and delivery evidence are recorded in the execution ledger. Historical
production occupancy still needs counted reconciliation, and intended-hardware
1000-user/100-RPS capacity qualification remains open.
