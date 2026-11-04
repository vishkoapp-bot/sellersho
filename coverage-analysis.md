# Comprehensive Test Coverage Analysis Report

**Date:** 2026-10-01  
**Project:** Drup Dropship Platform (Go Modular Monolith + Vite React Frontend)

---

## 1. Executive Summary

- **Backend Total Statement Coverage:** **8.8%** (`go tool cover -func backend/coverage.out`)
- **Frontend Test Coverage:** **0% (Unconfigured)** — No test framework (Vitest/Jest) or test scripts currently defined in `package.json`.
- **Core Strengths in Backend Tests:**
  - `internal/pricing` (~84.9% coverage): Rigorous testing of base cost, platform fee calculations, margin resolution, and 100-toman rounding.
  - `internal/privacy` (~84.5% coverage): Customer PII masking rules (name, phone, address, postal code) based on caller roles (supplier, seller, logistics, admin).
  - `internal/bank` (71.4% coverage): Iranian Sheba/IBAN checksum validation.
  - `internal/repository` (Redis Hold Engine) (~88.5% coverage): Atomic Lua script inventory reservation, order-level holds, expiration TTLs, and cleanup routines.
- **Coverage Deficits:**
  - All HTTP handler layers (`cmd/server`) and SQL database repositories (`internal/repository/*_repo.go`) currently rely on manual/integration testing rather than automated unit/integration test suites with mocked or testcontainer databases.

---

## 2. Backend Package Breakdown

| Package / Module | Functions Tested / Total | Statement Coverage | Status |
| :--- | :--- | :--- | :--- |
| `github.com/drup/platform/internal/pricing` | 3 / 4 | **84.9%** | Passed / Solid |
| `github.com/drup/platform/internal/privacy` | 5 / 5 | **84.5%** | Passed / Solid |
| `github.com/drup/platform/internal/bank` | 1 / 1 | **71.4%** | Passed / Solid |
| `github.com/drup/platform/internal/repository` (Redis) | 5 / 5 | **88.5%** | Passed / High Reliability |
| `github.com/drup/platform/internal/database/seeder` | 5 / 18 | **12.1% - 44.4%** | Partial (Clean/Reset tested) |
| `github.com/drup/platform/internal/repository` (SQL Repos) | 0 / 37 | **0.0%** | Critical Deficit |
| `github.com/drup/platform/cmd/server` (HTTP Handlers) | 0 / 25 | **0.0%** | Critical Deficit |
| `github.com/drup/platform/internal/auth` (JWT & OTP) | 0 / 8 | **0.0%** | Deficit |
| `github.com/drup/platform/internal/worker` (Background) | 0 / 4 | **0.0%** | Deficit |
| `github.com/drup/platform/internal/config` | 0 / 1 | **0.0%** | Low Risk |

---

## 3. High-Coverage Modules Analysis (>50%)

1. **`internal/pricing` (84.9%)**
   - `RoundTo100`: 100.0%
   - `ResolvePricing`: 84.9%
   - `ResolvePlatformFeeF`: 60.0%
   - *Behavior:* Accurately verifies platform fee tiers, supplier pricing, and seller customized margins without floating-point drift.

2. **`internal/privacy` (84.5%)**
   - `MaskCustomerForRole`: 100.0%
   - `MaskName`: 91.7%
   - `MaskAddress`: 88.9%
   - `MaskPostalCode`: 75.0%
   - `MaskPhone`: 66.7%
   - *Behavior:* Guarantees supplier and seller role isolation, ensuring customer identity and phone numbers cannot be scraped by unauthorized tiers.

3. **`internal/bank` (71.4%)**
   - `VerifySheba`: 71.4%
   - *Behavior:* Validates ISO 7064 MOD 97-10 check digits and Persian IBAN structure before payout requests are submitted.

4. **`internal/repository` (Redis Inventory Locking) (88.5%)**
   - `ReleaseInventory`: 100.0%
   - `HoldInventory`: 93.3%
   - `NewRedisClient`: 88.2%
   - `HasInventoryHold`: 85.7%
   - `ReleaseOrderInventory`: 75.0%
   - *Behavior:* Atomic Lua hold scripts manage JIT stock reservations prior to gateway payment without risk of double-selling.

---

## 4. Low-Coverage Packages (<50%) and Untested Critical Functions

### A. HTTP Server & Endpoints (`cmd/server` - 0.0%)
- **Untested Handlers:**
  - `handleSendOTP`, `handleVerifyOTP`, `handlePasswordLogin`, `AuthMiddleware`, `RequireRoles`
  - `handleProductsList`, `handleProductDetail`
  - `handleStoresList`, `handleRegisterStore`, `handleStoreOverrides`
  - `handleCreateOrder`, `handleTrackOrder`, `handleConfirmDelivery`, `handleListOrders`
  - `handleShaparakToken`, `handleShaparakCallback`
  - `handleGetBins`, `handleGetInbounds`, `handleGetManifests`, `handleDispatchManifest`
  - `handleGetLedger`, `handleGetDisputes`, `handleCreateDispute`, `handleResolveDispute`
  - `handleGetApprovals`, `handleCreateApproval`, `handleReviewApproval`, `handleGetAuditLogs`
- **Impact:** While end-to-end integration and manual testing verify these paths, unit tests using `net/http/httptest` are missing.

### B. SQL Database Repositories (`internal/repository` - 0.0%)
- **Untested Files:**
  - `admin_repo.go`: Approvals and audit log recording.
  - `catalog_repo.go`: `GetProducts`, `GetProductByID`, `GetCategories`, `GetNiches`.
  - `order_repo.go`: `CreateOrder`, `GetOrderByID`, `GetOrderByTrackingCode`, `GetOrdersByStoreID`, `ConfirmOrderDelivery`.
  - `store_repo.go`: `GetStoreBySlug`, `GetAllStores`, `CreateStore`, `SetStoreProductOverride`, `SetStoreMargin`.
  - `user_repo.go`: `FindByMobile`, `FindByUsername`, `RecordLoginFailure`, `UpsertUser`.
  - `wms_repo.go`: `GetBins`, `GetInboundShipments`, `GetManifests`, `DispatchManifest`.
  - `finance_repo.go`: `GetLedgerEntries`, `AddLedgerEntry`, `GetDisputes`, `CreateDispute`, `ResolveDispute`.
- **Impact:** High risk for database schema drift and query syntax regressions during migrations if not covered by automated test databases or mock suites.

### C. Authentication Services (`internal/auth` - 0.0%)
- **Untested:**
  - `jwt.go`: Token generation, expiration check, claim extraction, role validation.
  - `otp.go`: 5-digit code generation, Redis TTL storage, rate limiting, single-use invalidation.

### D. Background Worker (`internal/worker` - 0.0%)
- **Untested:**
  - `scheduler.go`: Goroutine ticker releasing expired delivery locks and stale reservations.

---

## 5. Frontend Test Framework Assessment

### Current Setup in `package.json`:
- **Build tool:** Vite 6.1.0 + TypeScript 5.7.3
- **Dependencies:** React 19, Motion, Lucide-react, Tailwind CSS v4
- **Scripts:** `"dev"`, `"dev:all"`, `"build"`, `"preview"`
- **Test Runner:** **None**. There is no Jest, Vitest, or test runner package installed.

### Recommendations for Frontend Testing:
1. **Install Vitest and React Testing Library:**
   ```bash
   npm install --save-dev vitest @testing-library/react @testing-library/jest-dom jsdom
   ```
2. **Add Script to `package.json`:**
   ```json
   "scripts": {
     "test": "vitest run",
     "test:watch": "vitest",
     "test:coverage": "vitest run --coverage"
   }
   ```
3. **Target High-Value Frontend Modules for Unit Tests:**
   - `src/services/apiClient.ts` (JWT injection, 401 token refresh, error normalization)
   - `src/context/StoreContext.tsx` (Cart calculation, margin resolution, store switching)
   - `src/components/views/ProductDetailView.tsx` (Add to cart, Buy now, pricing calculation)

---

## 6. Action Plan to Reach >70% Test Coverage

1. **Target 1: HTTP Handler Unit Tests (`cmd/server`) using `httptest`:**
   - Create mock repository interfaces or utilize an in-memory SQLite / test Postgres instance.
   - Add tests verifying HTTP status codes, role enforcement (401/403), payload validation (400), and proper response serializations.
2. **Target 2: Auth Unit Tests (`internal/auth`):**
   - Add tests for `JWTService.GenerateToken` and `JWTService.ValidateToken`.
   - Add tests for OTP generation, rate limiting, and replay prevention.
3. **Target 3: SQL Integration Tests (`internal/repository`):**
   - Leverage `sqlmock` or a localized Docker test DB (`dockertest` or `testcontainers-go`) to test complex transactions like `order_repo.CreateOrder` and `finance_repo.AddLedgerEntry`.
4. **Target 4: Frontend Component & Hook Tests:**
   - Configure Vitest with testing-library to achieve automated regression verification for cart, pricing, and checkout workflows.

| `github.com/drup/platform/internal/database` | 0 / 2 | **0.0%** | Low Risk |
| `github.com/drup/platform/cmd/seed` | 0 / 1 | **0.0%** | Low Risk |
| **TOTAL** | **19 / 140 functions** | **8.8%** | **Requires Expansion** |
