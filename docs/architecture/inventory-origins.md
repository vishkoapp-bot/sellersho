# Persisted inventory origins (INV-05)

Checkout and the standalone shipping quote plan stock through `ResolveInventoryOrigins`.
Only sellable lots with an active immutable warehouse or owned supplier-warehouse ID
can supply a named leg. Unknown history remains visible but cannot authorize a new
named-origin purchase. Multiple lots and multiple origins can split one purchased
line; `OrderItem.OriginSplits` is persisted transactionally in `order_item_origins`.
Warehouse labels and product flags never identify a physical lot. Carrier inquiry
uses the selected supplier warehouse rather than a default warehouse.

Capture revalidates each persisted split, its supplier/SKU and active origin. Split
quantities must sum to the purchased quantity. It debits only matching lots and
records both balance ID and origin-row ID. A shortage fails the transaction; it never
reroutes or reprices an authorized payment. Payment reconciliation/refund obligations
remain FIN-003/FIN-010 responsibilities; this change does not claim provider refunds.
Cancellation restores the recorded balance IDs exactly once, including after catalog
origin changes. Customer returns record their explicit destination, independently of
cancellation restoration.

New supplier declarations use an active owned product origin and a location key
containing that immutable ID. Changing the product origin does not move old lots.
New carton shipments persist their verified owned supplier source (`supplier_warehouse_id`)
and require an explicit active central destination (`warehouse_id`),
selected by the supplier UI from the authenticated central-warehouse list. SKU receipt debits the persisted carton source even after a product origin edit,
rejects unknown/insufficient source history, and copies the destination to the putaway task; bins must have the matching immutable
warehouse ID before putaway. New legacy inbounds similarly snapshot their verified supplier source at creation,
then the existing QC debit transfers exactly that recorded source; later product
origin edits affect future declarations only. Unknown or insufficient source history
rolls back QC instead of minting extra sellable stock. Existing historical inbound
sources remain unknown unless immutable evidence identifies them. The inbound
reservation/dispatch lifecycle remains INV-02; failed-QC accounting remains INV-04. Unknown bins are never identified from their current display names.

## Reconciliation and release prerequisite

Migration 202611140002 backfills only unambiguous existing inbound/task evidence.
It never infers a supplier warehouse from today's product origin, splits unknown
variant history, or moves a lot because its product origin changed. Unresolved rows
are exposed through `inventory_origin_reconciliation` and
`order_origin_reconciliation`, `carton_origin_reconciliation`, and
`inbound_origin_reconciliation`. The warehouse queue retains historical paid orders
and exposes `originReconciliationRequired` instead of hiding them.

Before production strict allocation is enabled, the release owner must reconcile
these views against physical receipts/tasks/captures. Record the evidence, actor and
explicit warehouse/bin assignment in the existing administrative audit process;
ambiguous history requires physical review. No automated name-based update is
permitted. Existing paid orders remain in their fulfillment queues for that explicit
exception process. Pending orders with unknown origins must be recovered through
payment reconciliation/refund handling rather than silently rerouted. Migration
success and health checks alone do not satisfy this data rollout prerequisite.

Storage consumption visits every persisted warehouse split and requires exact
position coverage. SKU putaway creates its named warehouse position once. Historical
missing positions require explicit reconciliation; a missing W2 position can never
be charged against W1 or silently ignored. Position quantity-time fees and the full
cross-domain guard adoption remain FIN-006/INV-12 responsibilities.

The current supplier shipment/provider model represents one parcel per supplier.
Multiple supplier pickup origins are rejected before checkout authorization and
again at registration for historical orders, requiring separate shipment support
before that configuration can be sold. Registration reads persisted split quantities
and identities and never substitutes today's product origin or the default pickup.

The allocation indexes cover `(product, SKU, origin, id)` with quantities included
for eligible stock reads. Production capacity and query latency measurements remain
INV-12/CAP-02 gates; the functional fixtures do not establish 100 RPS capacity.

## Verification

The guarded Docker runner first reproduced
`TestAuditSupplyWarehouseOriginCannotCaptureSupplierOnlyLot` returning a successful
capture against a supplier-only lot. The fixed regression rejects it. The integration
suite covers reverse direction, other central/supplier warehouses, inactive/unknown
origins, malformed splits, two-origin splits, future declarations after catalog origin
edits, concurrent last-unit capture, replay and exact cancellation restoration. SKU
receipt/QC/putaway/return fixtures use the actual origin-assignment trigger.

```powershell
python scripts/test-isolated.py -- go test ./internal/repository -run 'TestAuditSupplyWarehouseOrigin|TestInventoryOrigin|TestSKUInventory|TestGap13Warehouse' -count=2 -v
```

No fixture contacts a payment provider. A broader server run still has the existing
`TestProposeProductRejectsMissingMeasurements` schema-version/measurement expectation
failure; the shipping/quote/Tapin suite passes separately.
