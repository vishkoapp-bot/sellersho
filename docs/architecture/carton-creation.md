# Carton creation and retry contract (INV-11)

`POST /api/v1/supplier/shipments` creates a carton header and its SKU lines as
one atomic operation. The authenticated supplier scope comes from the existing
supplier middleware. Requests must include `creation_key`, `warehouse_id` and
nonempty `items` containing `variant_id` and `expected_qty`.

## Request identity and responses

The client creates one operation key for an unchanged draft and retains it after
a timeout, lost response, malformed response or incomplete response. A successful
result must contain a nonempty shipment `id` and `shipment_code` before the UI
clears its draft or acknowledges success. The pending key remains in memory only;
draft/session changes or an acknowledged successful operation reset it.

Keys are scoped to the authenticated supplier. The database stores the key and
a canonical SHA-256 fingerprint of operation version, explicit destination and
sorted normalized SKU UUIDs with integer quantities. Line ordering does not
change the fingerprint. Structural validation rejects duplicate SKU lines,
nonpositive quantities, any quantity above `2147483647` and a cumulative quantity
above that bound before any write. No invalid line is silently discarded.

After structural validation:

- The same supplier/key and fingerprint return the existing shipment.
- Reusing that key for different canonical contents returns HTTP 409, including
  when a changed valid SKU UUID does not exist. Existing identity is compared
  before fresh catalog discovery can determine the response.
- A fresh key with a nonexistent SKU returns HTTP 404. Invalid input returns 400.
- Exhausted SQL lock/deadlock/serialization retries return 503. The caller must
  retry an unchanged operation with its original key.

Different keys identify different requested carton operations; identical SKU
contents alone cannot prove that two cartons represent the same physical parcel.
Legacy internal repository callers without a key receive a fresh operation key;
the HTTP route requires an explicit key.

## Transaction and stock-source boundary

The replay lookup and fresh discovery use the caller transaction without row
locks. Fresh creation then acquires the complete supplier, product and SKU guard
plan in the [CORE-01 order](transaction-contract.md), rechecks replay and
revalidates ownership, active destination and active owned supplier source through
that transaction. All lines must use one explicit supplier warehouse. The header
persists that source and the central destination; later catalog-origin changes
do not relocate a committed shipment. See [persisted origins](inventory-origins.md).

Header and lines commit or roll back together. A failure after the first line
leaves no header or lines. Code collisions use `ON CONFLICT DO NOTHING` for at
most five code candidates. SQLSTATE `40001`, `40P01` and `55P03` restart the entire
rolled-back attempt with fresh discovery and the original operation key, at most
three attempts under one ten-second deadline. The transaction-local lock timeout
is 1500 ms. These bounds do not establish a production capacity result.

Current carton creation does **not** enable INV-02 stock reservations. Its actual
write path, `createCartonShipmentTx`, is called under the repository-owned
transaction and preheld supplier/product/SKU guards. Future exact-source
reservation integration must run there before commit, using that same caller
transaction and the full additional guard plan. A helper with an independently
committed reservation cannot satisfy this contract. Historical stock sources or
reservations must not be invented to make a transfer succeed.

## Historical preflight before release

Migration `202611140005_carton_business_idempotency` adds supplier/key uniqueness,
shipment/SKU uniqueness and complete key/fingerprint validation. Historical rows
retain null operation identities. Duplicate shipment/SKU lines block migration;
the migration never deletes or merges received quantities or audit history.

Before applying it, use an explicitly selected review database connection and run
the independent read-only preflight from the repository root, for example in
PowerShell with the review connection already configured:

```powershell
if (-not $env:CARTON_REVIEW_DATABASE_URL) { throw 'Set an explicit review database connection first.' }
psql -X -v ON_ERROR_STOP=1 --dbname "$env:CARTON_REVIEW_DATABASE_URL" -f scripts/carton-creation-preflight.sql
```

The script executes `BEGIN READ ONLY` and reports orphan/empty headers, duplicate
SKU lines with their individual IDs and expected/received quantities, and matching
physical-claim candidates by supplier, immutable source/destination and sorted
SKU quantities. The migration's `carton_creation_reconciliation` view retains
orphan and matching-claim review flags. Matching claims require receipt/audit
review: legitimate separate cartons may have identical contents. Reconcile
exceptions through the existing audited administrative process; do not infer a
source from today's product origin or automatically delete or merge history.

## Executable verification

The disposable Docker runner migrates an isolated guarded test database; new
fixtures open it through `internal/testdb.Open` and never fall back to an
application database:

```powershell
python scripts/test-isolated.py -- go test ./internal/repository ./cmd/server -run '^TestCarton|^TestSKUInventory|^TestInventoryOrigin|^TestAuditSupplyWarehouseOrigin|^TestGap13Warehouse' -count=1 -v
npx tsx --test src/services/cartonDraft.test.ts src/services/supplierApi.sku.test.ts
npx tsc --noEmit
```

The affected suite covers integer bounds, rollback after a first-line write,
ownership, concurrent/replayed requests, response loss, code collisions,
transaction retry, migration up/down, unchanged historical received units and
read-only preflight. The client tests use the actual API client and cover both
truncated JSON and valid JSON with missing shipment identity. These tests do not
certify production-history reconciliation, INV-02 reservations or capacity.
