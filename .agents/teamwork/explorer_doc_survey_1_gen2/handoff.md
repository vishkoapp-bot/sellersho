# Handoff Report: Exhaustive Technical & Architectural Survey of Sellersho Platform

**Agent:** `teamwork_explorer_survey_1_gen2`  
**Date:** 2026-11-07T22:45:00Z  
**Recipient:** Parent Orchestrator (`243d6987-8b38-4157-8806-a46f267c60d0`)  
**Artifact:** `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1_gen2\survey_tech_report.md`  

---

## 1. Observation

Direct, verified codebase observations from primary source files:

1. **Architecture & Frameworks:**
   - Go backend: Go 1.23 modular monolith (`backend/go.mod:3`).
   - Standard library router: `net/http.ServeMux` (`backend/cmd/server/main.go:251`).
   - Frontend: React 19.0.0, Vite 6.1.0, Tailwind CSS v4.0.7 (`package.json:18, 23, 33`, `vite.config.ts:3`).
   - PostgreSQL driver: `github.com/lib/pq` v1.12.3 with native `database/sql` queries; no ORM (`backend/go.mod:8`).
   - Redis driver: `github.com/redis/go-redis/v9` v9.7.0 (`backend/go.mod:9`).

2. **Database Models & Migrations:**
   - Exactly 308 SQL migration files (`backend/migrations/*.sql`) tracked transactionally via `schema_migrations` (`backend/cmd/server/main.go:69-155`).
   - Over 130 database tables created with foreign keys, checks, unique constraints, and indexes (`backend/migrations/000001_initial_schema.up.sql` to `202611060001_campaign_scope_variant.up.sql`).
   - Double-entry immutable financial ledger: `ledger_entries` table with target types `seller`, `supplier`, `carrier`, `platform`, `warehouse` and transaction types `debit`/`credit` (`backend/migrations/000001_initial_schema.up.sql:189`).

3. **Authentication & Authorization (RBAC):**
   - OTP Service: 4-digit code generated via `crypto/rand`, stored in Redis with 120s TTL and 30s cooldown (`backend/internal/auth/otp.go:40-90`).
   - Brute-force protection: Atomic Redis Lua script capping verification to 5 failed attempts before instant deletion (`backend/internal/auth/otp.go:131-145`).
   - Password login: Strictly gated to allowlisted super-admin mobiles (`09933070870`, `09127504005`) with bcrypt password hash verification (`backend/internal/accesspolicy/super_admin.go:9-21`, `backend/cmd/server/auth_handlers.go:305-321`).
   - Token & Claims: HMAC-SHA256 JWT valid for 7 days with payload `{userId, mobile, fullName, role, capabilities}` (`backend/internal/auth/jwt.go:119-144`).
   - Real-time mid-session verification: `jwtService.RefreshCapabilities` re-queries active capabilities from `user_capabilities`, and `jwtService.IsUserBlocked` rejects disabled users where `users.is_active = FALSE` (`backend/cmd/server/auth_handlers.go:24-60`).

4. **Canonical Pricing Engine:**
   - Formulas:
     - Reference cost: $R = \text{RoundTo100}(C \times (1 + \text{platformPct}/100))$
     - Platform fee: $F = R - C$
     - Final unit price: $P = \text{RoundTo100}(R \times (1 + \text{margin}/100))$
     - Seller margin: $D = P - R$
     - Central store price: $P = \text{RoundTo100}(C \times 1.10)$ with default `centralProfitPct = 10%`
     - Display discount: $\text{round}((OriginalPrice - P) / OriginalPrice \times 100)$
     - Source: `backend/internal/pricing/reference_pricing.go:10-23, 227-253`.

5. **External Integrations & Workers:**
   - SMS Gateway: IPPanel pattern-based sending (`POST https://edge.ippanel.com/v1/api/send`) (`backend/internal/ippanel/client.go:138-150`).
   - Payment Gateway: Shaparak PSP (`RealShaparakGatewayClient`) and `MockPaymentGatewayClient` with 15-minute atomic Redis inventory hold keys (`inventory:hold:order:{id}:product:{id}`) (`backend/internal/payment/gateway.go`, `backend/cmd/server/payment_handlers.go:69-95`).
   - Logistics & Post: Tapin upstream Tipax API (`https://api.tapin.ir`) envelope protocol with price calculation, parcel registration, and label streaming (`backend/internal/tapin/client.go:35-66, 245-300`).
   - Image Upload: Multipart handler with 10MB body cap, 5MB file cap, JPG/PNG/WEBP magic-bytes sniffing, UUID renaming, and `./static/uploads/products/` storage (`backend/cmd/server/upload_handlers.go:17-120`).
   - Background Tasks: `BackgroundWorker` with 60s ticker for 2-stage delivery lock release (Stage 1: auto-confirm after 20 days; Stage 2: release profit after 7-day return window) and monthly settlements (`backend/internal/worker/scheduler.go:102-160`).
   - Infrastructure & Reverse Proxy: Multi-container `docker-compose.yml`, Alpine 3.19 Dockerfile, Nginx reverse proxy with gzip, immutable assets caching, and 301 Persian product URL redirects (`nginx.conf:13-364`).

---

## 2. Logic Chain

1. **Requirement Analysis:** The user request mandates comprehensive platform documentation in Persian (`docs/fa/`) with complete workflows, technical coverage, and concrete examples.
2. **Investigation Execution:** As the technical survey explorer (`explorer_doc_survey_1_gen2`), we conducted a comprehensive review of all backend Go packages, SQL migration files, domain models, routers, middlewares, external integrations, workers, and deployment configs.
3. **Synthesis & Structuring:** We organized the technical reality into 10 structured sections in `survey_tech_report.md` covering architecture, database models, auth/RBAC, canonical pricing, API routes, integrations, workers, infrastructure, and concrete payloads.
4. **Enabling Downstream Writers:** With `survey_tech_report.md` established as the verified single source of truth, technical and business documentation writers can author `docs/fa/` with zero placeholder text or architectural hallucinations.

---

## 3. Caveats

- **Network Mode:** In accordance with subagent operating environment constraints, upstream network calls to live third-party endpoints (`api.tapin.ir`, `edge.ippanel.com`, Shaparak live terminal) were not executed; their contracts were verified via primary client implementations, unit tests, and live-parity audit files.
- **Legacy branch features:** Certain experimental branches (e.g. `seller_discount_codes` quote evaluation, absolute base-fee F overrides) are present in earlier commits or stubs but are confirmed superseded by the percent-only canonical pricing engine.

---

## 4. Conclusion

The comprehensive technical survey of the Sellersho platform is 100% complete and documented in:
`C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1_gen2\survey_tech_report.md`

All six core dispatch objectives are addressed with verified file paths, line citations, schema definitions, endpoint registries, mathematical formulas, and concrete JSON/cURL/SQL examples.

---

## 5. Verification Method

To independently verify the observations and findings documented in this report:

1. **Verify Backend Tests:**
   ```bash
   cd backend
   go test -v ./internal/pricing/...
   go test -v ./internal/auth/...
   go test -v ./internal/tapin/...
   ```
2. **Verify Database Migrations Count:**
   ```powershell
   Get-ChildItem -Path backend\migrations -Filter *.sql | Measure-Object | Select-Object -ExpandProperty Count
   # Expected output: 308
   ```
3. **Inspect Primary Source Files:**
   - Architecture & Router: `backend/cmd/server/main.go`
   - Domain Models: `backend/internal/domain/models.go`
   - Canonical Pricing Engine: `backend/internal/pricing/reference_pricing.go`
   - Auth & OTP Services: `backend/internal/auth/jwt.go`, `otp.go`
   - External Integrations: `backend/internal/ippanel/client.go`, `backend/internal/tapin/client.go`, `backend/internal/payment/gateway.go`
   - Nginx Reverse Proxy & 301 URL Map: `nginx.conf`
