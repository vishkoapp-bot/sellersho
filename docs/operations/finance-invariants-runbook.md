# Finance invariants runbook (FIN-013)

One entry point for every finance signal the platform emits. If an
alert fires, start here — do not improvise a compensating entry.

**Money unit:** Toman everywhere (int64). No Rial figure is ever
entered, compared, or quoted here. A figure that looks 10× too big
is a Rial/Toman mix-up until proven otherwise (FIN-012).

**Delivery honesty:** the Prometheus rules in
`ops/observability/alerts.yml` are visualization-only — there is no
Alertmanager and no Grafana contact point. The ONLY operator delivery
path is the in-app admin-panel notification inbox
(`PollDependencies` → `DeliverAlerts` → `notification_outbox`).
Every alert below lists its inbox condition code; the matching
Prometheus rule exists only so the same signal is visible on the
dashboard. Do not describe a Prometheus rule as "alerting the
operator" — it does not, on its own.

**Who decides:** the finance-domain reviewer (currently the owner +
Haji-Bashi) decides every compensating entry, refund, and release.
The operator on call triages, gathers the evidence in §1, and
escalates. Nobody writes a manual ledger entry, releases held profit,
or marks an obligation resolved without that decision recorded.

## 1. Signal map — what fires, where it comes from, what it means

| Inbox alert (condition) | Prometheus rule | Source metric | Fires when |
|---|---|---|---|
| `finance_invariant` | `SellershoFinanceInvariant` / `SellershoFinanceBalanceMismatch` | `sellersho_finance_invariant_failures_total{check}` (new failures in the poll window) and `sellersho_finance_balance_mismatch_count` (live gauge) | Any new conservation violation, or any live balance mismatch > 0 |
| `finance_unknown_stale` | `SellershoFinanceUnknownStale` | `sellersho_finance_oldest_unknown_refund_seconds` / `sellersho_finance_oldest_unknown_payment_seconds` | Oldest unknown payment or refund older than **1 hour** (proposal — the finance reviewer confirms the bound; see §6) |
| `job_stale` (existing) | `SellershoExpiryStalled` (pattern) | `sellersho_worker_refund_reconciliation_*` gauges | The `refund-reconciliation` job has no successful run within its threshold — the unknown-outcome queue is not draining |
| `slo_breach` (existing) | `SellershoLatencySLOBreachMutation` | mutation p95 > 750 ms / p99 > 2,000 ms | Finance mutations are slow (see §5) |

Finance counters, all bounded labels, no account/order/user ID and
no payment field (token, SHABA, reference, amount) ever in a series:

- `sellersho_finance_invariant_failures_total{check}` — checks:
  `balance_conservation`, `cumulative_refund_le_captured`,
  `reservation_le_availability`, `sum_parts_eq_parent`,
  `money_validation` (a money value outside the FIN-012 bound; the
  only check with a production emitter today, in the treasury
  handler — the others are recorded by the reconciliation oracle,
  §4, and by future invariant call sites via
  `RecordFinanceInvariantFailure`).
- `sellersho_finance_reservation_rejected_total{reason}` — reasons:
  `exceeds_availability` (payout above signed availability),
  `duplicate_cycle` (second reservation for a period/store),
  `coupon_exhausted` (last-use race loser). **Rejections are the
  system working, not a bug** — see §3. Do not page on a single
  rejection; the alert fires on invariant failures, not on these.
- `sellersho_finance_reconciliation_differences_total{kind}` —
  kinds are the §6.5 oracle check names (`orders_payments`,
  `payments_ledger`, `cancellations_obligations`,
  `obligations_outcomes`, `settlement`, `coupons`, `referral`,
  `storage`, `treasury`, `statements`). Recorded by whoever runs
  the oracle (`RecordFinanceReconciliationDiff`).
- `sellersho_job_retries_total{job}` — bounded retries per job,
  including `refund-reconciliation`. Retries are counted, never
  hidden; a retry that succeeds is not an incident.
- Per-query finance latency is the `sellersho_operation_duration_seconds`
  histogram with operations `treasury_cash_position`,
  `store_balance`, `settlement_candidates`, `refund_work_queue`
  (audit §2.6 queries) — histograms, not per-query log lines (see
  the log-disk note in §6).
- Deadlocks: `sellersho_pg_deadlocks_total` (from
  `pg_stat_database`). A deadlock is retried with backoff, bounded,
  never silent — if the gauge moves during a load run, record the
  delta and the retried operations; a sustained rise is a §5 case.
- Lock / pool wait: `sellersho_finance_lock_wait_seconds` (longest
  current lock wait from `pg_stat_activity`) and
  `sellersho_pg_locked`, plus the `sql.DBStats` pool-wait histogram.
  There is no per-account lock-wait metric by design (account IDs
  are high-cardinality labels and are never emitted).

## 2. `finance_invariant` — a conservation violation or balance mismatch

**Symptom:** inbox `نقض پایستگی مالی یا عدم تطابق تراز`, or the
mismatch gauge > 0, or the invariant counter increased.

**First queries (read-only, disposable tooling only — never on prod
writes; run against a read replica / the app's own read path):**

```sql
-- live mismatch, same definition as the gauge (dependencies.go
-- pollFinanceGauges): released orders with no release ledger entry,
-- plus paid-cancelled orders with no cancellation obligation.
SELECT
  (SELECT COUNT(*) FROM orders o WHERE o.profit_released
     AND NOT EXISTS (SELECT 1 FROM ledger_entries l
                     WHERE l.related_order_or_dispute = o.id))
  AS released_without_ledger,
  (SELECT COUNT(*) FROM orders o WHERE o.payment_status='paid'
     AND o.status='cancelled'
     AND NOT EXISTS (SELECT 1 FROM order_refund_obligations r
                     WHERE r.order_id=o.id AND r.purpose='cancellation'))
  AS paid_cancelled_without_obligation;
```

**Triage:**

1. Identify the check: `check` label on the counter, or which of
   the two mismatch counts is nonzero.
2. A merely-paid order has NO ledger entry **by design** — the first
   order-linked entry is written at staged profit release (return
   window close, ≥ 7 days after delivery), never at payment
   (`MarkPaymentPaid` writes none). Do NOT "fix" paid orders without
   ledger entries: the CAP-01 headroom "anomaly"
   (`paid_orders_without_ledger_entry = 6`) was exactly this — six
   unreleased paid orders, not a discrepancy. Only a **released**
   order without its release entry is a mismatch.
3. If a real mismatch exists: freeze the affected account's payouts
   (admin hold — do not invent a new mechanism; use the existing
   payout-review path), preserve the evidence (order id, amounts,
   the oracle output in §4), and escalate to the finance-domain
   reviewer with the numbers. **Never** post a manual ledger entry,
   edit an obligation, or release held profit to "make it balance".
   A compensating entry is a reviewer decision, recorded with its
   reason, and it is itself a ledger event — never an UPDATE.
4. If the counter moved but the gauge is 0 and the oracle (§4) is
   clean, the failure was transient (e.g. a `money_validation`
   rejection of an out-of-range value at the treasury boundary):
   record it in the incident note with the check name and close it
   as rejected-input, not as a conservation break.

## 3. Rejected reservations — expected vs bug

`exceeds_availability`: a payout above signed availability. Expected
when a seller requests more than the locked signed balance (refund
debt blocks withdrawal, FIN-001). Investigate only if the rate
spikes without a matching rise in payout requests, or if a
legitimate request is rejected (then check the balance definition,
not the guard).

`duplicate_cycle`: a second monthly-settlement reservation for the
same period/store. Expected under two-process execution (the second
process loses the advisory lock / uniqueness race). Investigate only
if a cycle ends with an account missing (settlement oracle check 5,
§4).

`coupon_exhausted`: a last-use race loser (100 concurrent attempts
on `maxUses=1` → 1 accepted, 99 rejected). Expected by design.
Investigate only if an accepted attempt exists without a discounted
order, or a rejected attempt was silently undiscounted instead of
receiving a clear `coupon_exhausted` response.

## 4. Reconciliation — the 10-point oracle

Run `scripts/fin013-oracle.sql` against the disposable DB **before
teardown** after any load run (and after any incident in §2). All
ten checks must report zero difference. The script is the runnable
form of audit §6.5; each check names its kind label for the
differences counter.

1. `orders_payments` — every paid order inside the payment model (at least one payment row or verification): exactly one active `payment_transactions` row; `payable_amount` equals the token/callback amount; and every paid order whose payment went through verification has exactly one `order_payment_receipts` row (the receipt is written by `RecordPaymentVerification`, not by `MarkPaymentPaid` — a direct `MarkPaymentPaid` without verification legitimately has no receipt). The migrated schema carries one legacy seed order (`ord-ready-pick-001`, migration 000008) with `payment_status='paid'` and no payment record at all — it is reported separately by the script's check 1b, not counted as a difference.
2. `payments_ledger` — every **released** paid order has ledger
   entries summing to its snapshot. Unreleased paid orders have no
   entry by design (§2.2) — the oracle counts only released ones.
   Deferred releases are expected inside a run window: a released
   order missing its entry is a difference; an unreleased order is
   not.
3. `cancellations_obligations` — every paid-cancelled order: exactly
   one `order_refund_obligations` row (`purpose=cancellation`), zero
   duplicates, zero missing.
4. `obligations_outcomes` — every obligation has a terminal state or
   is listed in the unknown work queue with an age; cumulative
   refunds per order ≤ captured amount.
5. `settlement` — every monthly-settlement cycle: at most one
   reservation per period/store; cycle totals equal the sum of
   account reservations; no cycle marked complete while accounts
   remain.
6. `coupons` — reservation count == `used_count` == discounted
   orders per coupon.
7. `referral` — the known asymmetry case (dispute → wallet → dispute
   return path; 2,500 Toman/case undercount in the dispute-then-
   wallet order; self-heals on the later dispute leg; the release
   gate catches it pre-release). The oracle asserts the release gate
   flags it (`referral_reversal_incomplete` / `needs_review`) and
   records whether the FIN-011 follow-up fix has landed. **Status
   at FIN-013 close-out: NOT landed** — the reverse-order case is
   still pinned at 5,000 in
   `TestFIN009AuditMixedDisputeThenWalletCountsOnce`; do not
   re-litigate the policy here, do assert the numbers.
8. `storage` — per-lot chargeable quantity-day integral equals the
   sum of fee events.
9. `treasury` — cash position recomputed from the event tables equals
   the reported position; negative recovery debt never inflates
   spendable cash (FIN-008).
10. `statements` — opening + posted credits − posted debits =
    closing; closing − active reservations = signed available, for a
    sample of stores including one with a cross-month reversal.

A nonzero difference is recorded with
`RecordFinanceReconciliationDiff(kind)` and triaged as §2 — it is
an invariant failure, not a rounding note. "Zero difference" means
exactly zero; there is no tolerance.

## 5. Lock / pool / latency pressure during a run

- `finance_lock_wait_seconds` sustained above the Q2 bound (see §6)
  or `pg_locked` > 0 across accounts: check `pg_stat_activity` for
  the blocking query, note the operation label (`monthly-settlement`
  vs foreground debit), and record whether foreground-debit p95
  stayed within the Q5 budget (mutation p95 ≤ 750 ms during a
  25-account sweep — proposed, to be confirmed by measurement).
  Never kill a finance transaction to clear a lock without the
  reviewer: a killed settlement batch must resume from its durable
  progress, and a killed refund leaves the obligation `unknown`
  (which is safe, and visible in the queue).
- Pool use: max open 25 (PERF-03; env-overridable). Pool wait
  rising with in-use at the cap means the run is pool-bound, not
  app-bound — record it as such; raising the cap is a PERF-03
  decision, not an incident fix.
- Deadlock delta > 0: confirm each deadlocked operation retried and
  converged (obligation/settlement oracles, §4). A deadlock that
  produced a duplicate effect is a §2 case.

## 6. Unknown outcomes — the work queue and the reconciler

**Visibility:** `GET /api/v1/finance/refunds/unknown-queue?limit=25`
(finance roles) lists the oldest unknown-outcome refund obligations
first with age in seconds. The treasury report exposes unresolved
provider cash as a liability in its totals; the queue is the
actionable surface.

**Automatic reconciler:** the `refund-reconciliation` job runs every
60 s (10 s timeout, bounded batch of 25) and calls
`ReconcileOrderRefunds` with the wired PSP refund adapter. Until
FIN-010 supplies that adapter the provider is nil and each tick is
a recorded no-op: the queue stays visible through its gauges and
the admin endpoint, and execution activates the moment FIN-010
wires the adapter (`BackgroundWorker.SetRefundProvider`). A tick
with a nil provider still records last-success, so a stalled
adapter is visible as a growing backlog, not as a failing job.

**Manual reconcile trigger:** `POST /api/v1/finance/refunds/reconcile`
(finance roles) runs one bounded pass (25 obligations). Until
FIN-010 wires an adapter it answers **503
`refund_provider_unavailable`** with an explicit message instead of
a 200 that reconciled nothing. When the adapter lands, the handler
must pass it to `ReconcileOrderRefunds` the same way the worker's
`SetRefundProvider` does. **Never** mark an obligation resolved by
hand, and never guess an outcome: an unknown outcome stays
`unknown` until the provider confirms or denies it.

**`finance_unknown_stale` triage (oldest > 1 h):**

1. List the queue; note the oldest item's age, order, amount,
   purpose, and latest-event state.
2. If the reconciler job is stale (`job_stale` also firing), fix the
   job first — the queue cannot drain without it.
3. If the job is running and the provider is nil (FIN-010 blocked),
   the item waits for the adapter: record it, do not escalate as an
   incident — but do escalate if the queue grows without bound or
   the oldest age passes the reviewer's bound.
4. If the provider answered ambiguously (timeout / unknown result),
   the outcome stays unknown by design; the retry is bounded and
   counted. Escalate only if the same obligation fails repeatedly
   past the reviewer's retry bound.

**Open bounds (proposals for the finance reviewer — not decisions):**
the 1-hour unknown-staleness bound (§1); the Q2 numeric bounds for
lock wait / queue age / pool use; the Q5 foreground-debit budget
during a sweep. Record the reviewer's confirmed numbers here when
they land; until then the proposals are the working values and are
labelled as such in every report.

**Log-disk note:** per-query SQL success lines are the load run's
disk risk (~15 lines/request → ~24 GB/day at 100 RPS; REL-02). The
§1 histograms replace per-query logs as the debugging surface for
finance queries — do not re-enable per-query success logging to
investigate a finance alert. Error logs stay on.

## 7. Escalation

| Situation | Who | How fast |
|---|---|---|
| Live mismatch > 0, or any §4 oracle difference | Finance-domain reviewer | Immediately; freeze affected payouts first |
| Unknown outcome > 1 h with a running reconciler and a live provider | Finance-domain reviewer | Same day |
| Unknown outcome ageing while FIN-010 blocks the adapter | Record; reviewer at the next review | No incident — but track the queue size |
| Rejection-rate spike with no matching request rise | On-call operator investigates; reviewer if a legitimate request was rejected | Same day |
| Deadlock / lock-wait / pool pressure with no money effect | Record with the run evidence; PERF-03 owner if the cap needs changing | With the run report |

When in doubt, escalate with the numbers rather than acting: a
wrong compensating entry is worse than a slow one.
