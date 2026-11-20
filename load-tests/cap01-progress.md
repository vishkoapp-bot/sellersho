# CAP-01 progress record — native 1,000-user / 100-RPS qualification harness

Status: IN PROGRESS (harness build + short baseline suite). CAP-01 stays
OPEN; CAP-02 (intended-hardware certification) stays OPEN. Nothing here
certifies production readiness. `certified:false` on every run by design.

## Coordinator decisions applied (2026-11-20)

- D1 — Native adapter. This VM cannot run Docker (setns blocked) and the
  owner is asleep / laptop Docker unavailable overnight. New runner
  `scripts/load_test_native.py` provisions disposable PG16 + scratch
  Redis + backend + SMS/carrier doubles + nginx TLS on loopback, seeds
  via `backend/cmd/capacity-fixture/`, and produces the same
  manifest/verdict/raw-report shape as the Docker runner. Node drivers'
  target/CA/sessions paths are env-overridable via
  `load-tests/target-config.mjs`.
- D2 — Generator is Node-only. Dead k6 scripts `load-tests/api-100rps.js`
  and `load-tests/baseline.js` DELETED (no k6 binary on this VM; the
  scripts were never invoked by either runner). Deviation from the
  spec's "k6 scripts" wording recorded here and in README.txt.
  `load-tests/users-1000.js` and `login-ramp.js` are NOT k6 — they are
  Node shims re-exporting the .mjs drivers, and are kept.
- D3 — Short baseline suite durations pinned: 10s/30s/90s (baseline,
  fixture-baseline, api-baseline, api-rolling, login-ramp,
  otp-background, journey-baseline, hot-sku). Full durations:
  api-sustained 30 min, journey 30 min, api-soak 2 h.
- D4 — Prod-target refusal = no-target-parameter design (hardcoded
  disposable domain `capacity.example.test`) PLUS a defense-in-depth
  guard (`resolveTarget()` in target-config.mjs) rejecting any other
  host, any non-loopback connect IP, and any non-https/http protocol.
  A "marked" target is one passing that guard. Unit-tested
  (target-config.test.mjs). Neither runner accepts a --url/--target
  argument (Python tests assert argparse rejects it).
- D5 — Latency failures are APP work. CAP-01 measures honestly, labels
  failures, and NEVER tunes app/server code to make latency pass. No
  server code was changed in this task.
- D6 — Intended deployment-hardware identity stays OPEN for CAP-02;
  nothing here invents it. All runs are labeled shared-VM/local-only.

## Gate table (specialist audit 2026-11-20 02:22 → after this task)

| Gate | Before | After |
| --- | --- | --- |
| 1,000 identities | EXISTS | EXISTS (unchanged; re-validated natively) |
| Failed-assertion gate | EXISTS | EXISTS |
| Dropped-arrival gate | EXISTS | EXISTS |
| Fixture-stock gate | EXISTS | EXISTS |
| Labeled baseline | EXISTS | EXISTS (D3 durations pinned) |
| Manifest | EXISTS | EXISTS (native manifest shape matches Docker runner) |
| Raw reports | EXISTS | EXISTS (per-profile summaries + .prom metrics) |
| Synthetic fixtures | EXISTS | EXISTS |
| Prod-target refusal | PARTIAL | EXISTS (D4: no-target-parameter + guard + unit tests) |
| Weights sum | PARTIAL | EXISTS (sum===100 in workload.mjs + validate_mix(); Node + Python tests) |
| Counts split | PARTIAL | EXISTS (countsFor() exact-split; 10/30/90s verified in tests) |
| Duplicate-effect | PARTIAL | EXISTS for callbacks (hot-sku) + cancels (new hot-account profile + SQL oracle); full ledger conservation still OPEN (FIN-013) |
| Staff profile | OPEN | IMPLEMENTED, run natively (--staff-baseline, 980 customers + 20 staff) |
| Hot-account profile | OPEN | IMPLEMENTED (hot-account.mjs: 100 paid cancels on one store + duplicate-cancel probe + obligation oracle) |
| Cold-cache profile | OPEN | IMPLEMENTED (--cold-cache: FLUSHALL + backend restart, cold vs warm labeled runs) |
| Fault profiles | OPEN | IMPLEMENTED (provider-timeout mode, Redis loss, backend SIGKILL restart convergence) |
| Headroom/recovery | OPEN | IMPLEMENTED (--headroom: 200 RPS 5 min + 100 RPS 2 min recovery) |
| Full price/ledger/refund oracle | OPEN | PARTIAL — price identity (payable/discount vs item snapshots), refund-obligation existence/uniqueness/amount bound, ledger sanity (negative/orphan) gated in both runners. Full ledger conservation across deferred releases/settlements stays OPEN (FIN-013): release postings are deferred by design, not due inside a run window. `paid_orders_without_ledger_entry` is reported informational, not gated. |
| Browser profile | OPEN (relay failed, zero rendered pages) | REPLACED — native puppeteer Chrome on loopback nginx TLS with leaf-SPKI pin. Diagnosis: relay pinned the CA cert's SPKI; Chrome's --ignore-certificate-errors-spki-list matches the LEAF SPKI, so the relay aborted after the TLS handshake before any HTTP request. If the native run renders pages, the hypothesis is confirmed; if not, root cause stays open. |
| Expiry/refresh/re-login + concurrent-device | OPEN | PARTIAL — session-lifecycle.mjs (10-user cohort): concurrent-device reads + real OTP re-login + logout probe. True expiry stays OPEN: no /auth/refresh endpoint exists and tokens are 3h JWTs. FINDING: POST /auth/logout returns success but does not revoke the token server-side (handleT01Logout is a no-op); the driver records old-token-still-valid as a failed gate, not a hidden pass. |
| Unread workload fields | OPEN | EXISTS — cpu_max_percent 70, unexpected_error_rate_lt 0.001, headroom_rps 200, recovery_seconds 120 now gated by validate_workload_contract() (both runners) + tests. |
| Stale password-login path | OPEN | FIXED — baseline.mjs CAPACITY_PROFILE=login now throws and directs to login-otp.mjs (customer password login was removed). |
| Dangling README reference | OPEN | FIXED — cap01-report.md reference replaced by this file + local-baseline-evidence.md. |

## Short baseline suite — native runs

(Harness repairs found while running natively — recorded here, not hidden:

- Guard-marker ownership: the native runner first created the
  `sellersho_test_guard` table as superuser gap13; `testdb.Open`
  then failed (test_runner cannot SELECT a gap13-owned table).
  Fixed in the runner (SET ROLE test_runner for the marker DDL) —
  the Docker runner and test-isolated-native.sh already did this.
- Carrier/SMS aliases: the backend's capacity adapters require the
  exact aliases `http://carrier:8082` / `http://sms:8081/v1`.
  /etc/hosts is a read-only bind mount in this cell, so the backend
  runs in a private mount namespace with a copied hosts file bound
  over /etc/hosts (aliases resolve to loopback only inside it).
- FIN005 fence (Nov 16, migration 202611160002): the financial-history
  seed flips profit_released false->true in raw SQL, which the new
  writer fence rejects without the transaction-local
  sellersho.fin005_writer='v1' GUC. Fixed in the harness
  (financial-history.sql sets it, same mechanism as the app's own
  release path). Would fail identically under Docker.
- VM RESTART during the short-suite run (2026-11-20 ~23:28 Tehran):
  the runtime restarted mid-fixture, killing the suite and wiping
  /tmp; libpq.so.5 and the postgres user were mid-restore by a
  concurrent process. Environment restored per
  fin008-env-fixed.md (libpq5 present, postgres uid 103/gid 105,
  o+x on /home/hatch, PG 16.15 cluster restarted on 5433,
  /usr/bin/redis-server back). Orphan disposable DB from the killed
  run verified by guard marker and dropped. Suite relaunched from
  scratch after the restore.
- RELEASE COHORT (2026-11-20 ~23:40 Tehran, APP finding, not a harness
  defect): `TestCapacityDataFloor` failed at
  `ReleaseClosedReturnWindows` — 0/100 released, every candidate
  cancelled with pq 57014 (the app's 300ms per-candidate budget).
  Measured per-candidate cost on the disposable DB, warm buffers:
  ledger_entries seq scan ~23ms (NO index on related_order_or_dispute),
  order_item_origins cast join ~49ms (oi.id::text =
  o.order_item_id::text defeats the index), order_items by order_id
  seq scan ~21ms, payment_transactions by order_id seq scan ~9ms,
  plus eligibility queries, locks and ledger posting. The cohort is
  now its own test (`TestCapacityReleaseCohort`, in
  release_cohort_test.go) and its own manifest gate
  (`release_cohort`: exit/released/status); both runners run it after
  the floor test and continue to the profiles either way. After one
  failed pass the 100 candidates sit in delivery-release retry
  backoff, so a second attempt in the same run finds 0 candidates.
  Fixing it is APP work (indexes / release-path cost) for a
  follow-up task — CAP-01 records the gate FAILED with measured
  numbers and does not tune app code (D5).
- Aged inserts (aged_test.go) have no ON CONFLICT — the fixture is
  single-shot per disposable DB; re-running it on an already-seeded
  DB fails on products_pkey. Runners always use a fresh DB, so this
  is a diagnostic note only, not a runner problem.
- Native-runner count bug (found 2026-11-20 ~00:05 Tehran, first time
  the fixture actually passed): the runner counted seeded rows with
  `id LIKE 'cap-%'` for every table, but users.id and
  product_variants.id are UUIDs — psql fails with `uuid ~~ unknown`.
  Fixed to use the fixture's own prefixColumn mapping (users ->
  full_name, product_variants -> variant_name). Docker runner does
  not share this bug.
- Oracle SQL %-format crash (found 2026-11-20 ~00:20 Tehran, first
  time api-baseline completed): `oracle_commerce` builds its SQL with
  `%`-formatting, but five LIKE patterns were unescaped
  (`'cap-product-%'` x4, `'cap-customer-%'` x1) — Python raised
  ValueError and killed the suite after api-baseline. Fixed to `%%`;
  regression test `test_oracle_sql_percent_formats_evaluate` added
  (native tests now 6/6).
- FULL SHORT SUITE, native run 2026-11-20 ~01:00 Tehran (report
  sellersho-cap-report-500f7a0b7410, nonce b90799615573458d66d8b3a4):
  fixture exit 0 (measured counts: users 1210, suppliers 100, stores
  100, products 10101, variants 50001, orders 100000, saleable
  5,000,000); release cohort FAILED 0/100 (recorded gate); 3/16
  profiles PASSED (fault-provider-timeout, fault-redis-loss,
  fault-restart); 13 FAILED, all labeled with measured numbers.
  ALL commerce oracles MATCHED within stated scope (price identity,
  ledger sanity, refund obligations, hot-sku, hot-account) — money
  is conserved even while latency/throughput gates fail.
  Route p95s (api-baseline): list 1089ms, detail 4369ms, store
  1606ms, account 993ms, quote 3699ms, create 5091ms; journey
  list p95 12210ms, create 7815ms, 0 completed checkouts.
  Driver throughput ceiling on this VM: api-baseline 357/1000
  requests (643 dropped), headroom burst 20923/60000 (39077 dropped).
  CPU max 64.6%–375.6% vs 70% contract (session-lifecycle 64.6% is
  the only commerce profile under it). Counter gates matched except
  api-rolling (api 1070 vs domain_allow 1080), journey-baseline
  (6133 vs 6744), staff-baseline (846 vs 932), headroom (21047 vs
  21258) — requests that passed domain auth but never reached the
  backend's api metrics (in-flight at scrape, or nginx→backend
  failures under load). nginx log: 328× 499 on payment callbacks,
  193× 499 on order creation — the 10s driver timeout fires while
  the backend is still working.
  OTP: otp_send 43% 503s on login-ramp (362/849), 28% on
  otp-background (184/668) — the app's dependency-budget overload
  shedder; otp_verify 100% OK both runs.
  Browser: 10/10 pages HTTP 200 with leaf-SPKI pin (relay
  diagnosis CONFIRMED — native mode works), but all 10 warm-render
  gates failed: pages load only the 29-char shell (transfer 3183B,
  0 page errors) — the SPA bundle does not render content in the
  window; timings excluded per plan.
  session-lifecycle: concurrent-device 10/10, re-login 10/10,
  old-token-after-logout 10/10 (logout does not revoke — recorded).
  hot-account: 100 paid, 0 cancelled (cancel p95 8630ms — cancels
  never completed in-window; oracle matched vacuously on 0 cancels).
  cold-cache-warm: fixture_preparation FAILED (warm run never
  started); cold run list p95 2600ms, create 5011ms.
  fault_redis_loss: health 503 during loss, 200 after restart.
  fault_restart: oracle identical before/after restart, converged.
  OPEN: hot-sku 409 mystery — see next bullet; drivers use obsolete
  `?page=N` list pagination (page 2+ → 400 "invalid catalog page
  request"; PERF-01 cursor contract) — harness fix pending, not an
  app bug; browse 94% errors in otp-background are that 400.
- HOT-SKU 409 MYSTERY (open, under investigation): hot-sku preparation
  quote returns 409 inventory_origin_unavailable, yet the runner's
  hot-sku oracle reports the SUCCESS state (sellable 0, captured 5,
  sku 0, paid_orders 5) in two independent runs (main profile and
  fault-restart's pre-restart oracle). Minimal-DB reproduction shows
  the origin query resolves fine for the seeded hot product
  (variant UUID matches Postgres md5 exactly), so the live-run stock
  must have been consumed before hot-sku ran — but no driver requests
  cap-hot-product except hot-sku itself. Decisive test running:
  fresh env with --profiles hot-sku ONLY (hot-sku first); if it
  passes, the writer is one of the six earlier profiles and the fix
  is sequencing (hot-sku before commerce) or excluding the hot
  product from commerce flows.
  UPDATE 2026-11-20 ~01:10 Tehran: hot-sku-first ISOLATION run
  (report 1037f06dc41a) — the profile WORKS mechanically when first:
  5 paid, 59 explicit conflicts, 115 measured + 205 helper calls,
  oracle matched. It failed only latency gates (initiate/callback
  p95) and count gates (59 vs 95 conflicts, 1 vs 10 duplicate
  callbacks, 45 failures — mostly 10s driver timeouts on a slow
  VM). So the full-suite 409 is pre-consumption by an earlier
  profile; bisection with a kept env in progress to find which.
  SOLVED 2026-11-20 ~01:35 Tehran — NOT pre-consumption at all: the
  fault-restart profile re-ran hot-sku.mjs under the SAME driver
  name ('hot-sku'), overwriting the main profile's summary/oracle/
  exit with the re-run's expected 409 (the main run had already
  bought the five units). The main run had succeeded mechanically
  (5 paid, oracle matched); only its summary file was clobbered.
  Fixed in the native runner: the re-run now uses name
  'hot-sku-restart' and oracle key 'hot_sku_oracle_restart'; only
  oracle convergence decides fault-restart. Regression test
  `test_fault_restart_does_not_clobber_hot_sku` added (native tests
  now 7). Also noted: --keep-env stops all processes
  and only preserves the work dir + DB (it does not leave a runnable
  env); the kept bisect DB was verified by guard marker and dropped.
- DRIVER PAGINATION FIX (2026-11-20 ~01:15 Tehran, harness defect):
  all four commerce/browse drivers requested
  `/api/v1/products?page=2..20` — the PERF-01 cursor contract
  (internal/catalogpage/cursor.go ParseSize) rejects any page other
  than 1 with 400 "invalid catalog page request". That is why
  otp-background browse was 94% errors and every commerce profile's
  list gate failed. Fixed to `?limit=24` (valid first page) in
  commerce-baseline.mjs, commerce-rolling.mjs, login-otp.mjs,
  users-1000.mjs. Deep cursor-walking (nextCursor) is a future
  extension, recorded here, not silently assumed. Node tests 35/35.
  VALIDATED in the partial re-run (report 17a1c547136f, killed by the
  second VM restart mid-api-rolling): api-baseline list 78 requests
  with only 4 failed assertions (was 102/107 — the 400s are gone;
  remaining failures are latency, p95 4492ms); otp-background browse
  errors 18% (was 94%).
- SECOND VM RESTART (2026-11-20 ~01:29 Tehran): the runtime restarted
  again mid-suite (api-rolling), killing the re-run that was
  validating the pagination + fault-restart fixes. Environment
  restored a second time (libpq5 dpkg, postgres uid 103/gid 105,
  o+x, PG 16.15 restarted; nginx + redis-server reinstalled by the
  concurrent restore process; orphan disposable DB verified by guard
  marker and dropped). Targeted validation re-launched
  (--profiles hot-sku,fault-restart) to prove the fault-restart fix
  without a second full 30-minute run.
- Aged-fee fixture stale since 4524ee3 (Nov 15): the assessment now
  charges ONLY certified receipt lots, which the synthetic aged
  cohort never had (lots require a real QC/return receipt; missing
  lots are explicit unknown history, never invented debt). The old
  expectation (3320 days × 4160 Toman) failed on current HEAD and
  would fail identically under the Docker runner — the "245s pass"
  evidence predates 4524ee3. Repaired in
  `backend/cmd/capacity-fixture/aged_test.go` (harness-only change):
  the cohort now correctly expects zero examined receipts/charges,
  with physical conservation (2000 units) still asserted. The
  fee-charging path is covered by storage_history_integration_test.go.
  No app code was changed.)

(Results appended after each native run; see load-tests/evidence/
native-<nonce>-compact.json for the committable record and
~/workspace/cap01-native-runs/sellersho-cap-report-<nonce>/ for raw
reports. Latency failures are labeled FAILED with numbers, never
hidden.)

## Full-duration runs

- --api-sustained (30 min), --journey (30 min), --api-soak (2 h):
  started as background processes; status + log paths recorded here
  when finished. Never claimed complete before they finish.
  Rolling-feeder queue behavior (bound 2000, 15-min payment holds):
  starvation/drop behavior recorded honestly from the run summaries.

## What remains open (exact reasons)

- Full ledger conservation across deferred releases/settlements
  (FIN-013): release postings happen after the return window, not
  inside a run window; no run can observe them end-to-end.
- True token-expiry test: 3h JWT lifetime, no refresh endpoint;
  needs a short-lived-token test build or a 3h+ run — out of scope
  for CAP-01, recorded for AUTH-01/CAP-02.
- Logout revocation: app behavior (no-op logout) is AUTH-01 work,
  not a harness defect; CAP-01 only records it.
- Intended deployment-hardware identity + CAP-02 certification (D6).
- Browser timings are excluded from capacity gates (shared-VM
  Chrome CPU unmeasured), same as the Docker runner's rule.
- Real-provider certification: doubles only; local simulation is
  not real-provider certification (FIN-010 stays the launch blocker
  for real payments).

## Files changed in this task

- NEW load-tests/target-config.mjs (+ .test.mjs), workload.mjs (+ .test.mjs)
- NEW load-tests/hot-account.mjs, session-lifecycle.mjs
- NEW scripts/load_test_native.py, scripts/test_load_test_native.py
- NEW load-tests/cap01-progress.md (this file)
- EDIT load-tests/arrival-plan.mjs, baseline.mjs, commerce-baseline.mjs,
  commerce-rolling.mjs, users-1000.mjs, login-otp.mjs, hot-sku.mjs,
  browser-assets.mjs, browser-target.mjs (+ .test.mjs)
- EDIT scripts/load_test_isolated.py, scripts/test_load_test_isolated.py
- EDIT load-tests/README.txt, profiles.json, seed-manifest.json
- DELETE load-tests/api-100rps.js, load-tests/baseline.js (dead k6)
- No Go/app code changed; no migration; no binary change (harness only).
