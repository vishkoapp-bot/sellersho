# Monthly settlement reservations — FIN-002

Monthly settlement creates fee-free internal payout reservations, not bank
transfers. Manual and scheduled triggers use one service and the canonical
[financial balance](financial-balances.md) under the existing typed seller guard.
Candidate amounts never authorize spending. No provider call runs under a lock.

## Durable cycle and account work

Initialization commits a running cycle and a snapshot of eligible noncentral
store IDs in one bounded transaction. It records no candidate amount and takes
no account row locks. The progress table deliberately has no store foreign key:
initialization must not hold key-share locks on every store. A worker takes the
individual account guard and revalidates its current existence, eligibility,
bank details and signed availability before any reservation.

Each account transaction writes its payout, monthly item and done progress
atomically. A positive balance with no bank details retains the existing skipped
item and leaves the balance withdrawable. Nonpositive/ineligible accounts finish
without a money effect. Failure rolls back that account; committed earlier
accounts remain durable. A repeated period/store cannot reserve again even if
its original payout is subsequently rejected or failed. A later period uses the
current canonical balance, including every still-active reservation.

| Bound | Value |
| --- | --- |
| Accounts considered per batch | 25 |
| Whole service call | 4 seconds, clipped by parent context |
| Account transaction | 1 second |
| PostgreSQL lock / statement timeout | 100 / 750 milliseconds |
| Failure metadata update | 250 milliseconds, clipped by parent |
| Temporary / reconciliation retry delay | 5 / 60 seconds |
| Existing scheduler tick | 60 seconds |

Only one account guard is held during its financial transaction. Work records
follow payout/item writes; cycle totals/finalization run separately without
account locks. A completed cycle is terminal, including concurrent finalizers.
Context cancellation never detaches a continuation. Commit uncertainty is
resolved by rereading the same durable period/store work under its account guard.

Per-account retry-after ordering prevents a busy first page from starving later
accounts. Persisted cycle last-attempt ordering prevents an older blocked cycle
from starving a newly due period. Reading invalid/unknown old progress must
still yield the cycle's turn. The worker initializes a newly due period before
fairly selecting unfinished work, and resumes work outside its original catch-up
window. An explicitly unset settlement-day setting preserves the existing pause;
restoring the setting permits unfinished work to resume.

## Progress and reads

Cycle status is running, blocked or completed. Responses expose candidate,
completed, pending and blocked counts plus `progressKnown`. New running totals
come from committed monthly items in one MVCC projection. Detail reads totals
and item rows in one read-only repeatable-read transaction. Sharing the handler
projection preserves all previously visible monthly history.

Manual run returns202 while unfinished,200 when the call completes the cycle,
and the existing409 for an already sealed period. Worker logging distinguishes
partial/blocked work from completion. Safe fixed reason codes carry diagnostics;
they contain no provider payload or secret. Broader worker scheduling, shutdown,
notifications and operational backlog controls remain JOB-01.

Unsupported transport-range totals fail closed instead of being rounded for
JavaScript or rewritten in storage. New unrepresentable aggregate totals leave
the cycle blocked for reconciliation. Existing exact database amounts and every
committed per-account effect remain intact; another period can still take its
fair turn. No historical amount is clamped or guessed.

## Migration and compatible rollback

Migration006 leaves completed history sealed, creates no historical manifest and
validates duplicate period/store effects and payout links before adoption. Missing
uniqueness can be added only after validation. Unsafe legacy data fails migration
atomically and requires attributable reconciliation. Existing accepted item IDs,
payout IDs, amounts and transfer states are preserved. `progressKnown=false`
distinguishes historical candidate counts that cannot be reconstructed.

The compatibility rehearsal compiles the actual previous commit's migration
runner/manual/scheduled code with only migration006 and a synthetic fixture added.
For running and blocked cycles, the old manual path returns409 and the old worker
returns the existing cycle without changing work, items or payouts, including a
processing transfer. Rollback therefore pauses unfinished progress; a forward
release resumes it. Keep migration006 in the global migration directory after
rollback, as `deploy_remote.py` already does: checksum history still requires its
source. Never restore an older database or delete the new progress as a code
rollback shortcut.

## Verification scope

The isolated tests use marker-verified disposable PostgreSQL/Redis and synthetic
accepted financial snapshots. They cover bounded batches, canceled/restarted
work, two workers, actual manual handler plus worker and competing debit paths,
source-level old-writer compatibility and invalid legacy migration fixtures.
A real PostgreSQL trigger pauses a foreground payout after its balance decision;
the sweep must wait on the same account authority, preserving2m instead of4m.
An owned row-lock barrier on the first25 accounts must not starve the next account.

Local foreground samples during a blocked sweep were approximately22–150ms;
these measurements describe this device and fixture, not a production latency
certificate. FIN-013/CAP-02 and production-equivalent1,000-user/100-RPS acceptance
remain open. No bank transfer, PSP charge, real SMS or production load test is
performed by these tests.

Run focused acceptance from the repository root:

```powershell
python scripts/test-suite-isolated.py --run '^TestFIN002' ./internal/repository ./cmd/server
python scripts/test_monthly_legacy_compat.py 8c0c7c6198cec23eb8d269db664779fd527fee61
```
