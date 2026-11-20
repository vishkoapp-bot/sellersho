# Local failed baseline evidence

These results are local diagnostics, never CAP-02 certification. No production
service, provider or intended production hardware was targeted. Every run used a
fresh marked disposable database and internal Docker network; cleanup was verified.
Existing failures remain failures after harness fixes.

## Measurement definitions and environment

All latency values below are milliseconds. CPU is Docker CPU percentage: 100%
represents one logical CPU, so PostgreSQL can exceed 100%. The host exposed eight
logical CPUs (Intel Family 6 Model 154), Windows 11 build26200, Docker29.7.2 on WSL2
kernel6.18.58.1, Docker memory6,072,987,648bytes. Disk class and interference from
shared developer-host workloads were not measured. These are material limitations.
One backend replica, SQL pool max-open25/max-idle10; PostgreSQL16.15, Redis7.4.11,
nginx1.31.7, Caddy2.11.9, Node24 container. Exact image IDs, binary/source/migration
digests and version outputs are in each raw manifest. Installed images are not
claimed to be release-patched. Snapshots include uncommitted source inputs.

`offered` counts scheduled workload arrivals; `requests` counts actual API attempts.
OTP is two API calls per successful identity (send and verify). Private OTP-double
reads are helper calls, not application API calls. Caddy domain-authorization
requests are separate internal calls. Backend Prometheus duration-count deltas
independently reconcile received API/domain calls, including declared preparation
and recovery calls. Dropped arrivals are failures even if never sent. Timeout
status0 is an unsuccessful client outcome, not evidence that the server rolled
back. Durable stock and payment checks therefore follow ambiguous outcomes.

Connections use trusted local CA verification and an HTTPS keep-alive agent;
principal state is independent, not one socket per identity. No warmup was applied
to the short diagnostics. New-connection TLS and browser assets require separate
profiles. Backend runs detached with bounded Docker logs; no undrained native
stdout/stderr pipe. Generator and backend metrics are collected separately.

## Retained results

| Profile / raw report suffix | Observed result | Verdict |
| --- | --- | --- |
| Signed-auth baseline `2e8f4f359a8b` |1000 calls/identities, zero drops/errors; p95=1330,p99=1755; generator41.81%,backend73.57%,PG68.60% single sample | FAILED read gate; original exit0 was a verdict bug, fixed without changing thresholds |
| Trusted TLS health `67c75b87603c` |1000 calls,zero drops/errors;p95=38.6,p99=182.9 | PASS health only |
| OTP+browse,200 worker bound `459ef1884f69` |6000 offered,2794 actual API/domain calls,206 successful identities,2732 dropped flow/browse iterations,526 OTP helper reads,560 SMS sends | FAILED; worker occupancy is not proof of generator causality |
| OTP+browse,1000 worker bound `40ef8d6c8ea4` |6000 offered,5911 actual API/domain calls,zero drops,233 successful identities;4000 browse timeouts;1000 send calls/89 timeouts,911 verify calls/678 timeouts | FAILED; source/data also changed, so not a controlled worker-only comparison |
| Actual OTP only `79d08f864717` |1000 successful identities,2000 API/domain calls,1000 helper reads/SMS sends;send p95/p99=31.63/32.50,verify=6.94/8.10;zero drops/errors | PASS local33.33API-RPS ramp only |
| Exact10s commerce `caffa30e8228` |1000 measured calls,240 preparation API helpers,1000 principals,zero drops;300list/200detail/150store/150account/80quote/40create/40initiate/40callback;all route gates exceeded | FAILED;1240 API/domain received counts matched |
| Rolling30s `63e398197ba4` |3000 offered,2840 measured,240 bootstrap helpers,160 missing-stage drops,120 recovery calls within budget,1000 principals,118 unresolved ambiguous states,2 confirmed payment outcomes | FAILED;3080 API/domain counts matched |
| Paced1000-user90s `f717d5dad583` |5809 API/domain calls,1000 principals,110.564s including graceful finish,52.54 actual RPS,peak523 in-flight,zero complete checkouts | FAILED;catalog timeouts prevented progression; not100RPS qualification |

The1000-worker combined run sampled generator48.75%,backend13.11%,PG676.67%.
SQL pool cumulative wait-count reached15879 and wait-duration24637.7seconds;
these are cumulative counter readings, not a per-request latency or standalone
causal proof. Before/after raw counters accompany reports. Normal OTP-only speed
and catalog-associated pressure support investigation of catalog query work.

At measurement time the driver requested `/api/v1/products?page=1..20` without a
page-size parameter; the old API ignored that page query. Detail and store routes
were `/api/v1/products/cap-product-N` and `/api/v1/stores/cap-store-N`. Thus the
catalog baseline includes old unbounded retrieval/pricing behavior. Future cursor
API changes require a separately identified run; these results are not rewritten.

The commerce oracle matched5,000,000 source units=4,999,986 sellable+14 net captured
after the exact mix; only4 callbacks had returned success. Rolling matched
5,000,000=4,999,989+11. Both matched SKU projections and zero payment/order amount
mismatches. These scoped checks do not establish full pricing/ledger/refund or
physical stock-state correctness. Historical rows in these runs were explicitly
cancelled/unpaid cardinality fixtures. Subsequent financial fixture work changes
the data shape and must be recorded separately.

Raw report directories are `%TEMP%/sellersho-cap-report-<suffix>`; summaries contain
request ordinal/principal index/status/latency, no credentials or response bodies.
CAP-02, full OBS acceptance, browser/staff/fault profiles and full financial/return
conservation remain open. Failed `.local` domain setup and unsupported customer
password-login attempts were harness setup failures, not application capacity
defects. Actual customer startup uses OTP.

## Reviewed source and bounded setup follow-ups

- Payment/carrier selector checkpoint `ff4ff13` is integrated; real/mock defaults
  remain unchanged. Native UUID origin lookup fix `8fefe13` and nullable warehouse
  geography migration `031df37` are reviewed normal source checkpoints.
- Rich-history setup `3bb4804b523a` failed before load: supplier balance took
  503.606909 seconds; old bounded EXPLAIN estimated cost 964267648 over 140,000
  lines with correlated text-cast origin scans. Safe plan: supplier-read-plan.json.
  Exact disposable query was cancelled under verified identity, not a live DB.
- Post-UUID-fix guarded fixture passed in 245.361 seconds. Five actual supplier
  reads were 1667, 1616, 1644, 1733 and 2035 ms. Individual repository samples,
  not HTTP percentiles; remaining financial latency still needs FIN013.
- Carrier fixture `42b1d7ab2a82` failed before load at missing normal geography
  columns. Additive migration0007 fixed schema; no fixture-only DDL escape.
- Internal-network port setup `41b1e9ee8fd9` failed; internal Chromium image
  attempts `e87712605871` and `38a36e03cbf7` failed dependency downloads. No
  application capacity conclusion follows; cleanup verified.
- Approved loopback raw-TLS browser fallback `ac98f2d3bd58` FAILED: Chrome
  157.0.8103.52 launched, 10 connections exchanged 20102/25260 bytes, zero
  relay refusals/errors, but zero rendered pages and zero nginx/domain/API
  requests. All 10 attempts failed before HTTP; root cause not established.
  Cleanup verified. Status-0 attempts were erroneously called network responses
  in that original summary; final classifier is corrected with a regression,
  original failure retained. All relay timings excluded from qualification.

See evidence/checkpoint-provenance.json for selected whitelisted source/binary/
migration/image/version/count metadata, without environments or credentials.

Fresh carrier/hot-SKU `3e26bb0b7c3b` (`031df37` source reference, actual snapshot
digests retained): full rich fixture PASS 337.80 seconds. Five supplier reads
1882/2187/1794/1620/1971 ms. Focused 100 principals: 115 measured calls + 205
helpers = 320 received API/domain calls, matched. Exactly five paid winners,
95 explicit stock conflicts, ten duplicate callbacks, zero wrong amounts or
unexpected failures. Source 5 = sellable 0 + captured 5; SKU 0; five paid orders;
zero payment/order amount mismatches. Carrier success path: one cached quote
provider call, zero outbound requests. FAILED latency: token p95/p99
1393.317/1430.813 ms; callback 1017.468/1017.468; duplicate 156.387/156.387.
Unchanged mutation gate 750/2000 ms. Cleanup verified; no capacity certification.

## Native short baseline suite — 2026-11-20 ~01:00 Tehran (first full native run)

Report `~/workspace/cap01-native-runs/sellersho-cap-report-500f7a0b7410`
(nonce `b90799615573458d66d8b3a4`; compact evidence
`load-tests/evidence/native-b90799615573458d66d8b3a4-compact.json`).
Disposable PG16 + scratch Redis + backend + nginx TLS on loopback;
fixture exit 0 (users 1210, suppliers 100, stores 100, products 10101,
variants 50001, orders 100000, saleable 5,000,000). Release cohort
FAILED 0/100 (separate gate; app 300ms per-candidate budget exceeded
by seq-scan-heavy eligibility — see cap01-progress.md). Verdict
certified:false by design (local preparation only; CAP-02 open).

Profiles: 3/16 PASSED (fault-provider-timeout, fault-redis-loss,
fault-restart); 13 FAILED, all labeled. ALL commerce oracles MATCHED
within stated scope (price identity, ledger sanity, refund
obligations, hot-sku, hot-account) — money conserved while
latency/throughput gates fail.

Route p95 (api-baseline): list 1089ms, detail 4369ms, store 1606ms,
account 993ms, quote 3699ms, create 5091ms (gates: reads 300ms,
writes 750ms). Journey list p95 12210ms, create 7815ms, 0 completed
checkouts. Driver ceiling on this VM: api-baseline 357/1000 requests
(643 dropped arrivals); headroom burst 20923/60000 (39077 dropped).
CPU max 64.6%–375.6% vs 70% contract. Counter gates matched except
api-rolling, journey-baseline, staff-baseline, headroom (requests
passing domain auth but not reaching backend api metrics). OTP:
otp_send 43% 503 (login-ramp) / 28% (otp-background) — app
dependency-budget overload shedder; otp_verify 100% OK. Browser:
10/10 pages HTTP 200 with leaf-SPKI pin, but all warm-render gates
failed — pages load only the 29-char shell (3183B); SPA bundle does
not render content in-window; timings excluded. session-lifecycle:
concurrent-device 10/10, re-login 10/10, old-token-after-logout
10/10 (logout does not revoke — recorded). hot-account: 100 paid, 0
cancelled (cancel p95 8630ms). cold-cache-warm: fixture_preparation
FAILED. fault_redis_loss: health 503 during loss, 200 after.
fault_restart: oracle identical before/after, converged.

Known harness defects found by this run (fixed after it, re-run
pending): drivers use obsolete `?page=N` list pagination (page 2+
→ 400 "invalid catalog page request" under the PERF-01 cursor
contract) — browse 94% errors in otp-background are that 400;
hot-sku preparation quote 409 inventory_origin_unavailable with the
runner oracle simultaneously reporting the success state (sellable 0,
captured 5, paid 5) — under investigation (hot-sku-first isolation
run in progress at time of writing).
