# Background jobs (JOB-01)

Each process starts six independent finite loops. Expiry has its own slot; the
other jobs share two slots. Busy work is retried after 1–5 seconds of jitter,
without a queued tick backlog. Errors back off from 2 to 60 seconds. Each attempt
has a new PostgreSQL owner token and a lease lasting its timeout plus 2 seconds.
Acquire, outcome and release use short statements; a lease never pins a connection
through a business batch. Owner comparisons prevent an old completion or release
from acknowledging a replacement worker's attempt.

| Job | Period | Business timeout | Maximum pass |
| --- | --- | --- | --- |
| Unpaid expiry | 5 seconds | 3 seconds | 200 orders |
| Expired ad data | 10 seconds | 2 seconds | 2,000 rows per table |
| QC escalation | 30 seconds | 2 seconds | 100 cases |
| Supplier dispatch assessment | 30 seconds | 3 seconds | 100 suppliers |
| Delivery confirmation/profit release | 60 seconds | 10 seconds | 100 per stage |
| Monthly settlement | 60 seconds | 4 seconds | Existing 25-account batch |

Expiry, delivery and supplier assessment use individual transactions and 300ms
candidate deadlines under the parent budget. Committed candidates survive a later
failure/cancellation. Ordinary expiry deadline/lock/serialization interruptions
retry automatically after five seconds using persisted operational metadata. They
remain in the ready backlog and have a separate automatic-retry count, rather than
being classified as financial review. Genuine rejected expiry guards retain their
five-minute review delay. Delivery/SLA rejected candidates likewise yield for five
minutes with visible review metadata. Pre-cutover review markers retain their
existing timing; no unknown historical reason is reclassified. Ad/QC selection uses
`SKIP LOCKED`. Supplier assessment retains advisory lock 90261109, the 90-day
sample window, strict score >98%, approved/active conditions and administrative
grant exceptions. Delivery retains 20-day confirmation, the configured return
window, financial guards, ledger/referral effects and the NULL-window review
case. Eligibility and profit are re-read after the existing order/account guards.

Start and Stop are synchronized and idempotent. Stop cancels business contexts and
drains within five seconds; Stop before Start prevents startup. Dependencies are
startup wiring and are snapshotted before loops begin. Process-wide signal drain
integration remains OPS-01. The existing admin delivery run-now action shares the
same slots/ownership, runs synchronously and propagates request cancellation.

Search rollup remains a compatibility no-op and is unscheduled. There is no new
search aggregation destination or retention policy.

## Payment hold deadline

Expiry latency is measured from recorded `payment_expires_at`, with a target
under five minutes after that deadline. New or renewed Redis holds use an
absolute expiry clipped to that deadline and the existing 15-minute maximum.
Existing keys and idempotent release Lua remain compatible. A process crash
after PostgreSQL cancellation cannot extend a new hold beyond the deadline.

An unpaid legacy order without a deadline, including an existing unpaid token,
returns HTTP409 `payment_deadline_review_required` before hold/charge writes.
No deadline is backfilled and no old hold is deleted by that decision. Paid
transaction replay remains available. Verified callbacks, captured money and
refund/compensation obligations retain the FIN-003 rules.

Pre-cutover holds may retain their original TTL for up to 15 minutes. This is a
temporary cutover exception to the five-minute target, not grounds for flushing
inventory keys or inventing historical reservations.

## Admin operations and outcomes

The existing authenticated admin operations panel shows all six expected jobs,
including missing records, running work, last success, stale success, fixed
failure codes, capped ready/review/automatic-retry counts and oldest ready delay.
The automatic-retry count is a subset of the ready count, not additional orders.
Ready age continues to include ordinary automatic retry wait; financial-review
alerts remain active even when monthly scheduling is disabled. Counts are
bounded at 1,001 and shown as 1,000+. A success becomes stale after three periods
plus its timeout. Missing successes always need attention except when monthly
settlement is explicitly unconfigured. A started attempt without a completed
outcome is visible even if its lease has expired or been released.

Outcome writes use an independent one-second metadata deadline after the callback
returns, retaining correlation. Only metadata detaches from cancellation; money
and inventory work never does. Stable run IDs make a bounded retry idempotent.
Storage failures are returned/logged and cannot masquerade as successful runs.
Legacy `job_runs` and `operations_job_runs` shapes/history remain available.
New run/effect counters begin at cutover. Monthly financial totals remain in the
existing settlement report; its cumulative cycle total is never added again on
each scheduler tick. No history retention or deletion is introduced.

Migration007 is additive. Apply it before starting the new application. Migrations
through006 remain sealed. To roll application code back, leave007 installed;
its additional tables/indexes are compatible with old readers/writers. Do not
drop control/outcome history during rollback. Deploy only committed reviewed
source through `deploy.ps1`, excluding `docs/fa/` and paused network rewards.

## Local acceptance evidence

Run from the repository root against the runner's marker-guarded disposable
PostgreSQL/Redis containers; never point these tests at application databases:

```powershell
python scripts/test-suite-isolated.py --run 'TestJobsBurst|TestJobsCrashRestart' ./internal/worker
python scripts/test-suite-isolated.py --run 'TestJobStore|TestJobsBound|TestDeliveryReleaseRevalidates|TestSupplierPoison|TestOutcomeStorageFailure' ./internal/repository
python scripts/test-suite-isolated.py --run 'TestPaymentInitiationHoldEndsAtRecordedDeadline|TestPaymentNullDeadline|TestManualDeliveryJob|TestOperationsHealthIncludes' ./cmd/server
```

The local two-worker test seeds 260 expired orders and sustains four new eligible
orders per second for 30 seconds while a forced slow/failing settlement and other
jobs overlap. One observed run drained all380 orders in31.97s, maximum oldest
ready age138s, with552 actual HTTP catalog requests (p50 3.12ms, p95 5.67ms,
maximum34.72ms), and observed three concurrent database users. A separate test
kills a child process after committed expiry work, interrupts a real SQL batch,
then restarts ownership and verifies preserved progress, unchanged stock and no
duplicate ledger effects. Financial release tests verify one balanced ledger
pair from the amount accepted under the guard. These are local measurements;
production-equivalent capacity certification remains open.

## Operations follow-ups resolved by OPS-01

OPS-01 routes exported delivery/monthly compatibility calls through Runner,
cancels manual pre-Start work on Stop, removes the duplicate delivery settings
pre-read while retaining its guarded authority, and excludes active financial
review from the automatic-retry subset even when both markers overlap. Real
regressions cover each case. Confirmed partial progress also resets scheduler
error backoff to2s; zero-progress, failed metadata and cancellation retain the
original exponential limits. See [Process lifecycle](process-lifecycle.md).
