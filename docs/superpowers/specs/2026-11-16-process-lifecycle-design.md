# OPS-01 — explicit startup, readiness and process drain

Continue the supplied OPS-01 contract (backlog lines1615–1637) after deployed
JOB-01. Base/origin/production is52e6edce4ccd606382e08fdb4a02e9f26ca2e08e.
The user authorizes validate, commit, push and deploy.ps1 for each coherent stage.
All migrations through007 are sealed; this stage needs no new schema. Exclude
docs/fa and paused networkreward from source, tests, commits and deployment.
Preserve runtime credentials, money/stock authority, primary access and services.

## Approach and scope

Controller choice: use the existing Go HTTP server and JOB-01 worker inside the
monolith. Docker remains the supervisor. Docker-only termination cannot drain
application requests; an additional supervisor/sidecar would change unrelated
services without solving admission ownership. No new service or dependency.

Fail required startup cleanly; separate process liveness from dependency/schema
readiness; bound probes; reject new mutations during drain; finish or safely
cancel in-flight work before dependencies close. Reuse OPS-02 request budgets.
Do not change PSP readiness/payment policy, financial formulas, pool sizing,
credential rotation or historical cutover. Hardware certification remains OPEN.

## Startup and shutdown contract

Add validated configuration STARTUP_TIMEOUT_SECONDS (default30, range1..600) and
SHUTDOWN_GRACE_SECONDS (default20, range1..25). Invalid values fail with a safe
field-name error, never their contents. Backend compose stop_grace_period is30s
in both supported compose files, leaving at least5s outside application grace.
This supported range is a controller choice; a longer grace needs a coordinated
orchestrator change. Existing credentials/environment values remain intact.
The current release runner uses installation-owned base compose files and does
not copy those two files from the release. Therefore also set the backend-only
stop_grace_period to30s in its existing runtime override; preserve its environment
and old-runtime rollback bytes. Do not replace whole installed compose files or
change other services just to deliver this setting. Verify Config.StopTimeout30
on the deployed backend independently.

Required configuration, PostgreSQL, Redis and migration validation must complete
before ready/worker startup. PostgreSQL connect/Ping and Redis initialization
each have at most3s under the caller's startup deadline. Migration validation and
ownership bootstrap SQL inherit startup cancellation. Required failure returns
nonzero with an actionable safe diagnostic; never continue with nil dependencies.
Preserve migration singleton/checksum enforcement and required-directory failure.
Do not print full configuration, DSNs, passwords or provider payloads.

Register SIGTERM/interrupt early so startup can be canceled. Listener/metrics bind
failure cleans already-open resources and does not leave workers running. Avoid
log.Fatal below the small main exit boundary, because it skips deferred cleanup.

On termination: mark draining/unready first; reject newly admitted mutations with
503/service_draining, Retry-After:1 and a recoverable response. GET/HEAD/OPTIONS
remain safe until HTTP closes. Existing requests keep their request contexts for
the HTTP grace: do not attach them to the immediately canceled signal context.
Default20s is one total deadline: up to14s HTTP, up to5s worker cancellation/wait,
and up to1s final cleanup. Scale/shorten phases for a shorter configured grace;
no phase can exceed the overall deadline. Force-close HTTP after its phase,
cancel/wait workers, stop metrics polling/listener and close SQL/Redis within
remaining time. Cleanup is idempotent and startup-error paths use it too.
Timeouts return a safe bounded shutdown result; no dependency close can hang the
process past grace. Standard unbuffered logging needs no invented async flush.

## Health and admission

/api/live is cheap process liveness, no SQL/Redis probes, and remains200 while
draining. /api/ready and legacy /api/health report initialized schema, current
dependency connectivity and drain state. Preserve the legacy healthy JSON keys
and existing dependency-down503 behavior; do not claim that behavior as new.
Use at most one in-flight readiness probe per process, nonqueued admission,
250ms PostgreSQL and250ms Redis subdeadlines inside a500ms request deadline.
Probe contention returns bounded503; dependency/pool exhaustion never hangs a
health request or changes liveness into a restart storm. All probes honor the
caller context, including connection-pool wait.

Preserve ReadHeaderTimeout5s, ReadTimeout65s, WriteTimeout70s, IdleTimeout60s,
MaxHeaderBytes64KiB and route budgets API8s/upload60s/domain2s/static20s.
Add bounded request bodies: ordinary API2MiB; the exact product-image upload
route10MiB; the existing supplier product/variant image collections48MiB.
Classify using existing exact IsUpload routes, never Content-Type alone. Existing
stricter handler limits remain. Reject known oversized bodies before handlers;
streamed/chunked bodies remain bounded and cannot commit after a decode limit
error. Preserve slow-body socket deadlines and upload behavior.

## JOB-01 follow-ups included

Supported exported delivery/monthly compatibility entrypoints must share Runner
admission/lease/lifecycle. Private direct cycle helpers can remain test internals.
Manual Run before Start must also be canceled by Stop; no late admission after
Stop, and no WaitGroup additions race shutdown. Expose context-aware worker stop
for the process budget while preserving the existing five-second Stop wrapper.

Remove the redundant delivery return-window pre-read/argument, keeping the
authoritative guarded transaction read and existing20-day/configured-window rules.
Exclude an active financial-review marker from automaticRetry counting, even if
both retry markers exist. Prove Ready0/Review1/AutomaticRetry0 in that state;
financial-review alarms stay visible. No migration/history rewrite.

Controlled capacity diagnosis also showed the scheduler accumulated outage
backoff despite confirmed partial commits. Consume the returned progress: after
positive Affected or Confirmed work with successful outcome recording, reset
error retry and its next streak to2s. Zero-progress, failed metadata and canceled
work retain the original2..60s exponential behavior. Do not widen any job budget,
period,75s backlog-drain fixture limit or300s expiry SLO to hide this failure.

## Acceptance and release

Observe meaningful current-behavior REDs for unbounded pool-exhausted health,
nil-dependency startup continuation, missing graceful shutdown/admission, manual
pre-Start worker cancellation and overlapping retry reporting. Use actual owned
PG/Redis/HTTP/process fixtures, never production failures or real providers.

Required tests: unhealthy dependencies return readiness503 within500ms plus a
small scheduling allowance while liveness200; a held sole DB connection cannot
hang probes, and concurrent probes do not multiply SQL work. Actual startup
subprocess fails safely for DB/config/migration failure without panic or secret
echo. Bind failure cleans resources. Real SIGTERM with active reads and checkout
finishes or returns recoverable state; replay after restart cannot duplicate the
logical order/payment/ledger/stock effect. Verify the composed worker cancels and
waits before dependency close, including timeout cleanup, repeated shutdown and
manual work started before scheduling. Real slow-header/body and oversized JSON/
upload tests demonstrate finite resource use and exact route classification.

Fresh builder, task and whole-change reviews; frozen allowed-source focused/full
Go, unchanged-frontend identity or full frontend when touched, build/vet/Linux
and isolated process rehearsal. Root commits explicit reviewed files, pushes,
runs deploy.ps1 and independently verifies source, restricted backup, sealed
003–007 checksums, health/live/ready, worker outcomes, financial/history
conservation and the backend30s stop grace. Continue remaining backlog.
