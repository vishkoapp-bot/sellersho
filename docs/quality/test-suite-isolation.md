# Complete integration suite isolation

The shared-database command reproduced all 109 original top-level failures on
2026-11-14. Older fixtures execute destructive DDL in public and leave truncated
table definitions for later tests. Serial package execution (`-p 1`) cannot
isolate tests within a package.

Run `python scripts/test-suite-isolated.py` from the repository root. It compiles
each package once and discovers every top-level Test function, then runs each
against a separate database cloned from the canonical migrated template. Seeder
tests receive the existing schema-only template with triggers preserved. Each
child retains the NOCREATEDB/NOSUPERUSER test role, independently verified marker,
and scrubbed provider environment. Only the container-owning runner creates and
drops clones. Existing schema-scoped fixtures and subtests keep their normal
relationships inside that test's database. Binaries run in their package directory
so relative migration/testdata paths match `go test`.

Use `--run REGEX` and package paths for focused reproduction. Failure names and
the aggregate result are retained; failed tests remain failed. Default helper or
opt-in skips emitted by test binaries remain visible. A full passing result is
not claimed while the complete isolated run and domain triage are outstanding.

Initial verification: the destructive `TestFinanceAdjustmentConcurrentReplay`
and `TestLegacyAliasAdoptionPreservesLedgerRows` both passed against separate
clones with verified container cleanup. The complete migrated suite is running
separately; remaining failures will be fixed without removing their assertions.

## First complete isolated baseline

The completed run discovered 565 top-level tests and reported 72 failures. Of the
109 shared-database failures, 43 no longer failed and 66 still failed. Six further
independent failures became visible after removing order-dependent contamination.
This is a failing baseline, including visible helper/opt-in skips, not 493 passing
integration gates. Local complete output: `logs/test02-per-test-full.log`.

## Supplier registration/access fixtures

Registration has activated supplier accounts immediately since the existing
October policy. Fixtures incorrectly attempted a second pending-only approval;
the email round-trip test also expected `pending_approval`. Fixtures now assert
the returned approved supplier and active owner, use valid numeric mobile numbers
for team members, and consume the migrated schema without rerunning historical
DDL. Seven admin queue/access/ownership/QC metadata/tracking-validation/team tests
passed in separate clones; the registration email/null round-trip passed too.
Remaining publication and inbound lifecycle failures retain their assertions and
are tracked separately. Logs: `logs/test02-supplier-access-green.log` and
`logs/test02-registration-green.log`.

## Return parcel charge SQL

The isolated canonical-schema lifecycle reproduced PostgreSQL 42P08 while
confirming sender-paid parcel charges. One SQL parameter appeared both as the
varchar parcel ID and as a text concatenation operand. Bind the immutable event
identity separately from the parcel-ID parameter. Both real return lifecycles
and the focused finance business-event test now pass (3/3, no skips); verification
also asserts the exact event identity and retries the same parcel under a new
request key while retaining one financial event and one ledger debit. The full
default Go suite passes with integration opt-ins absent. Logs:
`logs/test02-return-charge-final.log`, `logs/test02-stage3-default.log`.

## Public catalog/store fixtures

Store campaign/subscriber fixtures now insert the real owner before creating a
store, preserving the canonical foreign key instead of depending on earlier
tests having dropped it. Category JSON asserts the current eight-field shape,
including the direct active-product count. The obsolete
`TestTeam05PublicStoreListServesSuspended` is replaced by
`TestTeam05PublicStoreListExcludesSuspendedAndPreservesDetail`, matching the
existing FIX 9 rule in `handleStoresList`: active stores appear in the public
directory, while suspended store detail retains its status. It also verifies
the 15 public fields and absence of private margin data. All four isolated tests
passed without skips. No catalog or store business behavior changed.
Evidence: `logs/test02-catalog-fixtures-final.log`.

## Notification writer fixture boundaries

The minimal notification fixture used varchar item IDs but accidentally joined
the migrated public UUID origin table left behind by its DDL. It now explicitly
owns an empty origin relation with matching fixture item-ID types and includes
the shipment delivery timestamp used by cancellation guards. Five failing writer
cases passed, followed by all 16 tests in that fixture without skips. Assertions
still exercise real cancellation, disputes, return COD refusal, customer/supplier
recipients and replay counts. Production notification code was not changed.
Evidence: `logs/test02-notification-fixtures-final.log`.

## Payment fixture physical provenance

Two migrated payment races declared product stock without a physical lot or
immutable checkout split. They now seed a distinct active warehouse, its exact
sellable balance and each order line's matching origin before starting competing
operations. This exercises current capture rules without relaxing reconciliation
refusals. Both isolated tests passed, followed by five repetitions of both tests
in one guarded migrated database (10 successful cases). Assertions still require
two callbacks to observe one paid transaction, one inventory debit, no premature
profit release, and expired payment refusal. Evidence:
`logs/test02-payment-provenance.log`, `logs/test02-payment-repeat.log`.

## Carrier/cancellation fixture coverage

Carrier webhook coverage now owns its matching origin relation and the referrer
field required by financial account discovery. Its actual failed/delivered HTTP
flow passes, retaining repeated-webhook recipient assertions. Cancellation's
private schema likewise supplies current account-discovery fields and owns its
origin table rather than falling back to unrelated public rows. The race test
now explicitly proves each supplier/warehouse dispatch can succeed and then
reject cancellation before running its 200 concurrent cancellation/dispatch
iterations. Warehouse cases seed conserved origin quantities and matching storage
positions. Three cancellation tests and the carrier HTTP test passed separately;
the optional notification writer on the intentionally minimal cancellation
fixture still logs missing payable-amount metadata. Notification acceptance is
provided by the separate 16 writer cases, not these skipped optional writes.
Evidence: `logs/test02-cancellation-fixtures-final.log`,
`logs/test02-carrier-fixture.log`.

A subsequent full working-tree run reported 565 tests / 51 failures while the
payment fixture work was being prepared; it is diagnostic evidence, not a claim
that a pristine committed source passed. The remaining integration baseline is
still failing and required downstream lifecycle skips remain open.

## Admin supplier detail fixture

The supplier-detail fixture now uses UUID variant identities matching the
canonical table left by migrations and records the delivered line's immutable
supplier snapshot. Missing historical authority correctly yielded a null balance
with a gap marker; the old decoded value struct displayed that as zero. All four
supplier-detail tests now pass, retaining the expected earned/reserved/available
amounts and Tapin token privacy assertions. Evidence:
`logs/test02-supplier-detail.log`.

During release work the 25 GB server filesystem reached 93% utilization. Seven
explicit release paths created by this task held 221 MiB each of rebuildable
node_modules. After resolving paths and refusing live backend/frontend mounts,
only those dependency directories were removed. Backups, committed source,
binaries and dist were retained; available disk increased from 1.9 to 3.5 GB and
backend health passed. Dependencies can be recreated from each release lockfile.

## Worker and suspension finance fixtures

The staged delivery/profit worker now runs against canonical migrated orders,
account tables and job history. It seeds valid customers and five complete
orders rather than dropping the orders table and reconstructing an outdated
subset. Confirmation and closed-window release counts, untouched open/null
windows, and one durable job record all passed. The supplier-suspension payout
test records the immutable delivered-line supplier snapshot before requesting
settlement; the approved payout passes, suspension blocks the next payout, and
earned balance remains unchanged. Both tests passed in guarded isolated clones.
Evidence: `logs/test02-worker-canonical.log`, `logs/test02-supplier-freeze.log`.

## Public detail response contract

The SPN HTTP regression now follows the existing explicit FIX 12 contract in
assembleProductDetail: public responses omit dossier specs/gallery while the
repository preserves the stored specs. The test asserts both the omitted public
keys and the exact stored power value rather than deleting its data-preservation
check. It passed alongside both public-response key regressions (3/3, no skips).
Production serialization was not changed. Evidence:
`logs/test02-catalog-public-contract.log`.

## Product publication explicit source

A new canonical-schema regression reproduced both legacy and typed publication
rejecting their supplied, reviewed supplierWarehouseId because initial stock was
seeded before the product retained that ID. First publication now verifies active
ownership under a shared warehouse-row lock and attaches the exact selected ID
inside the publication transaction, before SKU/legacy stock seeding. Existing
product origins are not rewritten by this change. No profile/name inference or
historical stock backfill is introduced.

The real-schema regression passes for both publication models, asserts three
sellable units at exactly the selected warehouse, and rejects inactive, foreign
and unknown sources with product/status rollback checks. Five existing V2 model
tests now supply the same explicit source and clone their owned warehouse table;
all six top-level tests passed without skips. The default Go suite passed with
integration opt-ins absent. Evidence: `logs/test02-publication-origin-red.log`,
`logs/test02-model-publication.log`, `logs/test02-stage11-default.log`.

## Supplier flow source/destination conservation

Supplier flow fixtures now seed explicit owned source warehouses and canonical
sellable balances rather than product projections alone. Granular publication
submits and reviews the source ID. Scheduled inbound tests seed a distinct active
destination and test the current provenance-conflict response for cross-supplier
dispatch. Cancelling a fully dispatched transfer is an idempotent no-op preserving
in_transit; cancelling an undispatched reservation restores its source exactly.
The final assertion requires 18 sellable + 2 transit = 20 physical units. Eight
flow/publication/ownership/permission/tracking tests passed without skips.
Evidence: `logs/test02-flow-provenance.log`.

## Legacy proposal approval fixtures

Product-gap fixtures clone the current field-review/history and supplier warehouse
relations into their private schema, carry origin/schema/nullable legacy SKU
columns, normalize legacy payload aliases and explicitly review every submitted
leaf. Their synthetic source is owned and active. Two legacy approval cases and
the variant-price set/read/clear/ownership case pass (3/3, no skips), retaining
measurements, first-value registry mapping, gallery/default variants and pricing
audit assertions. The review gate was not weakened to accept unreviewed fields.
Evidence: `logs/test02-legacy-approval-fixtures.log`.

## Dedicated and generic approval routes

The legacy approval-route fixture now provides current publication columns,
nullable SKU identity and variant activity, and records an explicit synthetic
source plus reviewed canonical payload leaves. Both route tests passed: the
dedicated approved/rejected flag and the generic inbox still exercise their
actual shared publication pipeline and duplicate-review refusal. The fixture has
no real supplier recipient, so optional approval notification skips remain
visible and are not notification acceptance. Evidence:
`logs/test02-approval-route-fixtures.log` (2/2, no test skips).

## Supplier panel and unpublished typed proposal

Panel fixtures now select an owned origin through the real origin API, explicitly
declare stock before reserving an inbound and use a distinct active destination.
The proposal HTTP case sends the supported V2 simple SKU shape, a registered
niche and its source. It checks that no supplier product exists after submission
or a refused unreviewed approval, then records all field reviews and verifies the
actual publication ID returned by ApproveProposal. The provisional HTTP preview
ID is not treated as a published product identity. Both tests passed without
skips, retaining foreign-owner/negative-stock/price gates and stock-zero checks.
Evidence: `logs/test02-supplier-panel-current.log`.

Full immutable source snapshot 94ef32b421af83ecb418f671769e175ae1267488 completed
563 top-level tests with 34 failures and verified cleanup. The paused untracked
network-reward package was excluded by git archive. This remains a failing
integration baseline; subsequent focused fixes require a new complete run.
Evidence: `logs/test02-exact-94ef32b-full.log`.
