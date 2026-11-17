# Request/provider budget implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Bound request/dependency work, safely retry reads, preserve uncertain mutation identities and reject overload without retry storms.
**Architecture:** Synchronous HTTP deadline/admission middleware; shared provider executor with explicit safe-read allowlist; bounded context-aware Redis and generation-safe pre-send OTP reservation. Existing SQL already propagates request contexts.
**Tech Stack:** Go1.23, existing Go/Redis/PostgreSQL dependencies, React/TypeScript, nginx/Caddy. No new dependencies.
**Spec:** docs/superpowers/specs/2026-11-15-request-provider-budgets-design.md

## Global Constraints
- Preserve migrations001-005; no schema change or actual PSP/SMS in tests.
- Ordinary API8s context/10s socket/13s nginx/14s Caddy/15s browser; image uploads60/65/68/70/75s. Domain authorization2s/edge4s. Header5s/read65s/write70s/idle60s server ceilings.
- API64 and upload4 in-flight limits. Tapin16 concurrency/32RPS/burst32; IPPanel8/8/8, shared across instances, no queue. Provider5s logical budget; reads2s attempt, max2 attempts, jitter100-250ms; mutations one attempt.
- Redis context deadlines enabled; no retries; dial1s/read2s/write2s/pool1s/32 active. Existing OTP30s/120s/five attempts preserved, codes reserved before send, conditional cleanup only for proven rejection.
- Exclude docs/fa and paused networkreward. Validate, commit, push, deploy.ps1, verify; preserve credentials/services/backups. Hardware capacity OPEN.

## Review Focus
- Lost SMS response retains a verifiable code/cooldown; an old cleanup cannot delete a later generation.
- Context deadline covers slow response bodies and backoff, not just receiving headers; Redis defaults must not escape it.
- Locally refused admission cannot be mistaken for an unknown sent shipment; unknown sends must not be retried.
- Already-started HTTP response cannot be rewritten as success; committed checkout remains recoverable after disconnect.
- Long upload classification is exact and bounded, and all upload callers actually use the longer finite browser budget.

### Task 1: End-to-end deadlines and safe dependency retries

**Files:**
- Create backend/internal/dependencybudget/{executor,redis}.go and executor_test.go; backend/internal/testredis/deadline_test.go.
- Create backend/internal/requestbudget/http.go and http_test.go; backend/cmd/server/request_budget_test.go.
- Modify backend/internal/repository/{redis,tapin_repo}.go; add tapin_budget_integration_test.go; backend/internal/testredis/testredis.go; backend/internal/auth/otp.go; add otp_delivery_test.go.
- Modify backend/internal/{tapin,ippanel}/client.go; add client_budget_test.go in each package; backend/cmd/server/{main,auth_handlers,tapin_handlers,ippanel_handlers}.go.
- Modify backend/internal/observability/{metrics,dependencies}.go as needed for fixed overload counters and per-provider observations.
- Modify src/services/{apiClient,supplierApi}.ts; add apiClient.budget.test.ts; extend upload request tests.
- Modify nginx.conf, Caddyfile; add/update docs/operations/request-budgets.md and public progress.

**Interfaces:**
- dependencybudget.Policy + New(Policy) *Executor; Executor.Do(ctx,client,makeRequest,safeRead,maxBodyBytes) (Reply,error), Reply{StatusCode,Header,Body}. Error sentinel ErrOverloaded and IsAmbiguous/MarkAmbiguous; no secret values in executor errors/telemetry. Observer callback keeps helper independent of observability to avoid test fixture import cycles.
- dependencybudget.ConfigureRedis(*redis.Options) used by production and marker-verified test fixtures.
- requestbudget.New(Config) http middleware with DefaultConfig, parent cancellation, ResponseController/Unwrap and synchronous response guard; Config injects shorter durations for real timing tests.
- OTP reserve/conditional cleanup Lua operations use existing RedisClient; typed unknown delivery error for truthful handler502, provider overload503/retry-after1. Definite send rejection may clear only matching code/cooldown; no request detachment.

- [x] Write and run existing-code regressions: SMS accepted then response lost leaves no usable code/cooldown; missing Redis falsely reports success; caller-supplied AbortSignal bypasses explicit browser timeout. Expected RED for observable behavior, not compilation.
- [x] Write meaningful unit/integration tests for executor budgets/body/backoff/read allowlist/no-send retry and bounded parallel admission; implement executor/Redis options and shared Tapin/IPPanel integration. Expected focused tests PASS; immutable shipment attempt guards preserved on uncertainty.
- [x] Implement atomic OTP reservation and generation-safe cleanup; real disposable Redis proxy test proves stalled reservation deadlines and no send. Expected all OTP failure/verification/cooldown regressions PASS with no real SMS.
- [x] Implement synchronous route deadline/admission middleware, integrate main, context-aware SQL/Redis propagation and truthful timeout/overload responses. Test real HTTP delay/partial response, migrated PG cancellation/pool recovery and committed checkout recovery. Expected bounded responses and single stock/ledger/order effect.
- [x] Align nginx/Caddy and upload browser budgets, compose manual signal plus browser timeout; validate actual config syntax network-disabled and browser contracts. Expected PASS.
- [ ] Full isolated Go from allowed frozen archive, complete frontend/typecheck/build/vet/Linux builds and independent fresh whole-change review; reproduce/fix Important findings with RED/GREEN and repeat required gates. Expected zero failures.
- [ ] Update status, commit explicit allowlist, push, deploy.ps1 -BuildOnServer and verify backup, source marker, sealed migration checksums, financial preservation and health. Expected deployment0 and origin=HEAD. No doc-only deployment loop.

## Refined acceptance from actual failures
Old-source Tapin claim accepted16 concurrent owners; guard must be atomic and reject registered records. No provider call may follow a losing claim. Old edge config waited14.6s and returned200 instead of bounded504, and /internal/domains/allow fell through to public200. Config probes now require explicit route denial and POST method/body preservation on the named long-upload route. Partial source/type/fixture formatting is restored before the final frozen gate.
