# BRIEFING — 2026-11-07T21:48:00Z

## Mission
Conduct an exhaustive technical and architectural survey of the Sellersho platform codebase (Go backend, PostgreSQL, Redis, Auth/RBAC, APIs, Integrations, Infrastructure).

## 🔒 My Identity
- Archetype: explorer
- Roles: investigation, synthesis
- Working directory: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1_gen2
- Original parent: 243d6987-8b38-4157-8806-a46f267c60d0
- Milestone: milestone_doc_survey

## 🔒 Key Constraints
- Read-only investigation — do NOT implement
- Exhaustive technical and architectural survey of the Sellersho platform codebase
- Focus on concrete examples, file paths, line numbers, schemas, API endpoints, auth mechanisms, integrations

## Current Parent
- Conversation ID: 243d6987-8b38-4157-8806-a46f267c60d0
- Updated: 2026-11-07T21:48:00Z

## Investigation State
- **Explored paths**:
  - `backend/cmd/server/main.go` (2,374 lines), `backend/cmd/server/*_handlers.go`
  - `backend/internal/domain/models.go` (535 lines)
  - `backend/internal/config/config.go`
  - `backend/internal/auth/jwt.go`, `backend/internal/auth/otp.go`
  - `backend/internal/accesspolicy/super_admin.go`
  - `backend/internal/pricing/reference_pricing.go`, `pricing.go`, `discount.go`
  - `backend/internal/ippanel/client.go`
  - `backend/internal/payment/gateway.go`, `backend/cmd/server/payment_handlers.go`
  - `backend/internal/tapin/client.go`, `boxes.go`
  - `backend/internal/worker/scheduler.go`, `settlement.go`
  - `backend/cmd/server/upload_handlers.go`
  - `backend/migrations/` (308 SQL migration files, 130+ database tables)
  - `backend/docker-compose.yml`, `backend/Dockerfile`, `nginx.conf`, `package.json`, `vite.config.ts`, `src/services/apiClient.ts`
- **Key findings**:
  - Go 1.23 modular monolith with native `net/http` router, PostgreSQL 16 (`lib/pq`, raw SQL repository layer), Redis 7.
  - Multi-role & capability-based authorization (`UserCapability`), OTP 4-digit code in Redis with rate-limiting Lua script (max 5 attempts, 120s TTL, 30s cooldown), IPPanel SMS.
  - Percent-only canonical pricing engine: $P = \text{RoundTo100}(R \times (1 + \text{margin}/100))$, $R = \text{RoundTo100}(C \times (1 + \text{platformPct}/100))$, with ladder overrides (Product > Category > Global) and per-product margin ceilings.
  - 130+ PostgreSQL tables spanning core users, suppliers, stores, catalog, orders, WMS/QC/putaway, returns/disputes, double-entry immutable ledger, Tapin shipping tariffs, and platform operations.
  - External integrations: IPPanel (pattern SMS), Shaparak PSP / mock simulator with atomic Redis stock hold, Tapin Tipax upstream API (`api.tapin.ir`), 60s background worker for staged 20-day delivery lock release and monthly settlements, strict magic-bytes image upload handler, Nginx reverse proxy with 301 canonical Persian SEO URL redirects.
- **Unexplored areas**: None (all 6 survey objectives fully explored).

## Key Decisions Made
- Workspace fully surveyed across all 6 technical domains.
- Compiling comprehensive technical survey report `survey_tech_report.md` with complete architecture maps, DB schema inventories, API registries, auth flows, integration specifications, and concrete code/payload examples.

## Artifact Index
- survey_tech_report.md — Comprehensive technical survey report
- handoff.md — 5-component handoff report
- progress.md — Liveness heartbeat
- DISPATCH.md — Log of dispatch instructions
