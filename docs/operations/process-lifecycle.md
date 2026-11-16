# Backend process lifecycle

Required configuration, PostgreSQL, migrations, ownership bootstrap and Redis
must succeed before the HTTP listener is admitted and scheduled work starts.
Failures exit nonzero with field/dependency diagnostics and withheld connection
details. PostgreSQL and Redis initialization each have a maximum three seconds
inside the overall startup deadline. Migration and bootstrap SQL inherit that
deadline and signal cancellation. No schema changes accompany this release.

| Setting | Default | Accepted integer seconds |
| --- | --- | --- |
| `STARTUP_TIMEOUT_SECONDS` | 30 | 1–600 |
| `SHUTDOWN_GRACE_SECONDS` | 20 | 1–25 |

Both supported backend compose definitions and the release runtime override use
`stop_grace_period: 30s`. The runtime override matters because production retains
its existing base compose files and credentials. Deployment rollback retains the
previous runtime override bytes. Longer application grace requires coordinating
the supervisor grace first.

`GET /api/live` reports process liveness without SQL or Redis work and stays 200
during drain. `GET /api/ready` and legacy `GET /api/health` report schema/startup,
dependency connectivity and drain state, returning 503 when unavailable. Legacy
health keeps `status`, `engine`, `environment`, `isDev`, `postgres`, `redis` and
`timestamp`. Dependency-down 503 was already supported. Each health probe has a
500 ms total budget: PostgreSQL 250 ms, then Redis 250 ms. Only one probe sequence
runs at once; contending requests return 503 immediately. Probes include SQL
connection-pool waiting. Health admission is independent of ordinary API slots.
These endpoints do not certify provider availability or payment eligibility.

SIGTERM or interrupt first makes readiness false and marks mutation admission
draining. New POST/PUT/PATCH/DELETE requests reaching admission receive
503 `service_draining`, `Retry-After: 1`, and a retryable response. GET/HEAD/OPTIONS
remain safe until the listener closes. Already admitted requests retain their
contexts during HTTP drain; the signal context is deliberately separate.

One overall deadline bounds teardown. HTTP gets at most 14 seconds, worker
cancellation/wait at most five seconds, and final cleanup at most one second.
For grace shorter than 20 seconds these phases scale to 70%, 25%, 5%. All phases
also inherit the overall deadline. HTTP is force-closed after its phase, which
cancels remaining request contexts. Workers stop admission, cancel business work
and wait before metrics and SQL/Redis cleanup. Metrics polling/listener stop before
dependency closers; every dependency close is initiated even when another blocks.
Startup and bind failures share idempotent cleanup. A deadline overrun exits with
a bounded diagnostic; the supervisor retains at least five seconds beyond the
maximum supported application grace.

Clients should preserve checkout idempotency keys across retries and recover the
stored checkout attempt after a lost response. A drain rejection precedes handler
execution. A forced close may leave the client uncertain whether accepted work
committed; recovery and replay use the existing durable checkout authority.

| Request class | Handler budget | Maximum body |
| --- | --- | --- |
| Ordinary API | 8 s | 2 MiB |
| POST `/api/v1/upload/product-image` | 60 s | 10 MiB |
| POST supplier product or variant image collection | 60 s | 48 MiB |
| Domain permission | 2 s | 2 MiB |
| Static | 20 s | 2 MiB |

Upload classification uses exact existing routes, including method and path,
and never Content-Type alone. Stricter handler checks remain in force. Known
oversize bodies are rejected with 413 before handler execution. Streamed bodies
are fully read through the same limit inside existing admission slots and socket
deadlines before handlers run, preventing a first-value JSON decoder from
committing before a later body-limit error. This uses bounded in-process buffers;
local tests do not establish production-equivalent memory capacity or 100 RPS.
Socket ceilings remain 5 s headers, 65 s read, 70 s write, 60 s idle, 64 KiB headers.

Delivery and monthly worker compatibility entrypoints share Runner admission and
leases. Stop also cancels manual work admitted before scheduling begins.
`StopContext` accepts the process deadline; legacy `Stop` retains its five-second
wrapper. Delivery confirmation retains its guarded transaction setting read and
20-day rule. Active financial-review markers remain visible and are excluded from
the automatic-retry subset even if both marker types coexist.

The owned-fixture checks are `scripts/test-suite-isolated.py` for selected Go tests
and `scripts/test-process-lifecycle.py` for actual Linux SIGTERM with SQL-blocked
HTTP read/checkout, pre-Start manual job cancellation, restart replay and unchanged
ledger/payment/movement/stock snapshots. The latter compiles only `cmd/server`,
copies a test binary and sealed migrations into a label/ID-verified disposable PG
container, and removes that exact container. `--baseline-red` reproduces the former
server.Serve path without signal drain and intentionally fails its admitted-request
assertion. No production fault injection or real provider is involved.
