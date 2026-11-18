# OBS-01 infrastructure checkpoint

Metrics listen on literal loopback 127.0.0.1:9090 separately from public routes.
Do not publish that port publicly or proxy it through Caddy/nginx. The collector
reaches it (and Caddy :2019/metrics and nginx :8081/status) through
loopback-only publishes on the host (127.0.0.1:9090, 127.0.0.1:2019,
127.0.0.1:8081) — the local tunnel this checkpoint allows, because the three
endpoints live in three different network namespaces and no single shared
namespace can reach all of them. Collector wiring is declared in
`ops/observability/prometheus.yml` (15 s scrape) and
`docker-compose.observability.yml`
(Prometheus, Grafana, node_exporter, nginx-prometheus-exporter — all host
networking, all loopback-bound); `scripts/deploy_remote.py` starts the stack
in its observability step and imports `ops/observability/dashboard.json`
(Grafana provisioning) and `ops/observability/alerts.yml` (Prometheus
rule_files, loaded at container startup — the deploy force-recreates
Prometheus, so there is no `/-/reload` step). No pprof.
The collector lives in its own compose file on purpose (C-02):
`docker-compose.prod.yml` is included in EVERY deploy and in rollback, and
Grafana's `${GRAFANA_ADMIN_PASSWORD:?}` fail-fast used to sit in it, so an
unset password aborted the next deploy of any kind. The observability file
is included ONLY by the collector step, so an unset password blocks only
the collector deploy, never an app deploy or a rollback. Set
`GRAFANA_ADMIN_PASSWORD` in `/opt/drup/.env` (mode 0600, server-only)
before the first collector deploy; there is deliberately no default
password. `--web.enable-lifecycle` was removed from the Prometheus
command (C-09): it exposed POST `/-/quit` to host loopback, and its only
consumer — the deploy's `/-/reload` — was redundant because the container
is force-recreated. Do not re-add lifecycle without a written reason.
TSDB retention is explicitly bounded (3 days / 128 MB) because prod disk was
at 99% on 2026-11-18; revisit only after the disk-pressure incident clears. SLO buckets include 300ms, 750ms, 1s and 2s.
API, backend static and internal domain-authorization requests are distinct.
Caddy native handler metrics are enabled without per-host labels and remain
on its default private loopback admin endpoint (localhost:2019/metrics). No
public metrics route is configured. Nginx exposes stub_status only on
127.0.0.1:8081/status and writes redacted JSON counts/bytes/durations by bounded
api/upload/asset/page classes; backend request IDs correlate API calls.
No path, query, client IP, cookies, headers or browser identity enters that log.
A collector scrapes each service loopback endpoint (see above); the Caddy
admin endpoint and nginx stub_status are loopback-only publishes, never public.
Edge load/TLS behavior still needs EDGE-01 (no caddy_tls_* series exists in
Caddy metrics, verified live 2026-11-18; TLS failures are unmeasured).

Route labels use registered ServeMux patterns with collapsed subtree suffixes.
Unknown routes, methods and operation names collapse. Queries, headers, request
bodies, Redis keys/arguments/results, payment data and SQL values are excluded.
A fresh cryptographic 128-bit request ID is returned as X-Request-ID and carried
in request context. Incoming IDs are ignored. IDs never become metric labels.
Slow/error HTTP logs share that ID. LogOperation supplies bounded operation logs.

Redis hooks record command/Lua/pipeline timing and outcomes. Pool counts and
PostgreSQL active/idle connections/lock waiters, cumulative deadlocks and transactions sample
every 15 seconds with a 2s deadline. sql.DB Stats exposes pool waits and gauges.
The lib/pq connector instruments query/exec and prepared paths with request
correlation and SQL/argument redaction. Query duration includes Rows iteration
until EOF/Close/error, once. Serializability, commit/rollback, query cancellation
and pool recovery are checked on migrated PG. Runtime heap/GC, goroutines and
Linux process CPU/RSS/open descriptors/socket descriptors are exported. Linux root filesystem free ratio is sampled locally;
other OSs report unknown rather than a fabricated healthy value.

The approved operator channel is the existing admin panel notification inbox.
Incident state and admin/super-admin outbox inserts commit atomically. Persistent
state deduplicates repeated alerts across restarts. Recovery rearms an incident,
with at most one notification per code/admin per 15 minutes. A suppressed new
incident retains durable delivery_pending state and is delivered when the
cooldown expires, even if the failure persists throughout that interval.
Multiple incidents within that cooldown coalesce into one pending notification.
Disk pressure is the exception to one-notification-per-incident: a still-active
disk incident re-notifies when free space worsens into a lower band (15% / 10% /
5% / 2% free; see `severity_band` on `observability_alert_state` and
`diskPressureBand` in `backend/internal/observability/alerts.go`). Same-band
worsening does not re-notify; worsening inside the 15-minute cooldown is
remembered as durable pending state and delivered when the cooldown expires;
recovery resets the band to 0, so the next incident starts from band 1 again.
This is deliberate: the live disk incident (active since Nov 14, last notified
Nov 14 while disk fell to 99%) went silent exactly because the old rule
notified once per incident until recovery — a worsening condition must not
go silent. A DB outage queues bounded incident codes in process memory for retry after DB
recovery; a simultaneous process loss can lose this pending signal. Guaranteed
notification during database outage needs an independent collector/storage path.
No external messages or webhooks are used.

The poller evaluates API error ratio, dependency availability, expiry-job stale
history, known disk pressure, known backup staleness, and the latency SLO.
The SLO breach is computed per class from the HTTP histogram series the
backend already emits, over each 15 s poll window: read p95 > 300 ms or
p99 > 1000 ms, mutation p95 > 750 ms or p99 > 2000 ms (the agreed CAP-01
SLO, same thresholds as the Prometheus rules). Two deliberate differences
from the Prometheus rules: the poll window is 15 s, not 5 minutes, and a
class needs at least 10 requests in the window before its quantiles count
(a single slow request must not trip the SLO alert); below that floor the
class is unknown, never a fabricated healthy value. The first tick only
baselines the histogram, so boot-time cumulative history never trips it.
BAK-01 must publish
backup_last_success_timestamp_seconds through the trusted internal collector API;
absence still makes the Prometheus SellershoBackupStale rule fire — visible in
the Prometheus UI / dashboard only, it reaches no operator on its own (see the
delivery section below). Unknown backup state is not claimed healthy by
application telemetry.

## Prometheus rules are visualization, not operator alerts (C-01)

State this plainly, because the shipment used to imply otherwise: the seven
Prometheus rules in `ops/observability/alerts.yml` have NO delivery path.
There is no Alertmanager anywhere in the stack, no `alerting:` section in
`prometheus.yml`, and no Grafana contact point or notification policy in the
provisioning. A firing Prometheus rule reaches no operator on its own — an
operator would have to open the Prometheus UI (loopback-only, via SSH tunnel)
and notice it. `promtool check rules` / `promtool test rules` prove the PromQL
parses and fires on synthetic series; they prove nothing about delivery. Do
not write or say that these rules "alert the operator" — they do not.

What DOES reach the operator is the in-app poller only
(`PollDependencies` -> `DeliverAlerts` -> admin-panel notification inbox,
above), which covers six conditions: API errors, dependency loss, expiry-job
stall, disk pressure, backup staleness, and latency-SLO breach. The SLO
breach is mirrored from the Prometheus rules with the same thresholds
(`slo_breach` in `backend/internal/observability/alerts.go`, computed by
`sloFromHTTPDelta` in `dependencies.go` from the HTTP histogram series;
see the poller paragraph above for the 15 s window and 10-sample floor).
The Prometheus disk rule and backup-stale rule duplicate conditions the
poller already delivers, but their Prometheus copies are unseen unless
someone looks at the UI.

If a real Prometheus delivery route is ever wanted, it needs an explicit
owner decision — it cannot be the admin inbox via prod PostgreSQL for the
DB-outage case — and must be named in this doc. Never wire one quietly, and
never let a close-out list "7 alert rules" as if seven notifications exist.

Executed guarded local acceptance: 1000 arbitrary route IDs/secret inputs bounded
and redacted; unsafe metrics bind refused; panic accounting; Redis Lua argument
redaction; real migrated PG pool/lock/history collection; six alert failure
conditions produce six durable admin-only rows, repeated evaluation deduplicates
and recovery rearms (disk pressure additionally re-notifies on worsening into
a lower band); actual authorized admin read/unread handlers see all six,
customer access returns 403. Every DB test uses internal/testdb and the Docker
runner verifies labeled-container cleanup. Commands:

    python scripts/test-isolated.py -- go test ./internal/observability -count=1 -v
    python scripts/test-isolated.py -- go test ./cmd/server -run '^TestOperationsAlertsReachProtectedAdminInbox$' -count=1 -v

Actual gateway verification is decorated at startup and preserves correlation.
Operations workers generate IDs shared by their SQL operations and outcome logs.
Unpaid-order expiry publishes last success, backlog and oldest overdue age.

## Correlation-ID lookup (ring buffer, G-03)

The backend keeps the last 1000 completed HTTP requests in a bounded
in-memory ring, with operation spans (SQL / provider / Redis / worker;
64 spans max per record, dropped spans counted), served by
`GET /api/v1/admin/observability/requests?request_id=<id>` behind the
existing admin auth. A lookup that misses returns a plain 404 — the
ring holds only recent requests and is wiped on backend restart. Full
endpoint, curl, example response, retention bounds (measured prod
traffic ~0.03 API req/s on 2026-11-18, so ~6–9 hours at current idle
traffic, minutes under load), and the redaction rule (route/status/
duration/spans only — never headers, bodies, tokens, query strings or
PII) are in `docs/operations/correlation-lookup.md`. The admin-panel
lookup view is owned by the OBS-01 platform worker (parallel track);
Loki stays the follow-up for durable 7-day retention, with an owner
date to be set by the coordinator.

Remaining OBS-01 acceptance: measured frontend counts aggregated anywhere
(browser-local RUM exists in `src/utils/webVitals.ts` — window + console
only — until a backend ingest endpoint lands); queue/admission rejects
(the backend now emits `sellersho_http_rejects_total` /
`sellersho_admission_inflight`, but no dashboard panel or gate run covers
them yet); backup signal integration (the poller reads `backup_signals`
and the publisher script exists — first verified publish still to be
proven on a disposable stack, G-06); durable 7-day retention for
correlation-ID lookup — the ring covers recent requests only
(see `docs/operations/correlation-lookup.md`);
independent review. Host CPU/memory/disk monitoring, per-job worker
gauges, edge collection (Caddy + nginx series), SLO reporting (dashboard
panels + Prometheus rules — visualization only, see the delivery section
above, NOT operator alerts), and promtool fixtures are wired by this
change. This checkpoint does not close OBS-01 or prove production readiness.

## Disk pruning proposal — DOCUMENT ONLY, NOT implemented (C-03/C-12)

Nothing in this section deletes anything or adds an auto-delete step.
Deleting prod files needs the owner's explicit scope; this is the proposal
for that decision, written from the critic's 2026-11-18 prod probe
(disk 99%, 307M free on a 25G volume).

What is on the disk (probe, 2026-11-18 ~17:30 Tehran):
- `/opt/drup/releases` — 71 release dirs, ~115–121MB each, 4.3G total.
  Nothing ever prunes them; every deploy adds one (~120MB) plus a
  deploy-backup dir (~98–104MB), so every deploy costs ~220MB.
- `/opt/drup/backups` — 5.7G total (deploy-backup dirs ~98–104MB each,
  plus the nightly `backup-auto.sh` 7-day `auto/` rotation, which IS
  pruned already — the 7-day rotation covers `auto/` only).
- `/opt/drup/backend` 1.4G; `/var/lib/docker` 1.8G.
- One-off Oct 29 files still on disk, all superseded: a 42MB tarball, six
  ~29MB `.sql` dumps, three ~19MB `server-linux.bak-*` binaries.
- The collector images themselves are ~324MB compressed (prometheus
  123.8MB + grafana 182.2MB + node-exporter 13.0MB + nginx-exporter
  5.1MB), already more than the 307M free — do NOT attempt the collector
  deploy until free space is verified >= ~1.5GB (C-03).

Proposed keep-last-N rule (for the owner to approve, then implement as a
deploy step — never as a silent background deletion):
- Keep the last 3 release dirs in `/opt/drup/releases` (the running one +
  two rollback spares) and the deploy-backup dirs that belong to those
  same 3 commits; delete older release dirs and their deploy-backups.
  Expected recovery: ~68 dirs x ~118MB ≈ 8G — except the backups side
  is shared, so the honest estimate is ~4G from releases plus whatever
  old deploy-backups are dropped with them.
- Keep the nightly `auto/` 7-day rotation as it is.
- Delete the Oct 29 one-off files (tarball, six `.sql` dumps, three
  `server-linux.bak-*`) only after confirming each is superseded by a
  later backup/release — list them by name in the approval, do not
  delete by glob.
- Never delete: the running release, `.deployed-commit`, `.env`,
  `/opt/drup/backups` `auto/` dirs inside their 7-day window, or the
  newest deploy-backup.
- After any approved cleanup, re-probe `df` before the collector deploy,
  and add a pre-pull free-space guard to `deploy_observability()` (refuse
  the image pull below a threshold) so a future near-full disk fails
  loudly BEFORE the app deploy instead of after it. The guard is proposed
  here, not implemented — it is C-03's item, not this fix's.

Edge syntax/privacy verification: scripts/test_observability_config.py PASS2;
local network-disabled nginx -t and Caddy adapt --validate PASS with repository
config mounted read-only. Versions are existing local images, not release pins.
Primary documentation: https://caddyserver.com/docs/metrics and
https://nginx.org/en/docs/http/ngx_http_stub_status_module.html.

Independent checkpoint reviewer: review_obs_checkpoint. Found cooldown delivery
could be suppressed forever; guarded regression first failed with count5 rather
than10. Durable pending state fixed it; final full9-test OBS suite PASS2.306s.
Raw local logs: logs/obs01-tests.log, obs01-alert-review-red.log,
obs01-admin.log, obs01-worker.log and obs01-caddy.log (ignored).
