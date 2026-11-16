# FIN-004 parcel charge identity and corrections

The supplied backlog is the binding specification. This design applies FIN-004
after FIN-001 and deployed OPS-01 (7ee671b), without waiting for PSP credentials.
The owner has authorized implementation, tests, commit, push and deploy.ps1 for
each stage. FIN-010 remains open pending approved sandbox access.

## Intent and approach

A parcel's original carrier bill is one business operation, irrespective of
client retry keys. The current writer partially recognizes an earlier debit but
does not validate parcelStatus or affected rows, and receiver-pays has no durable
acknowledgment. Cancelled or handed-over uncharged parcels can therefore acquire
a debit. Sparse event_identity does not constrain legacy NULL identities.

Retain the Go/PostgreSQL monolith, typed supplier guard and ordered return/parcel
locks. Replacing these with a separate payment service is unnecessary; key-only
deduplication cannot identify a repeated business operation. Add an immutable
confirmation receipt per parcel and explicit optimistic bill adjustments. The
controller chooses these mechanics within the supplied execution scope.

## Confirmation contract

Keep ConfirmReturnParcelCharge(ctx, returnID, parcelID, actor, key) error and the
existing authenticated warehouse HTTP route. Require actor and a trimmed key of
1..128 bytes. Discover supplier, lock typed account, then canonical work records;
revalidate supplier and parent relation under locks. No provider call.

Only draft parcels may first confirm. A prior recorded confirmation is replayable
without creating any effect, including after handover, if the currently recorded
bill/method matches the effective confirmed bill. A confirmed parcel without
trustworthy evidence fails reconciliation; cancelled/handed-over/in-transit/
received parcels without a receipt cannot first charge. Return lifecycle must
not be cancelled/rejected. A reused key on a different parcel conflicts.

Sender-pays needs a positive integer toman bill <= pricing.MaxTransportMoney;
receiver-pays permits NULL/zero or a nonnegative bounded bill but creates no
supplier debit. Bind receipt to immutable supplier, return, method, accepted bill,
source and reference; confirmation retries with changed accepted fields conflict.
Snapshot actor/time for audit, not fingerprint identity. Exactly one original
debit event and ledger leg for sender-pays, zero for receiver-pays. Verify the
single draft->confirmed UPDATE affected one row. Receipt, ledger, event and state
commit atomically. Return existing acknowledgment on retry, never overwrite it.

## Corrections

Add CorrectReturnParcelCharge(ctx, returnID, parcelID, actor, key, input)
returning an immutable adjustment receipt. Input: expectedAmount, finalAmount,
source, reference, reason. Integers are toman in 0..MaxTransportMoney; nonempty
reason/reference and source tariff/api/manual. Only sender-paid confirmed bills
are correctable, including after handover/receipt; cancelled parcels are held for
review. Receiver-pays cannot produce a supplier correction debit. No change to
supplier or payment method.

Under the same guard prefix, replay matching key before comparing current bill;
changed key payload conflicts. expectedAmount must equal current effective bill,
preventing concurrent corrections from silently overwriting each other. Require
a real change. Positive delta posts one debit event/ledger leg; negative delta
posts reversal event/credit ledger leg, using the existing signed supplier balance
projection. Update the effective parcel bill and append immutable adjustment
metadata in the same transaction. Never mutate/delete the original event. A
repeat confirm cannot post another debit after a correction.

Expose POST /api/v1/warehouse/returns/{return}/parcels/{parcel}/correct-charge
under the same warehouse role middleware, actor claims and idempotency header.
HTTP400 malformed/missing key;409 conflicting/state/reconciliation;503 retryable
lock saturation;200 stored result. No new UI is required for this operations API.

## Additive migration and historical evidence

Add 202611160001_parcel_charge_identity, leaving every applied migration intact.
Receipt and adjustment metadata (one append-only relation with operation kind
or separate relations) are subordinate metadata under existing guards,
not new independent row-lock classes. Add event component/original-charge
identity so only one original debit per parcel is database-enforced; correction
debits are explicitly distinguished. Append-only evidence must survive accidental
UPDATE/DELETE; migration down refuses after evidence exists.

Before uniqueness/backfill, detect ambiguous history: multiple original debit
rows, mismatched supplier/return/amount/method, missing/mismatched ledger leg or
unsupported magnitudes. Abort transaction with a safe reconciliation-required
message, no deletion/guessing/repricing. A single fully matched sender event may
be mapped to its original receipt, preserving all existing IDs, keys and dates.
Legacy receiver confirmations with no receipt/effect cannot prove accepted terms;
leave held for review. Existing distinct carrier charges need reviewed component
mapping and compensating reversals before migration, never auto-deduplication.
Provide a read-only SQL audit used before production release. If live audit has
ambiguities, stop this rollout and continue independent work rather than guess.

## Verification and boundaries

Real disposable migrated PostgreSQL tests reproduce the existing invalid-state
debit before the fix; cover 120000 once under same/new keys and concurrent calls,
receiver zero debit, changed bill/reference/method and cross-parcel key conflicts,
state matrix, account/parent revalidation, concurrent correction/confirm, multiple
corrections, retry and stale expectedAmount. Trigger-injected failures between
ledger/event/state/receipt leave all unchanged. Assert canonical supplier balance,
event/ledger sums and immutable original evidence, not just row counts.

Migration rehearsals cover clean install, fully matched old row, ambiguity with
no partial DDL/data commit, and down refusal. Existing return/inventory/finance
regressions remain green. Frozen tracked-source full backend validation excludes
docs/fa and paused networkreward. Reuse frontend evidence only with exact input
identity; build both apps for release. Production: backup, deploy.ps1, migration
checksum/health/jobs/financial/history checks. No real PSP, live financial fixture,
data deletion, unrelated service changes or capacity certification.

## Builder-check rulings

Historical matching money alone cannot prove that the old invalid-state writer
was legitimately invoked. Automatic mapping additionally requires parcel status
confirmed, nonempty confirmed_by, parent packed/approved, and full event/ledger
binding. Advanced/cancelled/draft histories with debit evidence block migration
for reviewed provenance instead of being legitimized. Production's initial
read-only audit has zero parcels/events; no historical repair is presumed.

An exact stored confirmation/correction retry is read-only recovery and succeeds
after later cancellation/rejection, provided its accepted payload still matches
the receipt/effective bill as applicable. Lifecycle restrictions apply to NEW
financial effects. New corrections are blocked when either parcel or parent is
cancelled/rejected. Returning an old receipt grants no new fulfillment or funds.

## Live-upgrade evidence fence

The production runner migrates while the old backend is still serving. Acquire
the supplier FK DDL reservation first (SHARE ROW EXCLUSIVE, permitting account
FOR UPDATE readers), then ACCESS EXCLUSIVE locks on return requests, parcels,
ledger entries and financial events, before preflight. Keep these locks through
classification and backfill in the runner's existing bounded transaction. This
prevents a committed old-writer event or lifecycle change appearing between the
checked snapshot and receipt creation. A timeout aborts safely; it must not map
unchecked evidence. Deterministic PostgreSQL barriers cover all three old-writer,
parent-cancellation and parcel-handover races, discovery-reader progress, and
queued lifecycle writes after preflight until backfill commits.

The full validation gate also exposed a pre-existing crash/restart fixture defect:
its designated barrier candidates omitted the actual retry eligibility filters.
Repair the test's selection and observable backend teardown, preserving every
conservation assertion and existing deadline. Recognize a deadline-associated
driver bad-connection leaf only with an actual root deadline and budget evidence;
metadata or unrelated joined errors remain failures. This changes test machinery
only, not production jobs or their budgets. Preserve original failing receipts.
