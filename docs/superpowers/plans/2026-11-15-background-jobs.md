# JOB-01 implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development with one fresh implementer for this coherent task, task review and whole-change review. Root retains release actions.

**Goal:** Independent bounded/recoverable jobs, timely expiry and truthful admin outcomes.
**Architecture:** Fixed cancellable job loops, PG owner leases, per-job bounded business batches and fresh bounded metadata recording.
**Tech:** Existing Go/PostgreSQL/Redis/React; no service/dependency additions.
**Spec:** docs/superpowers/specs/2026-11-15-background-jobs-design.md

## Global constraints
Preserve migrations001–006 and all older files. New007 only. Exclude docs/fa and
networkreward. Preserve production data, runtime credentials, primary services,
refund identities, exact inventory authority and configured business windows.
All numeric periods, bounds, retries and SLO definitions are in the spec. No real
providers/messages; isolated marker-guarded test targets only. Local capacity only.

## Review focus
Cancellation after a committed order cannot erase progress; an expired lease
owner cannot acknowledge/release a newer owner; slow monthly/SLA work cannot take
the expiry lane; callback cancellation cannot leave unbounded SQL; missing outcome
records and review blockers remain visible; hold TTL never extends the true window.

### Task1: End-to-end background jobs and controls

File map (builder may split focused helpers): create internal/jobrunner engine
and tests; repository job-control/outcome helpers/tests and migration007; modify
worker/{scheduler,operations,settlement}.go and repository/{operations_repo,
order_repo}.go for bounded jobs preserving guards. Modify payment hold helper and
its regressions for true deadline TTL. Update admin job callback/main wiring and
operations overview plus existing AdminOperationsTab/types/tests for health alerts.
Add operational contract and update public progress only after local acceptance.

- [x] Read the supplied JOB-01 backlog/current callers and builder questions; record rulings.
- [x] Write/observe named existing-behavior RED regressions before implementation.
- [x] Implement lifecycle, fixed admission, durable leases and bounded record store; prove cancellation/two-worker ownership and metadata failure visibility.
- [x] Bound each business job and retry progress; align holds with payment deadline, preserve money/stock and late-payment handling; test burst/sustained backlog and slow-job independence.
- [x] Wire truthful admin alerts/manual cancellation; test actual HTTP and UI behavior.
- [x] Measure local overlap/connection/latency and rehearse migration/rollback compatibility.
- [x] Fresh task and whole-change review, fix Important findings with RED/GREEN; complete frozen tests/build gates.
- [x] Root commits explicit reviewed files, pushes, deploys with deploy.ps1 and verifies source/backup/sealed migrations/money/history/health. Continue remaining backlog.
