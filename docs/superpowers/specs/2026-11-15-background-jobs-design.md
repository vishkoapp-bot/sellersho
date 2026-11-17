# JOB-01 — bounded background work

Implement the supplied JOB-01 contract under standing authorization to test,
commit, push and deploy each coherent stage. Base/production is
aeed326559b27ab4553fc851f9ea3332ec4aacbb. All migrations through006 are sealed;
new schema uses007. Preserve business rules, credentials, backups and primary
services; exclude docs/fa and paused networkreward. Capacity certification stays
open on production-equivalent hardware; local measurements are local evidence.

## Approach

Builder choice: fixed per-job goroutines, bounded nonqueued admission and durable
PostgreSQL leases, keeping the existing Go/PG/Redis monolith. A shared serial
ticker starves expiry; a detached unbounded queue cannot stop or recover safely.
Start/Stop are synchronized/idempotent, propagate cancellation, and drain with a
WaitGroup and a default5s stop budget. Stop-before-start prevents later startup.
Setters remain startup wiring; snapshot dependencies before starting goroutines.
OPS-01 owns process-wide signal/drain integration; JOB-01 supplies and tests the
cooperative component and fixes manual-job request cancellation propagation.

| Job | Period | Timeout | Batch/ownership |
| --- | --- | --- | --- |
| unpaid-order-expiry | 5s | 3s | up to200 IDs, one guarded order transaction at a time |
| ad cleanup | 10s | 2s | up to2000 expired rows/table, oldest first, SKIP LOCKED |
| QC escalation | 30s | 2s | up to100 overdue un-escalated rows |
| supplier SLA | 30s | 3s | up to100 suppliers, oldest assessment first; preserve existing advisory lock and grant policy |
| delivery/profit release | 60s | 10s | bounded100/order pass, individual transactions; bounded drain within budget |
| monthly settlement | 60s | 4s | existing FIN-00225-account durable batch |

Expiry has a reserved concurrency lane1; other jobs share lane2. No tick backlog
or simultaneous execution of one job in a process. Busy slots retry with bounded
1–5s jitter; errors back off2–60s. A job-specific durable lease uses a fresh owner
token per attempt and clock_timestamp expiry at timeout+2s. Acquire/release are
bounded, owner-CAS prevents an old completion releasing a new owner. A crashed
process relinquishes business transactions via PG rollback; leases expire and
existing business identities preserve exactly-once money/stock effects. Avoid a
pinned SQL connection across an entire job/provider call. Record/cleanup is part
of the slot budget, so normally at most3 job SQL connections/process are active.

## Business work and expiry contract

Preserve existing payment_expires_at (15-minute default),20-day auto-confirm,
configured return window, fee-free monthly reservation and supplier-SLA formulas.
Revalidate eligibility and accepted amounts after existing CORE account/order
guards. Every list and mutation is bounded; one poison row must yield through
persisted retry metadata rather than block a first page indefinitely. Existing
unpaid expiry already has per-order guards and rejected-candidate retry state:
retain it, add per-candidate deadlines/oldest-deadline ordering as needed, and
preserve committed results even if a later candidate fails.

Release ruling after a real contention regression: an ordinary expiry candidate
whose deadline/cancellation interrupts its transaction retries automatically after
five seconds in the additive007 operational retry table. Capture its context error
before cancellation; bare sql.ErrTxDone or a permanent financial/principal error
alone does not justify this classification. Genuine reconciliation failures retain
their existing five-minute review delay. Automatic retries remain in ready backlog
and oldest-age measurements; their separate count is a subset, not additional
orders. Disabled monthly scheduling does not suppress unresolved review alerts.

Expiry/release SLO is5minutes AFTER the recorded payment deadline, not5minutes
after order creation. Current payment initiation renews Redis holds for15minutes
even near that deadline. Clip hold TTL to the known remaining authoritative
window; do not mint a new window for unknown legacy deadlines. New reservations
then cease authorizing cached availability at the deadline even after a process
crash between PG cancellation and best-effort Redis cleanup. Preserve key names
and idempotent Lua release. Legacy pre-cutover holds have at most15minutes TTL;
disclose the cutover window rather than flushing or inventing old reservations.
Inspect the call sites/tests before changing reserveOrderInventory; preserve
valid payment replay and late verified-payment refund obligations.

Ruling for NULL legacy payment deadlines: already-paid transaction replay stays
available. Unpaid NULL-deadline initiation, including an existing unpaid token,
cannot create/renew holds or initiate a new charge: return a truthful409
`payment_deadline_review_required` before side effects. Do not backfill a deadline
or delete an existing hold. Existing callbacks/verified money and compensation
remain governed by FIN-003; do not silently reject an already captured fact.
This deliberately fails closed on missing authority rather than inventing a new
15-minute window; record and test the compatibility change for legacy accounts.

The existing search rollup method returns read-only aggregates and the scheduled
method is a no-op with no established write destination. Do not invent a retention
or aggregation policy: keep the compatibility no-op, remove its misleading timer
and expected-success alert, document it as unscheduled.

## Outcomes and admin visibility

Remove runtime job_runs DDL (the table already has a migration). Migration007
adds lease/state/outcome storage and necessary queue/cleanup indexes; old history
is preserved. Record outcomes with an independent bounded1s metadata deadline
after the business callback returns, keeping correlation but never detaching money
work. Record failures visibly rather than swallowing inserts. Stable run IDs make
metadata retries idempotent; release leases with owner CAS. Fixed safe error codes,
no credentials/provider payloads. Maintain legacy delivery job_runs and operation
run shapes for existing readers; new counters may explicitly start at cutover
rather than claim reconstructed lifetime history. No-work history may be sampled,
but last-success/state is updated on every actual run; disabled monthly settings
are informational, not a missing-success alarm.

Expose running/last-success/stale/failure, bounded ready/review backlog and oldest
eligible delay inside the existing admin operations panel (user's chosen alert
destination). Clearly distinguish ready work from financial review blockers; never
hide poison records to claim the SLO. Use existing UI styles and Persian job names,
not leases/SQL internals. Keep authenticated admin access. The existing delivery
run-now endpoint must use request cancellation and shared ownership/admission;
do not allow its callback to continue with context.Background after disconnect.

## Acceptance/release

Meaningful RED tests for double Stop panic, serial slow-job expiry starvation,
expired-context outcome loss and late-init hold extending beyond payment deadline.
Actual disposable PG/Redis tests: backlog>200 and a sustained4 eligible orders/s
(conservative upper bound of the supplied4-create-RPS workload); backlog drains
after burst and oldest ready delay stays below5min. Force slow/failing settlement
while expiry progresses. Two workers plus interrupted/restarted ownership produce
no duplicate ledger/refund/stock effects or stuck ownership. Exercise actual
mid-batch cancellation, bounded shutdown and failed outcome recording. Report local
DB usage and interactive HTTP latency while all jobs overlap; no hardware claim.
Rehearse additive migration/old-code compatibility, preserve financial snapshots.

Fresh builder and whole-change reviews; frozen allowed-source focused/full Go,
frontend/build/vet/Linux gates. Commit explicit allowlist, push, deploy.ps1
using its full local-build mode and independently verify new007/sealed old checksums, restricted
backup, source marker, history/money conservation and health. Source changes to
bank/payment-window business policy or unrelated services are outside this task.

Release ruling: server disk headroom is limited. Build both committed applications
locally through deploy.ps1 rather than recreating server npm dependencies. Check
the actual release/archive/backup space budget before upload; retain source,
rollback binaries, all backups and runtime services.
