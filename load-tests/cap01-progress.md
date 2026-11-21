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
| Counts split | PARTIAL | PARTIAL (countsFor() splits exactly in tests, but the live path is arrivalPlan(), which computes the split inline and never calls countsFor — the exact-split guarantee is proven for the helper, not for the path the drivers run) |
| Duplicate-effect | PARTIAL | PARTIAL (callback duplicate-effect proven for hot-sku; the hot-cancel oracle is vacuous on 0 cancels — b907: 100 paid, 0 cancelled, cancel p95 8630ms, none completed in-window, so the duplicate-cancel probe never exercised a completed cancel; full ledger conservation still OPEN (FIN-013)) |
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
  diagnosis CONFIRMED — native mode works), but render was
  PARTIAL/intermittent, not a uniform shell (an earlier draft of this
  note wrongly said all pages loaded only the 29-char shell): warm
  principals 1 and 8 DID render fully (11,365 chars, root_children
  2; the same two reached 994 chars cold) while 5 warm pages served
  the 29-char shell and 3 rendered 0 chars; all 10 warm-render gates
  still failed (the two full renders each carry 1 broken image).
  Failures underneath: broken logo
  /static/uploads/logo/logo-icon.png 404 (4 requests, warm
  principals 1 and 8) + /api/v1/domain-store 404 on 17 requests
  (every page, cold and warm) + blocked_external 20 whose URLs were
  never logged in b907 — browser-assets.mjs now records every
  blocked URL in blocked_urls. 0 page errors. Timings excluded per
  plan.
  Headroom oracle variance (informational, not gated):
  paid_orders_without_ledger_entry was 6 in the headroom burst
  oracle vs 0 in every other commerce oracle in b907 — unexplained;
  no cause established, no gate depends on it.
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

## Critic conditions — closed post-b907 (2026-11-20)

- Evidence integrity (condition 1): write_compact_evidence() now
  copies profile_failed + sms_double_counts + carrier_double_counts
  (b907's compact omitted all three); verdict.json now writes the
  baseline profile's own exit under baseline_exit plus the suite exit
  under suite_exit (b907's verdict said baseline_exit 1 while the
  profile itself exited 0). Tests:
  test_compact_evidence_includes_profile_failed_and_double_counts +
  test_verdict_evidence_uses_baseline_profile_exit_not_suite_exit
  (native Python tests now 9).
- Doc honesty (condition 2): profiles.json records every b907
  outcome (no "native run pending" remains); README.txt:103 no
  longer describes deleted api-100rps.js in present tense; browser
  claim corrected above; gate table downgraded (counts-split and
  duplicate-effect/cancels → PARTIAL); headroom
  paid_orders_without_ledger_entry: 6 variance recorded above.
- Target-guard DNS hole (condition 5): CLOSED. resolveTarget() now
  resolves the hostname before any driver request when no IP is
  pinned and refuses anything but loopback (Docker generator
  excepted: private address + /.dockerenv only; the SMS double
  hostname 'sms' gets the same treatment). The hole was live, not
  theoretical: this VM's resolver answers 198.18.245.176 for
  unresolvable names (getent: capacity.example.test →
  198.18.245.176; any unresolvable name → 198.18.245.17x), so a
  manual driver without CAPACITY_TARGET_IP would have connected
  there. Unit tests prove a hostile mapping is refused
  (target-config.test.mjs; Node tests now 37).
- Concurrent-run hardening (critic §3): the drivers' summary path is
  run-scoped (CAPACITY_SUMMARY_FILE under the run's work dir; they
  fall back to /tmp/summary.json only when run manually) and the
  nginx/SMS ports are drawn from free_port() (were fixed 8443/8081).
  The CARRIER port stays fixed at 8082 — NOT pretended fixed: the
  backend's startup gate (cmd/server/capacity_adapters.go) requires
  CAPACITY_CARRIER_BASE_URL to be exactly http://carrier:8082, and
  changing that gate is app code, out of scope here. The first
  post-fix run proved the constraint: with a dynamic carrier port
  the backend refused to start ("capacity carrier endpoint must be
  the isolated harness alias", run a7176686b6b71af3f37c3a45). So two
  concurrent native runs still collide on the carrier double;
  start_doubles() now fails fast with a clear error naming the held
  port instead of dying opaquely. Full concurrent-run support needs
  a backend-gate change in a follow-up task. Proven in the field
  while fixing: the killed long run's orphaned commerce-rolling
  driver (pid 15270, PPID 1) kept writing /tmp/summary.json after
  its runner died; it was terminated during env cleanup, and the
  run-scoped path makes that class harmless.
- Fault-restart regression test: test_fault_restart_does_not_clobber_hot_sku
  is now behavioral — it drives the real run_fault_restart with
  stubbed environment collaborators and asserts the main hot-sku
  summary, oracle and exit survive the re-run + restart.

## Long runs — killed, not finished (2026-11-20 ~05:36 Tehran)

- The coordinator killed the long-duration runs (Python runner pid
  10913). Partial evidence in
  ~/workspace/cap01-native-runs/sellersho-cap-report-1e6e4ae9cfa1 +
  long-runs.log: the run had seeded the fixture (disposable DB
  sellersho_test_589bba890c470bbeaf98191a, release cohort 0/100
  FAILED as in b907) and started api-sustained before the kill; no
  manifest/verdict was written (SIGKILL, no finally block). The
  ~05:00 Tehran finish claim is ABANDONED — no long run completed.
  Long runs are CAP-02 input, not CAP-01 acceptance.
- 7-hour fixture-child elapsed anomaly: OPEN harness-reliability
  item for CAP-02 diagnosis. Candidates: VM freeze/restore
  distortion, shared-VM contention — neither established. (Reported
  in the coordinator's kill report; no corroborating line was found
  in the run logs available to this team.)

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

## Clean full short suite, post-fix (2026-11-20, nonce 05879119c0db957ebaaeaa03)

- Run 02:20–02:55 UTC (05:50–06:25 Tehran), ~35 min, report
  sellersho-cap-report-f12c4cf62694, compact
  load-tests/evidence/native-05879119c0db957ebaaeaa03-compact.json,
  console + Node/Python/vet/route-table outputs in
  load-tests/evidence/cap01-clean-short-suite.txt. Fixture exit 0
  (users 1210, suppliers 100, stores 100, products 10101, variants
  50001, orders 100000, saleable 5,000,000 — identical to b907);
  release cohort FAILED 0/100 again (same app finding, seq-scan
  release path). Baseline smoke PASSED (exit 0); 13 profiles FAILED,
  3 passed (fault-provider-timeout, fault-redis-loss,
  fault-restart) — same shape as b907. ALL commerce oracles MATCHED
  within stated scope. Verdict.json in the NEW shape: baseline_exit
  0, suite_exit 1. Compact now carries profile_failed + sms/carrier
  double counts (verified read back).
- SUPERSESSION: b907's list/browse numbers are SUPERSEDED by this
  run and must not be quoted as the current baseline — the
  pagination fix changed every commerce list request (b907's
  api-baseline list p95 1089ms was measured on fast-failing 400s;
  this run's 3794ms is on valid first-page requests).
- Per-profile (all FAILED on latency/throughput/counter gates
  unless noted; oracles matched):
  fixture-baseline 771/1000, 229 dropped, max arrival lag 59ms;
  login-ramp otp_send 486 OK / 334x503 (40.7%), otp_verify 486/486
  OK; otp-background browse 1145 OK / 273x500 / 118x503 (25.5%
  errors, all 500/503 — the pagination 400s are gone), otp_send 280
  OK / 62x500 / 36x503, otp_verify 227 OK / 24x500 / 29x503;
  api-baseline 296/1000 (704 dropped), route p95s list 3794,
  detail 3517, store 1473, account 912, quote 2138, create 5499,
  initiate 4786, callback 5402ms, counter gate FAILED (api 521 vs
  domain_allow 536); api-rolling 713/3000 (2287 dropped), 0
  completed checkouts, route p95s ~5.1–5.7s; journey-baseline 5812
  requests, 0 completed checkouts, list p95 10498, create 7938ms;
  hot-sku 5 paid, oracle matched, FAILED latency/count gates
  (initiate p95 2738, callback 5877, duplicate-callback 8019ms) —
  CLOBBER FIX VERIFIED: hot-sku-summary.json is the main run
  (paid 5) and hot-sku-restart-summary.json is separate (paid 0,
  preparation failed as expected); staff-baseline 638/3000 (2362
  dropped), 0 checkouts; hot-account 100 paid, 0 cancelled (cancel
  p95 8804ms) — oracle vacuous again, confirming the PARTIAL
  downgrade; session-lifecycle concurrent 10/10, re-login 10/10,
  old-token 10/10, only failed gate logout_does_not_revoke_token;
  browser-assets 10/10 pages 200, warm render 6x29-char shell,
  3x0 chars, 1x994 — NO full render this run (b907 had two full
  11,365-char warm renders; render is intermittent across runs
  too); blocked_external 13, URLs NOW LOGGED — the storefront
  hotlinks Snapp CDN category images
  (web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/{appliances,beauty,digital,fashion,gold}.png;
  APP finding, not a harness defect — the category images are
  external hotlinks, follow-up, not CAP-01); domain-store 404 x17
  again; logo 404 x1 (warm principal 4); counter gate CHECK
  (domain_allow 198 vs network_responses 150); cold-cache cold
  222/1000 (list p95 5005, create 5563), warm 293/1000 (list 4481,
  create 5060) — warm RAN this time (b907's warm never started);
  headroom burst 16877/60000 (43123 dropped), recovery 3848/12000
  (8152 dropped), 0 checkouts, recovery gate failed;
  fault-provider-timeout / fault-redis-loss / fault-restart all ok
  (restart oracle converged).
- CPU max 10.8% (session-lifecycle) – 274.3% (headroom-recovery);
  only session-lifecycle (10.8%) and baseline (16.9%) under the 70%
  contract. SMS double 776 sends, 0 rejected; carrier 694 quotes,
  0 rejected, 0 outbound requests.
- paid_orders_without_ledger_entry variance: api-baseline 2 this
  run (b907: headroom 6, api-baseline 0) — the value moves between
  profiles and runs, still unexplained, informational only, not
  gated.

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

## Post-fix changes (critic conditions, 2026-11-20 — this section)

- EDIT scripts/load_test_native.py (compact evidence keys,
  verdict_evidence(), dynamic nginx/SMS ports, run-scoped summary
  path, carrier-port fail-fast), scripts/test_load_test_native.py
  (compact + verdict tests, behavioral fault-restart test)
- EDIT load-tests/target-config.mjs (+ .test.mjs: DNS-hole guard)
- EDIT load-tests/browser-assets.mjs (blocked_urls logging),
  sms-double.mjs / carrier-double.mjs (env port override),
  baseline.mjs, commerce-baseline.mjs, commerce-rolling.mjs,
  hot-account.mjs, hot-sku.mjs, login-otp.mjs,
  session-lifecycle.mjs, users-1000.mjs (run-scoped summary path)
- EDIT load-tests/profiles.json, README.txt, cap01-progress.md
- NEW load-tests/evidence/native-05879119c0db957ebaaeaa03-compact.json
  (clean post-fix suite), native-a7176686b6b71af3f37c3a45-compact.json
  (aborted carrier-gate run), cap01-clean-short-suite.txt
  (Node/Python/vet/route-table outputs + both runs' console output)

## CAP-02 W-1.1 diagnostic re-run (2026-11-21, run nonce b5b450178a3953f7445f8ebb, output dir nonce 2abdc5f9986649d119f9d714)

DIAGNOSTIC, certified:false, never a capacity result. Same VM shape as
0587, same 16-profile short suite, native runner. Purpose: measure the
combined PERF-02 + PERF-03 + EDGE-01 effect against the 0587 compact,
profile by profile. Suite exit 1 (expected: 13 profiles failed).
Report: ~/workspace/cap02-runs/sellersho-cap02-diag-2abdc5f9986649d119f9d714/
Compact: load-tests/evidence/native-b5b450178a3953f7445f8ebb-compact.json

Per-profile diff vs 0587 (requests delivered / offered; route p95 ms;
SLOs: read p95 300ms, mutation p95 750ms — NOT loosened):

| profile | 0587 delivered | b5b4 delivered | 0587 route p95 | b5b4 route p95 | verdict |
|---|---|---|---|---|---|
| fixture-baseline | 771/1000 (229 dropped) | 996/1000 (4 dropped) | — | p95 5.7ms | FAILED both (arrival/identity gates) |
| login-ramp | 1306 req; otp_send 486 OK / 334×503 (40.7% shed) | 1486 req; otp_send 487 OK / 512×503 (51.3% shed) | otp_send p95 118ms | otp_send p95 72ms | FAILED both; shedder cap CONFIRMED: ~487 OK / 60s ≈ 8.1/s, unchanged |
| otp-background | 2194 req; browse 1145 OK / 273×500 / 118×503 | 2596 req; browse 1746 req, 40 errors (2.3%) | browse p95 3991ms | browse p95 2323ms | FAILED both; browse error rate 25.5% → 2.3% |
| api-baseline | 296/1000 (704 dropped) | 495/1000 (505 dropped) | list 3794, detail 3517, store 1473, account 912, quote 2138, create 5499, initiate 4786, callback 5402 | list 911, detail 822, store 431, account 191, quote 302, create 1540, initiate 435, callback 3276 | FAILED both; counter gate now MATCHED (was FAILED); account p95 191ms now UNDER the 300ms read SLO |
| api-rolling | 713/3000 (2287 dropped), 0 checkouts | 1546/3000 (1454 dropped), 0 checkouts | list 5508, detail 5577, create 5691, callback 5474 | list 1022, detail 884, create 1490, callback 2256 | FAILED both; counter gate now MATCHED (was FAILED) |
| journey-baseline | 5812 req, 0 checkouts | 7373 req, 2 checkouts | list 10498, detail 4980, create 7938 | list 6430, detail 2615, create 4535 | FAILED both; first completed checkouts in a journey run (0 → 2) |
| hot-sku | 5 paid | 5 paid | initiate 2738, callback 5877, dup-callback 8019 | initiate 600, callback 901, dup-callback 1323 | FAILED both (latency gates); oracle matched both |
| staff-baseline | 638/3000 (2362 dropped) | 984/3000 (2016 dropped) | list 5518, create 5612, callback 5697 | list 3403, create 5479, callback 5316 | FAILED both |
| hot-account | 100 paid, 0 cancelled, cancel p95 8804ms | 100 paid, 64 cancelled, cancel p95 4016ms | cancel 8804, dup-cancel 8710 | cancel 4016, dup-cancel 2214 | FAILED both (latency gates); cancels now COMPLETE 64/100 — exactly the FIN-013 measured range (58–64/100) |
| session-lifecycle | 70 req; logout_does_not_revoke_token | 70 req; logout_does_not_revoke_token | — | — | FAILED both (same single gate) |
| browser-assets | 187 req; render gates failed | 285 req; render gates failed | — | — | FAILED both |
| cold-cache cold | 222/1000 | 493/1000 | list 5005, create 5563 | list 974, create 1298 | FAILED both |
| cold-cache warm | 293/1000 | 480/1000 | list 4481, create 5060 | list 1127, create 2431 | FAILED both |
| headroom burst | 16877/60000, 0 checkouts | 22520/60000, 0 checkouts | list 3721, create 5883 | (burst p95s in summary) | FAILED both |
| headroom recovery | 3848/12000, 0 checkouts | 5893/12000, 2 checkouts | list 5084, create 5290 | — | FAILED both |
| fault-provider-timeout | ok (oracle converged) | ok | — | — | ok both |
| fault-redis-loss | ok | ok | — | — | ok both |
| fault-restart | ok | ok | — | — | ok both |

Release cohort: 0587 released 0/100 (FAILED); b5b4 released 99/100
(FAILED, want 100) — the 100th candidate hit the 300ms per-candidate
budget (57014). PERF-02's claim "0 → 99/100" is CONFIRMED by this
re-run: 99 released with paired balanced ledger entries, release replay
0. The gate still wants 100, so the cohort gate stays FAILED.

CPU max: api-baseline 135.3% (of the 1.5-shared-vCPU VM — over the 70%
SLO as before; this VM cannot prove the CPU SLO), headroom 137.5%,
journey 126.7%. CPU is not a pass/fail signal on this VM shape.

paid_orders_without_ledger_entry — EXPLAINED (W-1.1 informational
finding, was "6 in b907 headroom, 2 in 0587 api-baseline"): the count
now scales with the paid orders each profile produced in its window
(api-baseline 26, api-rolling 78, cold 25/37, headroom 21, headroom
recovery 121, journey 2, staff 44, fault-provider-timeout 0). It equals
unreleased paid orders in the window — paid orders have no ledger entry
by design until staged profit release (FIN-013 oracle check 2, which
counts only RELEASED paid orders). Corroborated by the exact matches:
journey 2 == 2 completed checkouts; fault-provider-timeout 0 == 0 paid
(timeout mode verifies nothing). NOT a gate; recorded, not hidden.

HARNESS DEFECT FOUND BY THIS RUN (fixed in the same change set):
CancelOrder sets ONLY status='cancelled' — it never touches
payment_status (which stays 'paid'). The runner's oracle_commerce
(paid_cancelled_without_obligation) and oracle_hot_account
(hot_cancelled_paid, hot_cancelled_without_obligation) filtered on
payment_status IN ('cancelled','refund_pending') — a predicate that
matches ZERO rows by construction (no code path ever sets those
values). In 0587 the bug was invisible (0 cancels completed); in b5b4
64 cancels completed but the oracle still reported 0. Fixed in
scripts/load_test_native.py, scripts/load_test_isolated.py and
scripts/cap02_reconcile.py to payment_status='paid' AND
status='cancelled' (FIN-013 check 3 semantics). The FIN-013 leg of the
W-2.1 gate already used the correct predicate, so the consolidated
gate would have caught a missing obligation where the runner oracle
was blind.

## CAP-02 W-1.5 harness-reliability records (2026-11-21)

- 7-hour fixture-child elapsed anomaly: NO corroborating line exists
  in ~/workspace/cap01-native-runs/long-runs.log (untimestamped, ends
  at the api-sustained start); the figure exists only in the
  coordinator's kill report. Verdict: BOUNDED UNKNOWN, with mitigation.
  The runner now records fixture elapsed (monotonic seconds + wall
  start/finish) in the manifest (fixture_elapsed) on every run, so a
  future anomaly is bounded by measurement: if the monotonic clock
  reads ~7h the time really passed; if it reads minutes while a child
  timestamp reads hours, the child's clock jumped (VM freeze/restore).
  No cause is claimed.
- --keep-env FIXED (scripts/load_test_native.py close()): it used to
  stop every process and keep only the work dir + DB, so "kept-env"
  was a work-dir-only stub. It now leaves a RUNNABLE env (processes
  stay up) and writes env.sh (ports, DB, DSN, Redis URL, CA, sessions,
  PIDs; 600) + stop-env.sh (kills the PIDs, drops the disposable DB;
  700).
- Carrier port 8082 concurrency limit: RECORDED as a known constraint.
  The backend startup gate (backend/cmd/server/capacity_adapters.go)
  requires CAPACITY_CARRIER_BASE_URL exactly http://carrier:8082, so
  two native runs on one host collide on the carrier port and
  start_doubles() fails fast naming the held port. The backend-gate
  change is separate app work, NOT CAP-02 — the gate was not changed.

## CAP-02 W-3.5 failed replacement under load — BLOCKED (2026-11-21)

DO NOT BUILD. Blocked on REL-02 Q1 (5-min maintenance window) + the
staged design, which does not exist. R-3 is expected to FAIL until the
staged design exists — that failure is evidence, not a defect to hide.
Recorded in docs/operations/capacity-evidence.txt §5 and
load-tests/profiles.json (failed-replacement: BLOCKED).

## CAP-02 verification run 1 — 7 harness/driver bugs found + fixed (2026-11-21)

Run nonce 15a62497c3491b8cdbdd4a3a, DB sellersho_test_cdd7a6b35d436851718e95fb.
Suite exit 1. fixture-baseline PASSED (release cohort 100/100 PASS — the
first 100/100; W-1.1 was 99/100). coupon-burst, independent-account,
session-lifecycle, staff-baseline, fault-boundary-b1..b4, fault-db-loss
FAILED; fault-worker-failover ABORTED the suite (uncaught psql error in
the release-batch query — ledger_entries has no order_id column — so
fault-redis-load never ran). Compact:
load-tests/evidence/native-cdd7a6b35d436851718e95fb-compact.json.

1. Orders-list shape: GET /api/v1/orders returns {orders:[...]}, not a
   bare array. The boundary replay threw although the replay HAD paid;
   the session-lifecycle disjointness check passed vacuously (both []).
   Fixed in fault-boundary.mjs + session-lifecycle.mjs (read body.orders).
2. b2/b3 callback outcome: nginx returns 502 when the upstream dies; a
   backend killed mid-response can surface as 500. A 5xx during the kill
   window now counts as 'lost' (effect unknown; replay + gate decide).
3. b4 deadline: the create-order response carries NO paymentExpiresAt.
   The setup now records created_at and the runner computes deadline =
   created_at + 15 min.
4. Coupon/payout URLs: the drivers used /api/v1/seller/stores/<slug>/...,
   which only serves variant routes (http.NotFound otherwise). The real
   routes are /api/v1/stores/<slug>/discount-codes|payouts. Fixed the
   prefix in coupon-burst.mjs + independent-account.mjs.
5. DB-loss mechanism: REVOKE CONNECT does not block the DB owner
   (test_runner owns the run DB). Now ALTER DATABASE ... CONNECTION
   LIMIT 0 + pg_terminate_backend (verified live).
6. db-loss + redis-load load windows were 60 s; the driver only accepts
   30/120/300/1800/7200. Now 120 s.
7. Worker-failover release SQL used l.order_id / direction / amount_toman;
   the columns are related_order_or_dispute / type / amount (release pair:
   operator='system:delivery-lock-releaser'). Fixed in both runners.
   (Docker runner had the same bugs 3, 6, 7; its db-loss uses docker
   stop/start, so no REVOKE issue there.)

W-1.4 / W-2.3 evidence from run 1: independent-account accounts_used=100;
session-lifecycle switch_ok=10, negative control 10/10; staff-baseline:
all 20 staff-slot principals present, 50 report-leg requests.
Re-verification run 2 (nonce c0837439f88ffcb1) in progress with all fixes.

## CAP-02 W-2.1 gate proof (2026-11-21, kept DB of run 1)

Clean run PASS (exit 0) — after two earlier clean attempts failed closed
first: clean FAIL (FIN-014 leg error) → clean2 FAIL (INV-12) → clean3 PASS.
Four plants, each flipped its leg to FAIL and
was restored: commerce negative ledger row (ledger_negative_amount=1;
restore needs the append-only triggers disabled/re-enabled — a
reversing entry does NOT restore the count); FIN-013 payable_amount +1
(orders_payments=1; also trips commerce payment/price checks — one
mismatch, not three defects); FIN-014 paid order -> cancelled
(certain_defects=3, the planted order outside the seed baseline;
also trips FIN-013 check 3 + commerce paid_cancelled — documented
overlap); INV-12 products stock+1 (inv_product_projection_drift=1).
Final clean re-run PASS. Refusals: non-disposable name -> exit 2;
guard-marker mismatch -> exit 2. Gate-shape findings: FIN-014 seed
baseline {PAYOUT-PENDING-001, PAYOUT-PENDING-002}; INV-12 capture_moves
at PRODUCT level (movements carry NULL variant_id) with the quadratic
per-variant form rewritten (~0.5 s vs >300 s timeout);
inv_variant_projection_drift N/A (balances never carry variant_id);
inv_product_projection_drift excludes seed prod-1..prod-6. Full record:
docs/operations/capacity-evidence.txt §5; reports in
~/workspace/cap02-runs/sellersho-cap02-verify-15a62497c3491b8cdbdd4a3a/
w21-*.json. The run-1 disposable DB was dropped after the proof.

## Environment incident (2026-11-21)

A runtime restart wiped /tmp (the kept env) and emptied
~/pgx-rootfs/usr/lib/postgresql/16/bin|lib, and stopped the PG cluster
on 5433. Rebuilt per ~/workspace/fin008-work/fin008-env-fixed.md:
re-extracted the PG 16 server .deb into ~/pgx-rootfs; libpq5 +
libllvm17t64 already present system-wide; postgres uid 103/gid 105
already existed; chmod o+x /home/hatch; restarted the cluster from
the surviving ~/gap13pg/data (the run-1 DB survived — the W-2.1 proof
ran against it after the rebuild). The in-flight W-2.1 reconcile never
wrote its output and was re-run.

## CAP-02 verification run 2 — fixes proven at mechanism level; bug 8 found (2026-11-21)

Run nonce c0837439f88ffcb1, DB sellersho_test_508835e1a582f3a35a2d8bec.
Suite exit 1. fixture-baseline FAILED (release cohort 99/100 — run 1's
100/100 was the lucky draw; 99/100 is typical). coupon-burst,
independent-account, session-lifecycle, staff-baseline FAILED on
latency/overload gates only. fault-boundary-b1 ABORTED the suite at the
negative-control restore (bug 8); b2/b3/b4, db-loss, worker-failover,
redis-load never ran. Compact:
load-tests/evidence/native-508835e1a582f3a35a2d8bec-compact.json.
Mechanism proofs: coupon creation 2x201, silent_discount=0 both
variants, runner oracle gate MATCHED; independent-account payouts reach
the handler (153x201), accounts_used=100; session-lifecycle ONLY failed
gate is logout_does_not_revoke_token, switch_ok=10,
switch_orders_disjoint=10 (real order lists), negative control 10/10;
b1 replay exactly once (1 order row), W-2.1 gate after replay PASS (all
4 legs), negative-control gate FAIL (FIN-013 check 1, as designed).
BUG 8: the unplant DELETEs the planted receipt + verification, but both
tables have BEFORE DELETE OR UPDATE triggers calling
immutable_order_refund_evidence(). Fixed in both runners: disable both
triggers, delete, re-enable (verified live on the run-2 DB: 3 rows
deleted, 0 left, both triggers 'O'). The run-2 kept env was verified
runnable (health 200) and torn down via stop-env.sh (processes stopped,
DB dropped) — the W-1.5 --keep-env fix proven end-to-end.
Continuation run 3 (b1, b2, b3, b4, db-loss, worker-failover,
redis-load) in progress, nonce 1062d3b72990574d.

## CAP-02 verification run 3 — b1/b2/b3 PASSED; bugs 9/10/11 found + fixed (2026-11-21)

Run nonce 1062d3b72990574d, DB sellersho_test_c4a555840848d7f556e72bd5.
Suite exit 1. fault-boundary-b1/b2/b3 PASSED (replay exactly once,
W-2.1 gate PASS, negative control FAIL -> restore -> PASS) — the W-3.1
callback boundaries are proven. b4, db-loss, worker-failover FAILED;
redis-load never ran. Compact:
load-tests/evidence/native-c4a555840848d7f556e72bd5-compact.json.
BUG 9 (b4): the slow-expiry trigger used pg_sleep(10), exceeding both
the expiry job's 300 ms per-candidate timeout and its 3 s per-attempt
deadline — the candidate failed at 300 ms on every attempt and the
order never expired. Fixed (both runners): pg_sleep(0.15) + poll
0.2 s -> 0.05 s.
BUG 10 (db-loss): the 10 s lead before the loss is shorter than the
driver's ~15 s sequential bootstrap; the loss landed on principal 67's
bootstrap_order (503) and the load failed at bootstrap (the loss
mechanism itself worked: health 503 -> 200, oracle matched, gate
exit 0, negative control PASS). Fixed (both runners): 30 s lead.
BUG 11 (redis-load): run_fault_redis_load read users.json as a bare
list (users[20]) but it is a dict {scope, staff, users} — KeyError
aborted the suite (recorded as manifest failure 'KeyError'). Fixed:
users['users'][20]. The 30 s lead was also applied to redis-load.
WORKER-FAILOVER precondition failure: the unpaid-order-expiry job
entered a sustained deadline_exceeded mode at 01:56:37 (before the b4
order was expirable), 0 expiries committed, backlog 61 — a death
spiral (failed candidates are retried, not dropped, so the backlog
only grows). App/VM finding, not a harness bug; the b4 trigger
aggravated it after 02:02:23 but did not start it. Run 4 runs
worker-failover first on a fresh DB, b4 last.

## CAP-02 verification runs 3–10 — fault matrix proven; bugs 9–15 fixed (2026-11-21)

Run 3 (nonce 1062d3b72990574d): b1/b2/b3 PASSED (replay exactly once,
gate PASS, negative control FAIL -> restore PASS). b4/db-loss/
worker-failover FAILED; redis-load never ran. Bugs 9 (b4 pg_sleep(10)
> 300 ms per-candidate timeout), 10 (db-loss 10 s lead < ~15 s
bootstrap), 11 (redis-load users.json is a dict, KeyError) found.
Run 4 (f440940cc2138671): ABORTED at worker-failover's un-release
DELETE. Bug 12 (ledger append-only triggers block the DELETE).
Run 5 (421ed02686223277): worker-failover FAILED (release batch never
started); db-loss + redis-load FAILED although every assertion was
TRUE; b4 FAILED (expiry job death spiral from the load backlog).
Bugs 13 (oracle_gate string compared to 'matched') + 14 (un-release
did not reset referral_commission_released) found. Redis-load's
refund path proven (409 inventory_hold_expired, 1 obligation).
Run 6 (7c2758391e8bf5c3): ABORTED by a VM/runtime restart at ~03:08
UTC (mid-b4 wait; /tmp wiped, libpq5/libllvm17t64/nginx/postgres
user removed). Environment rebuilt (user 103/105, o+x, libpq5 16.15,
libllvm17t64 1:17.0.6, nginx 1.24.0, PG 16.15 on 5433); orphaned DB
dropped. Before the restart the expiry job was healthy and the
fixture cohort was 100/100.
Run 7 (779fb38e62d37e4d): db-loss PASSED, redis-load PASSED. b4 +
worker-failover FAILED (fixture ~7200-order expired backlog starves
newly-expired orders; the job processes ~21 per 5 s batch). Fix:
queue-jump (test orders' payment_expires_at -> '2000-01-01', first
in the expiry queue). b4 deviates from the audit's real 15-min hold
wait — the hold DURATION is not what b4 tests.
Run 8 (a7282eb4e83641aa): worker-failover PASSED (expiry 60/60,
release 20/20 paired, lease takeover), db-loss PASSED, redis-load
PASSED. b4's mechanism worked (PgSleep observed, expired, replay 0,
gate 0) but the negative control picked a paid order with 0 receipts
(0 -> 1 passes check 1). Bug 15 (plant requires exactly 1 receipt).
Run 9 (c8626551691a229d, b4 only): mechanism worked again, but the
plant found no target — the fixture's 800 paid orders all have 0
receipts/verifications, so the negative control needs a prior live
payment in the same run. Fix: run b4 after b1.
Run 10 (0f5c7c2049c146ce, b1 + b4): SUITE EXIT 0. b1 PASSED, b4
PASSED (kill mid-batch observed, expired, replay 0, gate 0,
negative control FAIL -> restore PASS, planted against the b1
order). Fixture cohort 100/100 (sixth 100/100; runs 2 and 9 were
99/100 — the 100th candidate is timing-marginal).
FINAL: all seven fault profiles PROVEN at diagnostic scale (b1/b2/
b3/b4, db-loss, worker-failover, redis-load), each with a passing
run AND a failing negative control. The 100-RPS executions remain
gated on D-1. Docker ports are written but UNPROVEN.
App/VM finding: the expiry and release jobs both enter a
deadline_exceeded / job_failed death spiral when their candidate
backlog exceeds what a 3 s / 10 s batch can clear on this VM;
failed candidates are retried, not dropped, so the backlog only
grows. A real capacity/robustness signal for D-1.
