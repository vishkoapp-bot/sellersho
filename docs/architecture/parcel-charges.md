# Parcel charge operations

`ConfirmReturnParcelCharge` keeps the existing confirmation API. Each original
carrier bill has one immutable `parcel_charge_receipts` row, one business identity
per parcel, and a durable key mapping including accepted retry aliases. Sender
confirmation posts one positive debit and ledger leg. Receiver confirmation
records its accepted amount (including NULL versus zero), source and reference
without posting money. Only draft parcels can first confirm. Unproven historical
confirmations require reconciliation.

Warehouse roles remain `warehouse_staff`, `logistics_specialist`, `admin`.
`POST /api/v1/warehouse/returns/{return}/parcels/{parcel}/correct-charge` requires
authentication and a trimmed 1..128 byte `Idempotency-Key`. Body example:

```json
{"expectedAmount":120000,"finalAmount":90000,"source":"manual","reference":"carrier-bill-2","reason":"carrier rebate"}
```

Both integer toman amounts are required and bounded by MaxTransportMoney
(9007199254740991), including zero. Source is tariff/api/manual; reference and
reason are required. Reference is at most 150 Unicode characters. An unchanged final amount
is invalid. HTTP 400 indicates invalid input/key, 401 missing actor, 403 denied
role, 409 stale amount, changed key payload, state or reconciliation conflict,
503 with Retry-After retryable lock saturation. A 200 response is the immutable
receipt containing ID, kind, bindings, old/new amount, terms, actor, time and event
ID. Retrying an earlier correction returns that same receipt even after a later
correction. Use a new key and the current effective amount for each new correction.

Corrections preserve supplier and payment method. Only sender-paid, previously
confirmed bills can change, including handed-over/in-transit/received parcels.
New effects on cancelled parcels or cancelled/rejected parents are blocked.
Exact accepted retries remain read-only recovery after cancellation. Positive
deltas produce debit/debit, negative deltas reversal/credit. The existing signed
supplier event projection remains authoritative; original evidence is unchanged.
Repeat confirmation compares the latest effective terms and never posts again.

Every write discovers supplier, acquires its typed account guard, then the
canonical return and parcel guards. Supplier and parent binding are revalidated.
Receipt/key metadata is subordinate to these locks; no new lock class is added.
All event, ledger, state and metadata writes share one transaction. The draft
transition and effective-amount update each require exactly one affected row.
No provider is called and no supplier balance floor is introduced.

Run `scripts/audit-parcel-charges.sql` against production before migration and
after release; it is a read-only transaction compatible with both schemas.
Every reported anomaly must be zero. Review ambiguity outside this rollout;
never deduplicate, delete, guess provenance or reprice old bills. Migration160001
also aborts atomically before mapping any history unless one sender debit has
confirmed parcel/actor, approved/packed parent, exact amount and fully bound
supplier/return/ledger evidence. It preserves existing event IDs, keys and dates.
Receiver history without durable acceptance remains held. Applied migrations
are sealed. Down is permitted only before any charge evidence exists; thereafter
roll forward. UPDATE/DELETE on charge receipts, keys and classified events is
rejected. Existing ledger append-only protection remains unchanged.

Verification uses guarded disposable PostgreSQL through scripts/test-isolated.py;
broader legacy suites require scripts/test-suite-isolated.py database clones.
No live financial fixtures or provider calls are part of these tests.

The production migration runs while the old backend is still serving. Before
historical preflight, 160001 takes suppliers SHARE ROW EXCLUSIVE, then ACCESS
EXCLUSIVE on supplier_return_requests, supplier_return_parcels, ledger_entries,
and supplier_return_financial_events, in that order. The supplier mode reserves
the lock needed for receipt foreign-key creation while permitting account row
lock readers to finish. Supplier ACCESS EXCLUSIVE would block those readers
while they retain return-discovery read locks, creating a preventable cycle.
Strong return/parcel locks drain existing FOR UPDATE holders before later locks;
a weaker write-excluding SHARE mode would permit a later UPDATE lock upgrade to
cycle with ledger/event acquisition. Ledger precedes events as the old debit
writer does. All evidence stays stable through classification/backfill and commit.
A previously uncommitted invalid debit or lifecycle change is seen after its
writer commits and aborts the migration without partial metadata. Later lifecycle
writes wait until backfill commits, then continue normally. The tradeoff is a
short table-wide pause of reads/writes on the four evidence relations, including
the shared ledger, and supplier writes. The existing migration transaction and
lock/statement timeouts bound the pause; timeout aborts the release rather than
loosening validation. No service shutdown or unrelated deployment change is
required. Retry only after reviewing the migration error and live evidence.

Older binaries do not write the required charge component or receipt and cannot
confirm new parcel charges after this migration. If an application rollback is
needed, keep parcel charge mutations unavailable and roll forward to compatible
code; do not remove the constraint or restore a database backup over new orders.
Once receipts exist, migration down refuses to discard them. Read-only access
and unrelated site operations do not require removing the charge evidence.
