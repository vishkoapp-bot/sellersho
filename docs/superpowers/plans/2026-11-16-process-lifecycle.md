# OPS-01 process lifecycle implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development with one coherent implementer, fresh task review and final whole-change review. Root retains staging/commit/push/deploy.

**Goal:** Bounded fail-closed startup, truthful health and controlled process shutdown.
**Architecture:** Go lifecycle/readiness component around the existing HTTP server and JOB-01 worker; no new services.
**Tech Stack:** Existing Go, PostgreSQL, Redis, Docker Compose.
**Spec:** docs/superpowers/specs/2026-11-16-process-lifecycle-design.md

## Global Constraints

Preserve runtime credentials, money/stock authority, primary access and services.
All migrations through007 are sealed; no schema changes. Exclude docs/fa and
paused networkreward. No real providers or production fault injection. Startup,
shutdown, health, body and route budgets are the exact values in the spec.
Local tests do not certify100RPS/production-equivalent capacity.

## Review Focus

- A canceled signal context must not prematurely cancel accepted HTTP work.
- Exhausted SQL pools and readiness floods cannot pin unbounded probes.
- Cleanup after bind/startup failure and after a drain timeout must still finish.
- Pre-Start manual work and exported compatibility APIs cannot bypass Stop.
- Streaming/oversized bodies and overlapping retry markers must remain truthful.

### Task1: End-to-end process lifecycle and health

**Files:** create a focused backend/internal/lifecycle component and tests;
backend/cmd/server startup/lifecycle/process integration tests and an operational
contract. Modify main.go narrowly, health.go, config.go and config tests,
repository/{postgres,redis,background_job_health}.go, worker/{scheduler,settlement},
jobrunner lifecycle if needed, requestbudget body enforcement, both compose
backend stop-grace settings and the release runner's backend-only runtime override.
Builder may split focused helpers, not broad-format main.

**Interfaces:** lifecycle component owns initialized/draining admission and bounded
health handlers, plus Serve/drain orchestration with context-aware worker/dependency
closers. Repository context constructors preserve existing wrapper signatures.
Worker exposes StopContext(ctx) error and preserves Stop(). Config adds startup
and shutdown durations. The release runner delivers backend stop_grace_period30s
through its existing runtime override; root retains release actions.

- [x] Read spec/backlog/current startup/health/worker/budget callers; record any rulings.
- [x] Observe named current-behavior REDs, not new-API compilation failures.
- [x] Implement startup cancellation/required failure and safe resource cleanup; test actual subprocess failures/bind cleanup without secret echo.
- [x] Implement live/ready/legacy bounded probes and draining mutation admission; test actual PG pool exhaustion, dependency failure, probe concurrency and safe reads.
- [x] Wire signal-driven HTTP/worker/metrics/dependency shutdown and compose30s grace; test actual process SIGTERM with recoverable checkout and restart conservation.
- [x] Bound bodies using exact existing route classes; exercise slow headers/bodies and known/chunked oversize rejection without mutation.
- [x] Close the three JOB follow-ups with real guard/window/counting regressions; test pre-Start manual cancellation and idempotent stop.
- [x] Self-review and report exact explicit allowlist/RED-GREEN/remaining gates; no implementer stage/commit/push/deploy.
- [x] Root obtains fresh task/final reviews, frozen gates, commits/pushes, deploys.ps1 and verifies live/ready/health/sealed migrations/backup/money/history/stop grace. Continue backlog.
