# OBS-01 infrastructure checkpoint

Metrics listen on literal loopback 127.0.0.1:9090 separately from public routes.
Do not publish that port or proxy it through Caddy/nginx. A collector must share
the backend network namespace or use a local authenticated tunnel. No pprof.
Import ops/observability/dashboard.json and alerts.yml into Grafana/Prometheus.
Scrape every 15 seconds. SLO buckets include 300ms, 750ms, 1s and 2s.
API, backend static and internal domain-authorization requests are distinct.
Caddy native handler metrics are enabled without per-host labels and remain
on its default private loopback admin endpoint (localhost:2019/metrics). No
public metrics route is configured. Nginx exposes stub_status only on
127.0.0.1:8081/status and writes redacted JSON counts/bytes/durations by bounded
api/upload/asset/page classes; backend request IDs correlate API calls.
No path, query, client IP, cookies, headers or browser identity enters that log.
A collector must scrape each service network namespace; namespace access is
not configured by this checkpoint. Edge load/TLS behavior still needs EDGE-01.

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
A DB outage queues bounded incident codes in process memory for retry after DB
recovery; a simultaneous process loss can lose this pending signal. Guaranteed
notification during database outage needs an independent collector/storage path.
No external messages or webhooks are used.

The poller evaluates API error ratio, dependency availability, expiry-job stale
history, known disk pressure and known backup staleness. BAK-01 must publish
backup_last_success_timestamp_seconds through the trusted internal collector API;
absence remains an alert in the external Prometheus rules. Unknown backup state
is not claimed healthy by application telemetry.

Executed guarded local acceptance: 1000 arbitrary route IDs/secret inputs bounded
and redacted; unsafe metrics bind refused; panic accounting; Redis Lua argument
redaction; real migrated PG pool/lock/history collection; five alert failure
conditions produce five durable admin-only rows, repeated evaluation deduplicates
and recovery rearms; actual authorized admin read/unread handlers see all five,
customer access returns 403. Every DB test uses internal/testdb and the Docker
runner verifies labeled-container cleanup. Commands:

    python scripts/test-isolated.py -- go test ./internal/observability -count=1 -v
    python scripts/test-isolated.py -- go test ./cmd/server -run '^TestOperationsAlertsReachProtectedAdminInbox$' -count=1 -v

Actual gateway verification is decorated at startup and preserves correlation.
Operations workers generate IDs shared by their SQL operations and outcome logs.
Unpaid-order expiry publishes last success, backlog and oldest overdue age.

Remaining OBS-01 acceptance: all other worker backlog/oldest age/reconciliation success;
edge TLS/asset telemetry collection and measured frontend counts; queue/admission rejects; host CPU/RSS/connection metrics; backup signal integration; promtool fixtures; independent
review. This checkpoint does not close OBS-01 or prove production readiness.

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
