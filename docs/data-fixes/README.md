# Staging data fixes — M3 + O3 (Team 6 remainder, Fix69 base)

Scope: staging database ONLY (container `drup_staging_phase3_postgres`,
database `staging_db`). No live database was accessed, no schema change,
no migration, no Go/frontend code change, no commit, no deploy, no
preview rebuild. No release sweep was run. No credential was printed;
the staging password was obtained only via server-side substitution
from the staging backend's `DATABASE_URL` environment and never echoed.

Worktree: `/root/drup-beh-remainder`, branch `feature/beh-remainder`,
base Fix69 `1b0a063`. Deliverables are in this directory only.

## M3 — store-scope campaigns with non-empty target_id

Fix69's code normalises `target_id` to `''` for `scope='store'` only on
create/update (`seller_panel_repo.go`: `CreateStoreDiscountCampaign` /
`UpdateStoreDiscountCampaign`). The resolver
(`discount_repo.go` `GetActiveDiscountCampaign`) queries
`store_id=$1 AND scope='store' AND target_id=''`, so pre-existing
store-scope rows stored with a non-empty `target_id` could never apply.

- Table/columns verified first: `seller_discount_campaigns`
  (`id, store_id, name, scope, target_id, mode, value, starts_at,
  ends_at, is_active, created_at, updated_at`); `target_id` defaults to `''`.
- Before dump: `m3-staging-before.csv` — columns `id, store_id, scope,
  target_id, name, created_at` only (no customer data, codes or secrets).
  The dump contains header only: there were no affected rows to dump.
- Staging counts:
  - Total campaigns in staging: **0**
  - Store-scope campaigns: **0**
  - Store-scope with `target_id=''` (resolver-matchable) before: **0**,
    after: **0**
  - Store-scope with `target_id <> ''` (affected) before: **0**,
    rows changed by the guarded UPDATE: **0**, after: **0**
- The guarded staging UPDATE
  (`SET target_id='' WHERE scope='store' AND target_id <> ''`) therefore
  ran as a no-op (`UPDATE 0`). This is a staging-data result, NOT proof
  about live: live must be re-counted before any deploy-time run.

## O3 — delivered_confirmed orders with NULL return_window_ends_at

Fix69 stage 1 stamps `return_window_ends_at = confirmed_at +
return_window_days` (platform setting, COALESCE 7, clamped 1..365 — see
`order_repo.go` `returnWindowDaysFromTx` / `GetReturnWindowDays` and
`worker/scheduler.go` stage 1). Pre-existing confirmed rows lacking the
window can never be released by stage 2, which requires a non-NULL window.

- Table/columns verified first: `orders` (`id, status, confirmed_at,
  created_at, return_window_ends_at`, among others).
- Staging `platform_settings` (id=1): `return_window_days = 7`
  (in the 1..365 clamp, so the fallback was not needed).
- Before dump: `o3-staging-before.csv` — columns `id, status,
  confirmed_at, created_at, return_window_ends_at` only.
  The dump contains header only: there were no NULL-window
  delivered_confirmed rows to dump.
- Staging counts (orders total: 37):
  - `delivered_confirmed` total: **8**
  - NULL-window before: **0**, rows backfilled: **0**, NULL-window after: **0**
  - Backfillable (NULL window AND `confirmed_at IS NOT NULL`): **0**
  - Residuals left untouched (NULL window AND `confirmed_at IS NULL`): **0**
    — there was nothing in this category, and no timestamp was invented
    for any row.
- The guarded staging UPDATE (window = `confirmed_at` + live-read
  `return_window_days`, only where `status='delivered_confirmed'` AND
  window IS NULL AND `confirmed_at IS NOT NULL`) ran as a no-op
  (`UPDATE 0`). Only the window column was in scope; `updated_at` and
  all profit/release columns were not touched, and no release sweep ran.

## Observations (not fixes, left untouched)

- 2 of the 8 staging `delivered_confirmed` orders have
  `confirmed_at IS NULL` but already have a `return_window_ends_at`
  (`ord-f6ab74df-ff3e-47b9-becb-c3c87eb68d1e` and
  `ord-69c42bb9-50c2-4398-8645-bdbecfccc87f`). They are outside O3's
  definition (their window is not NULL), so they were not changed.
  Their recorded window (2026-10-10) also predates their `created_at`
  (2026-10-18), which looks like seed/test data; this is unproven and
  was not investigated further, as it is out of scope.
- Dump-sequence note: the M3 dump was written before any UPDATE. The O3
  dump file was written after the two no-op UPDATEs (an earlier
  write attempt did not complete), but because both UPDATEs affected
  0 rows, the staging data was identical to the pre-UPDATE state, so
  the O3 dump is still an accurate before-state dump (header only).

## Deploy-time proposals for live (NOT run by this worker)

- `m3-live-proposal.sql` — same guarded, idempotent M3 UPDATE, with a
  comment header (what it does, why it is safe, backup-first warning)
  and commented pre/post count checks.
- `o3-live-proposal.sql` — same guarded, idempotent O3 UPDATE, with a
  comment header (what it does, why it is safe, backup-first warning,
  no release sweep, NULL-confirmed rows left untouched) and commented
  pre-count checks including the residual count.
- Both files must be re-counted against live before running, run only
  with a full database backup first, and re-verified after running.
  Staging's zero counts must NOT be assumed for live.

## Files in this directory

- `m3-staging-before.csv` — M3 before-state dump (header only, 0 rows)
- `o3-staging-before.csv` — O3 before-state dump (header only, 0 rows)
- `m3-live-proposal.sql` — deploy-time M3 proposal (not run)
- `o3-live-proposal.sql` — deploy-time O3 proposal (not run)
- `README.md` — this file
