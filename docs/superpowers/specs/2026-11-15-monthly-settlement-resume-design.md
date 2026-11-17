# Monthly settlement concurrency and restart safety — FIN-002

## Intent and existing contract

Execute the supplied FIN-002 backlog under the user's standing authorization to
continue, validate, commit, push and deploy each stage. Preserve integer toman,
fee-free monthly payout requests, configured settlement day/catch-up window,
noncentral-store eligibility, bank-details source and missing-bank skip behavior.
This creates internal reservations, never bank transfers. FIN-014 owns unknown
historical transfers; FIN-013 and CAP-02 retain production-hardware certification.

Both current manual and automatic paths use FIN-001 account guards and recompute
BalanceTx, but duplicate the implementation and lock every candidate account in
one transaction. One slow account can block the entire run and foreground debits.
The original stale-balance race may already be prevented by FIN-001; prove the
2m-not4m invariant rather than reintroducing it to obtain a failing test.

Read-only production audit before implementation found one completed cycle, one
item, no duplicate cycle/store groups and no invalid payout links. The item has
no linked payout. Counts are evidence, not permission to rewrite history.

## Approach and constraints

Builder choice: persist a cycle's candidate identities and process bounded batches
using one short transaction per account. A single giant transaction cannot meet
foreground isolation/restart progress; an in-memory cursor cannot survive restart.
Keep the existing Go/PostgreSQL monolith and no new dependency or queue service.
No product/business policy question remains in the detailed supplied backlog.

New migration006 adds durable per-cycle/store progress and extends cycle status
to distinguish running/blocked/completed. Existing completed cycles remain sealed;
never seed new work or resend/recreate their payouts. Preserve all existing item
rows and links, and existing UNIQUE(cycle_period,store_id). Validate duplicates
and inconsistent links before any new uniqueness/adoption; fail closed with an
actionable reconciliation error instead of deleting/choosing a winner. Applied
migrations, including202611020002 and202611150001–005, are immutable.

## Shared service and progress

The manual handler and scheduled worker call one repository service. Initialization
atomically creates a running cycle and a snapshot of eligible store IDs, without
balances or reservations. A concurrent initializer observes the same cycle; no
check-then-insert race. New stores after initialization belong to a later cycle.
An empty candidate set may complete immediately. Initialization uses a bounded
transaction and no account row locks; a failed initialization leaves no partial
manifest. Existing completed cycles return unchanged.

Each batch considers at most25 pending accounts and has a4s overall call budget
within OPS-02's8s request limit. Each account transaction has a1s deadline,
100ms lock_timeout and750ms statement_timeout (builder defaults, not capacity
claims). Take only that seller's existing typed account guard, then re-read work,
current eligibility, bank details and BalanceTx. No stale candidate amount grants
spending authority. Same-period/store progress already done prevents a new effect
even if its prior payout was rejected or failed later. No provider I/O under locks.

Create the fee-free pending payout and monthly item and mark progress done in the
same transaction. Positive balance without bank details produces the existing
skipped item and finishes that store; nonpositive/ineligible stores finish with
no payout/item. A rolled-back transaction leaves pending work for retry. Account
timeout/lock contention must not starve later accounts in the bounded batch.
Unresolved financial provenance blocks that work without guessing a balance;
other accounts may finish and the cycle remains incomplete/blocked. Persist only
bounded safe reason codes, no credentials/provider payloads.

Use FIN-001/CORE lock order. The account guard serializes work for that store;
read its progress through MVCC before new payout/item writes. Do not acquire a
cycle row lock before an account guard or update cycle counters after taking a
later work-row guard. Finalize/refresh aggregate cycle state in a separate short
transaction with no account locks; only complete when no unfinished work remains.
Aggregate sums use checked monetary handling and accepted item snapshots.
List/detail/run expose truthful completed/pending/blocked progress and existing
totals; a partial batch never claims completed. A completed manual retry retains
the existing409 behavior; running retries resume and return202 with progress.

The worker initializes a newly due cycle even if older work is blocked, and
resumes unfinished cycles outside their original catch-up windows. Select one
cycle fairly by persisted last-attempt time (never strict oldest-period priority),
then one bounded batch per existing60s tick; completed cycles never run again.
Use persisted per-account retry-after ordering so a hot/blocked first page cannot
starve later accounts. A blocked older store cannot stop unrelated later-cycle
accounts; canonical balance guards retain every existing reservation. Keep truthful running/blocked
logging instead of printing completed for partial work. JOB-01 owns broader
worker scheduling/lifecycle changes. No detached continuation after cancellation.

## Verification and release

Use disposable, marker-verified migrated PostgreSQL and synthetic providers only.
Prove the existing boundedness/restart problem RED without changing old code;
retain any already-passing stale-balance regression as a non-defect proof.
Exercise manual+worker, two workers, seller payout/service purchase/early withdrawal
competition (total reservations at most2m from2m), rejected prior payout replay,
and crash/cancel after a committed first account followed by complete resumption.
Use real row locks/barriers to force ordering. Assert cycle/store uniqueness,
balance conservation, durable progress and no premature completed status.

Hold one account while a multi-account sweep runs and show unrelated foreground
debits proceed; measure local foreground latency and report the environment and
limits. Local evidence does not certify FIN-013 hardware SLOs. Rehearse migration
on synthetic legacy completed cycles, valid links and duplicate/inconsistent
fixtures: valid history preserved, unsafe history rejected without writes or
money transfer. New schema is additive and must retain old INSERT compatibility.
Rehearse the actual previous manual/worker paths on running and blocked cycles:
manual409 and automatic return-existing must leave progress/items/payouts/state
unchanged. Compatible rollback may pause unfinished progress while preserving
availability and reservations; document that pause and forward recovery with the
new writer. Do not block automatic rollback without evidence it is unsafe.

Independent design/release review, isolated whole Go and frontend/build gates,
explicit allowlist commit/push and deploy.ps1 follow OPS-02 publication. Preserve
production data/backups/credentials/services and exclude docs/fa and the paused
networkreward directory. Record counts/checksums/health before and after release.

## Implementation rulings from acceptance

Progress has no store FK: initializing all candidates must not key-share-lock all
accounts. The per-account typed guard revalidates existence and eligibility.
Missing configuration retains the old explicit pause; restoring it permits resume.
Older unknown/corrupt progress and unsupported aggregate amounts still yield their
worker turn. Transport-unsafe totals fail closed, preserving exact database values;
new unsupported aggregates block completion for reconciliation. Detail totals and
items share a read-only repeatable-read snapshot. Sharing the projection preserves
all previously visible monthly history, including more than24 completed cycles.
Global additive migration files remain present through code rollback so the old
migration runner can verify the new checksum history. These are builder decisions
supported by concrete regression/compatibility tests, not new payout policies.
