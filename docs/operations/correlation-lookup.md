# Correlation-ID lookup (OBS-01 D-03 / D-14)

Companion view to the "Correlation ID lookup" panel on the
"Sellersho correctness and capacity" Grafana dashboard
(`ops/observability/dashboard.json`, `request_id` variable).

## What a request ID is

Every HTTP response carries a fresh server-generated 128-bit request ID
as `X-Request-ID` (incoming IDs are ignored). The same ID is carried in
request context into SQL operations, provider calls and worker runs, and
appears in the backend's `operation request_id=...` log lines and in the
nginx telemetry log's `backend_request_id` field (API calls only).

## The lookup (today)

The primary lookup is the bounded in-memory ring buffer (OBS-01 G-03,
last 1000 completed requests) behind the admin API — it lands with the
ring-buffer backend change, in the same deploy as the admin-panel lookup
view (Admin panel → پایش عملیات خودکار → جست‌وجوی درخواست با شناسه).
Grafana panel 21 ("Correlation ID lookup") cannot query this endpoint —
Grafana has no datasource that can call an authenticated admin API — so
the panel links here instead of pretending to query.

```bash
# Admin token required (same admin auth as the operations inbox).
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://127.0.0.1:8080/api/v1/admin/observability/requests?request_id=<request-id>"
# 200 example:
# {"request_id":"...","method":"GET","route":"/api/v1/products/{id}","class":"api",
#  "opclass":"read","status":200,"outcome":"completed","duration_ms":412,
#  "started_at":"2026-11-18T14:00:00Z",
#  "spans":[{"component":"sql","operation":"query","outcome":"success","duration_ms":380}],
#  "dropped_spans":0}
# 404 (plain message): the ID is not in the ring — the ring holds only the
# last 1000 requests and is wiped on backend restart. A 404 means the
# request aged out of the ring, not that it never happened, and the
# response never fabricates a breakdown.
```

**Redaction:** a ring record stores ONLY `request_id`, `method`, the
bounded ServeMux route pattern, `class`, `opclass`, `status`,
`outcome`, `duration`, `started_at`, and per-span
`component`/`operation`/`outcome`/`duration`. It NEVER stores headers,
bodies, tokens, query strings, URLs, SQL text, command arguments, or
any PII / business identifier (same value-free rule as the metrics
registry; see the comment at the top of
`backend/internal/observability/ring.go`).

**Retention bounds (explicit):**

| Bound | Value | Effect |
|---|---|---|
| Completed ring | 1000 records | Hard cap; oldest evicted on insert |
| Spans per record | 64 (+ dropped count) | One request cannot bloat the ring |
| In-flight map | ~70 entries (admission slots) + 10-min age eviction | Bounded by construction |
| Memory, worst case | ~1000 × (record ~300 B + 64 spans × ~120 B) ≈ **8 MB** | Negligible next to the heap gauge |
| Memory, typical | A handful of spans per request | Well under 1 MB |
| Time retention | **Traffic-dependent — see measured rates below** | Lookup works only for recent requests |
| Process restart | Ring is wiped | A restart loses the window; the dashboard panels and Prometheus series are unaffected (they are aggregates, not the ring) |

**Measured prod traffic (read-only probe, 2026-11-18 ~17:45 Tehran /
14:15 UTC):** 131 API calls carrying a real backend request ID in the
~72 minutes since the frontend container (re)started at 13:03 UTC (0
in the last 10 minutes; 185 total edge log lines in the same window).
That is ~0.03 API req/s (~2/min), so at current idle traffic the
last-1000 ring covers roughly **6–9 hours**. The window is
traffic-dependent, not a duration guarantee: at 1 req/s it is ~17
minutes, at 10 req/s ~100 seconds, and during a load test it collapses
to minutes. (The 24 h count equals the 2 h count only because the
container log starts at the container start — no older lines exist to
count; the rate above is measured over the 72 minutes the log covers.)

**Lookup-view owner:** the admin-panel lookup box (Admin panel →
پایش عملیات خودکار → جست‌وجوی درخواست با شناسه) is owned by the
**OBS-01 platform worker (parallel track)**; the endpoint + this
documented curl is the minimum viable companion view, and Grafana
panel 21 stays a link to this doc, not a query.

## The lookup (fallback: stdout grep)

Grafana has no log store yet, so the lookup runs against the container
stdout logs on the prod host. Paste the ID into the dashboard's
**Request ID** variable, then on the host:

```bash
# 1. Backend spans: every SQL / provider / worker operation for the request
docker logs drup_backend --since 2h 2>&1 | grep '<request-id>'
#    lines look like:
#    operation request_id=<id> component=sql operation=query outcome=success duration_ms=12
#    operation request_id=<id> component=worker operation=expiry outcome=success duration_ms=1

# 2. Edge telemetry line for the same request (API calls only)
docker logs drup_frontend --since 2h 2>&1 | grep '<request-id>'
#    {"class":"api","status":200,"duration_seconds":0.012,...,"backend_request_id":"<id>"}
```

`docker logs` defaults to the whole container log; use `--since` to bound
the search (see retention below).

## Retention mechanism — defined, and its gap stated honestly

**Store today:** Docker's default `json-file` logging driver on the prod
host — i.e. the container stdout/stderr streams, kept as files under
Docker's data directory. There is NO rotation configured for these
containers (no `max-size` / `max-file` in any compose file), so retention
is effectively "until the host disk fills" — which, with prod disk at 99%
(310M free on 2026-11-18), is both unbounded growth and a live disk risk
in itself. This is the gap D-14 names: the threshold-gated stdout record
has no defined retention, rotation, or aggregation.

**What supersedes it (named follow-up, not built here):** a Loki +
Promtail log stack on the prod host, scraping the same container stdout,
with 7-day retention and a `request_id` label/parsed field, wired as a
Grafana logs datasource so the dashboard lookup returns rows instead of
grep commands. Owning task: "OBS-01 follow-up — queryable log store
(Loki)". Until it lands, queryable retention is the ring buffer only
(last 1000 requests — ~6–9 hours at current idle prod traffic,
minutes under load; wiped on restart) — durable 7-day retention does
not exist yet. The Loki follow-up's owner date is to be set by the
coordinator; no date is set here.

**Immediate mitigation (do with the next deploy, no new service):** add
`logging: {driver: json-file, options: {max-size: "10m", max-file: "3"}}`
to the backend/frontend/edge services so container logs are bounded at
~30M per container instead of growing without limit. Not added in this
change because it alters production logging behaviour and belongs in the
same deploy review as the collector stack.

## Per-run correlation appendix (CAP-02 W-2.2)

The spec's correlation — route latency ↔ SQL / locks / pool / Redis /
edge / provider / CPU / memory — is automated by
`scripts/cap02_correlate.py`. It reads a runner output directory
(`manifest.json` + `<profile>-metrics-before.prom` /
`<profile>-metrics-after.prom` + `<profile>-summary.json`) and emits a
per-profile appendix (JSON + readable text) joining:

- driver route p95/p99 (from the summary) with backend-received
  per-route status counts (from the `/metrics` histogram deltas);
- provider overloads (`sellersho_operation_duration_seconds`,
  component=provider, outcome=overloaded — the IPPanel shedder);
- SQL operation counts/seconds by outcome, pool waits
  (`sellersho_db_wait_total` / `sellersho_db_wait_seconds_total`),
  PostgreSQL transactions/deadlocks, Redis pool misses/timeouts;
- worker backlogs after the profile, process CPU seconds and heap,
  manifest CPU-max and received counts.

```bash
python3 scripts/cap02_correlate.py --run-dir <run-output-dir> [--out <dir>]
# optional pg_stat_statements dump (TSV: query, calls, mean_ms, total_ms, rows)
python3 scripts/cap02_correlate.py --run-dir <run-output-dir> --pgstat <file>
```

No Prometheus scrape is required: the runner snapshots `/metrics`
itself per profile, so the collector works while the prod Prometheus
backend scrape is DOWN (PERF-03 C2 — the 9090 publish fix rides the
REL-02 deploy path; see `docs/operations/capacity-evidence.txt` §4).
Without `--pgstat` the SQL section is limited to the `/metrics`
operation counters and says so — statement text is never invented.

**Proof (audit §8 W-2.2 gate — PASSED 2026-11-21):** the collector
reproduces the known CAP-01 OTP-shedder finding from raw data before
being trusted on new runs:

```bash
python3 scripts/cap02_correlate.py \
  --run-dir ~/workspace/cap01-native-runs/sellersho-cap-report-f12c4cf62694 \
  --proof otp-shedder
# PROOF otp-shedder: MATCH
#   metrics_otp_send_200: 486, metrics_otp_send_503: 334,
#   metrics_provider_overloaded: 334, shed_rate: 0.4073
#   (expected 486 / 334 / 0.407 — the 0587 clean-run numbers)
```

The 334×503s are the in-app 8/s IPPanel dependency-budget shedder
(PERF-03 audit: 487 OK / 333 shed = 40.6% predicted vs 486/334 =
40.7% observed), visible three ways in the same window: driver
summary, `/metrics` http counts, and the provider `overloaded`
operation counter — all three agree at 334.

## Limits of the lookup

1. **The ring is recent-only.** It holds the last 1000 completed
   requests — roughly 6–9 hours at current idle prod traffic
   (~0.03 API req/s, measured 2026-11-18), minutes at 1–10 req/s or
   under a load test — and is wiped on backend restart.
   The stdout-grep fallback below is older still: the backend logs an
   `http request_id=...` line only for status >= 500 or duration >=
   300ms (`backend/internal/observability/metrics.go`), so a fast
   request's route, status and duration are NOT recoverable from logs —
   only its SQL/provider operation spans are. Use the ring lookup first;
   grep is spans-only history.
2. **Provider spans depend on D-09.** Outbound provider calls carry the
   request ID in their log lines, but header propagation to the provider
   (Tapin/IPPanel) is backend-worker scope (D-09); until it lands, the
   provider's own records cannot be joined by this ID.
3. **Worker delay is per-job explainable in metrics.** All six jobs have
   last-success/backlog/oldest-age gauges (dashboard panels 22–23, D-04);
   the `operation` log lines and `operations_job_runs` rows are the backup
   path.
4. **Log lines have no aggregation.** Grep is the only query path; there
   is no sampling, no per-route rollup, and no alerting on log content.
