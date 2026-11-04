# Team 09 — database / workers live-parity notes (Phase 2)

Scope: schema and workers only. Pricing, order, settlement, catalog
and API-handler logic belongs to other teams and is deliberately
not implemented here. All references are to MASTER-AUDIT unless
stated otherwise.

## D66 — migration chain (142 -> 149 .up.sql files, NOT 160 records)

- Source at 16388db has 142 `.up.sql` files. This change adds 7:
  6 live-only placeholders (see below) + 1 `ad_event_rejections`
  reconstruction. A fresh database therefore records 149 versions.
- Live has 160 `schema_migrations` records. The gap is NOT an error
  to close by invention:
  - 148 of the 149 fresh records match live records exactly (142
    source files + the 6 live-only names, recorded by the runner
    under their exact `.up.sql` filenames).
  - The remaining live records are 12 short-name duplicates
    (`202610190001`, `202610190002`, `202610190003`, `202610190004`,
    `202610190005`, `202610190006`, `202610190007_cms_improvements`,
    `202610190009_pricing_engine`,
    `202610190010_store_template_electronic`,
    `202610190011_store_template_fashion`, `202610190012`,
    `202610200001`) — the same migrations registered a second time
    without the `.up.sql` suffix, attributed in the audit to an
    older runner. They are a historical double-registration artifact
    (MASTER-AUDIT U76: unproven mechanism) and are NOT reproducible
    by the current runner, which records `filepath.Base` of the
    file it actually applied. Adding fake short-named files, or
    hand-inserting short-name rows, would corrupt the chain and is
    forbidden here.
  - Net arithmetic: 149 fresh = 160 live - 12 duplicates + 1 new
    `ad_event_rejections` record (live has the table but no
    migration record for it).
- The migration runner in `backend/cmd/server/main.go`
  (`runAutoMigrations`) is UNCHANGED: it records `filepath.Base`,
  and it fail-closes with `log.Fatalf` on a re-run of a
  non-idempotent migration. No existing migration file was renamed.
- Numbering gaps that exist nowhere were NOT filled:
  `202610310012`, `202610310013`, `202611020001`, `202610160015`.
- Branch migrations were NOT added (not live):
  `202611030001_remove_pricing_tiers`,
  `202611030002_seller_discount_codes`. Do not merge or cherry-pick
  `feature/monthly-settlement`, `feature/remove-tiered-pricing` or
  `feature/dropshipper-discounts` into this work.

## D66 (placeholders) — the 6 missing files are documented no-ops

`202610160011_store_metrics`, `202610160012_finance_metrics`,
`202610160013_support_metrics`, `202610160014_growth_metrics`,
`202610160016_overview_metrics`, `202610180003_staged_profit_release`
each contain only a comment block plus `DO $$ BEGIN NULL; END $$;`.
Their true contents are UNVERIFIED (U71); live has no metrics table
at all, so inventing table definitions would create schema live
never had. Replace a placeholder ONLY with the recovered file from
the 2026-11-02 merged tree, verified against a disposable database.

## D67 — ad_event_rejections

Added as `202611020004_ad_event_rejections.up.sql` with exactly the
audited columns: `id`, `campaign_id`, `event_type`, `token_prefix`,
`reason`, `ip_address`, `occurred_at`. Idempotent (`IF NOT EXISTS`).
Column types and the filename are reconstruction choices, not
live-verified facts; origin is TODO(U72). No FK on `campaign_id`
(see the migration comment).

## D68 — backup demo tables (DATA ONLY, pending user decision)

`backup_demo_gallery_20261020` (400 rows) and
`backup_demo_products_img_20261020` (100 rows) exist in live as
data, not schema. They are NOT added to the migration chain and
nothing was deleted. Keep or drop them only on an explicit user
decision; this change takes neither action.

## D34 (worker part) — staged profit release

See `backend/internal/worker/scheduler.go`. 60-second tick,
startup run, live startup message, two stages, `job_runs` row per
cycle with `released` / `confirmed`. Ledger pair, return-window
writer and NULL-window rule are out of this worker's scope
(TODO U19/U26 in the code). There is NO automatic monthly
settlement worker in live and none is created here: no
`worker/settlement.go`, no settlement tick. Settlement tables
already exist via migration `202611020002`; they stay untouched.

## D43 (schema/worker part) — logged search

Schema already present: `search_queries` (`202610220003`) plus
`catalog_result_count` (`202610310009`). The log writer
(`logSearchEvent` etc.) and the search-insight endpoints belong to
the catalog/API teams. The worker part is the hourly rollup tick in
`scheduler.go`, which is a documented no-op pending TODO(U73) —
see below. No search table or column was added or changed here.

## D51 (schema/worker part) — view recording

Schema already present: `store_view_daily`, `product_view_daily`,
`view_dedupe_daily` (`202610230002`) with the dedupe rule in that
migration's comment (one daily token counts at most one product
view per product and one store view per store per Tehran day).
The recorder handler (`POST /api/v1/views/events`) and its
dedupe-cleanup wiring belong to the API team; no recorder code was
added here, and no dedupe cadence was invented.

## D73 — Redis key patterns (UNCHANGED, no Flush)

Verified in source, unchanged by this change, and no `Flush`
exists anywhere in the diff:

- `otp:<mobile>`, `otp:cooldown:<mobile>`,
  `otp:attempts:<mobile>` — `backend/internal/auth/otp.go`
  (4-digit code, 120s TTL; cooldown 30s; 5 attempts).
- `catalog:categories:all`, `catalog:niches:<slug>` —
  `backend/cmd/server/main.go` (categories / niches endpoints).
- `jwt:revoked:*` — NO producer found in source Go. The 8 live
  keys are therefore still unexplained here; revocation
  semantics belong to the identity team (MASTER-AUDIT U42/U52
  area). No revocation mechanism was invented.
- `sms:bulk:*` — a producer DOES exist in source, contrary to the
  Phase-1 note that it was untraced (U58): after a successful SMS
  send, `otp.go` stores the provider message id under
  `sms:bulk:<provider-id>` with a 30-day TTL. The pattern is kept
  exactly as-is; this note only records that the producer was
  found in source, it does not change any key.
- Absence of `otp:*` keys at a snapshot is normal (120s TTL) and
  is not treated as an OTP failure.
