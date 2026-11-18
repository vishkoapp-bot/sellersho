# Comprehensive Technical and Architectural Survey Report: Sellersho Platform

**Document Version:** 1.0.0  
**Date:** 2026-11-07  
**Author:** Teamwork Explorer Survey Agent (`teamwork_explorer_survey_1_gen2`)  
**Target:** Sellersho E-Commerce, Dropshipping, Fulfillment, and Logistics Platform  
**Scope:** Full-stack codebase examination (`backend/`, `src/`, database migrations, configurations, integrations, infrastructure)

---

## Table of Contents
1. [Executive Summary & High-Level Architecture](#1-executive-summary--high-level-architecture)
2. [Codebase Organization & Architectural Pattern](#2-codebase-organization--architectural-pattern)
3. [Database Architecture & Schema Deep Dive](#3-database-architecture--schema-deep-dive)
4. [Authentication, Authorization & Role-Based Access Control (RBAC)](#4-authentication-authorization--role-based-access-control-rbac)
5. [Canonical Pricing & Margin Engine](#5-canonical-pricing--margin-engine)
6. [Comprehensive Backend API Route Registry](#6-comprehensive-backend-api-route-registry)
7. [External Integrations & Third-Party Services](#7-external-integrations--third-party-services)
8. [Asynchronous Workers, Scheduling & Event Handlers](#8-asynchronous-workers-scheduling--event-handlers)
9. [Infrastructure, Deployment & Nginx Reverse Proxy](#9-infrastructure-deployment--nginx-reverse-proxy)
10. [Concrete Code, Payload & Configuration Catalog](#10-concrete-code-payload--configuration-catalog)

---

## 1. Executive Summary & High-Level Architecture

The **Sellersho Platform** (internally identified as **Drup Platform**) is an enterprise multi-tenant dropshipping, B2B wholesale marketplace, and automated fulfillment/WMS platform specifically tailored for the Iranian e-commerce ecosystem.

### Key Architectural Tenets:
- **Modular Monolith Core:** Implemented in **Go 1.23+**, providing sub-millisecond route dispatching, type safety, low memory overhead, and compile-time concurrency primitives.
- **Pure Relational Database with Raw SQL:** Powered by **PostgreSQL 16**, eschewing heavy ORMs (such as Prisma, GORM, or Hibernate) in favor of explicit, auditable, high-performance parameterized SQL scripts executed via `database/sql` and `github.com/lib/pq`. Over 130 database tables are versioned across 308 transactional migration files.
- **Double-Entry Financial Ledger:** All monetary movements (seller profits, supplier wholesale payouts, platform commissions, shipping fees, returns, adjustments) are recorded into an immutable, double-entry ledger (`ledger_entries`).
- **High-Throughput Caching & Distributed Synchronization:** Powered by **Redis 7** for temporary order inventory holds (redlock pattern), brute-force OTP attack prevention via atomic Lua scripts, ad click anti-fraud deduplication, and catalog taxonomy caching.
- **Modern Responsive Frontend:** Single-page application built on **React 19**, **Vite 6**, and **Tailwind CSS v4**, featuring 5 distinct storefront template engines (Modern, Electronic, Tech, Digikala, and Fashion/Aastin).

```
                      +------------------------------------------+
                      |         End Users & Merchants            |
                      |   (Storefront / Seller / Supplier / Admin)|
                      +------------------------------------------+
                                           |
                                           v
                             +---------------------------+
                             |   Nginx Reverse Proxy     |
                             |   - SSL / Security Headers|
                             |   - 301 Product SEO Map   |
                             |   - Static SPA & Assets   |
                             +---------------------------+
                                   |               |
               /static/uploads/    |               |  /api/v1/*
                                   v               v
            +------------------------------------------------------+
            |             Go 1.23 Modular Monolith                 |
            |  - Router & Dispatcher (net/http ServeMux)           |
            |  - Auth & Capability Evaluator (JWT + RBAC)          |
            |  - Domain Handlers (Catalog, Store, Order, WMS, etc.)|
            |  - Canonical Pricing Engine (Percent Ladder)         |
            |  - Background Worker (2-Stage Release & Settlement)  |
            +------------------------------------------------------+
                     |                     |               |
                     v                     v               v
      +------------------------+  +-----------------+  +----------------------+
      |   PostgreSQL 16 DB     |  |     Redis 7     |  | External Providers   |
      | - 130+ Relational Tables|  | - OTP State &   |  | - IPPanel (SMS)      |
      | - Immutable Ledger     |  |   Rate Limits   |  | - Shaparak / PSP     |
      | - WMS & Inventory      |  | - Inventory Hold|  | - Tapin (Tipax API)  |
      | - 308 SQL Migrations   |  | - Catalog Cache |  |                      |
      +------------------------+  +-----------------+  +----------------------+
```

---

## 2. Codebase Organization & Architectural Pattern

### 2.1 Clean Domain Isolation
The codebase enforces strict separation of concerns following clean modular design:
- `backend/cmd/server/`: HTTP entry point, route definitions, middleware chains, and HTTP controller handlers.
- `backend/internal/domain/`: Pure domain structs and entities (`models.go`) matching system contracts without transport dependencies.
- `backend/internal/repository/`: Data access layer containing parameterized SQL queries against PostgreSQL (`database/sql`) and key-value/Lua operations against Redis (`go-redis/v9`).
- `backend/internal/pricing/`: Isolated, deterministic canonical pricing engine.
- `backend/internal/auth/`: JWT token lifecycle, cryptographic hashing, and OTP verification services.
- `backend/internal/accesspolicy/`: System-level security policies, hardcoded allowlists, and privilege guards.
- `backend/internal/worker/`: Periodic cron workers and background reconcilers.
- `backend/internal/ippanel/`, `payment/`, `tapin/`: Upstream external API integration clients.
- `backend/migrations/`: Sequential database schema migration files (`.up.sql` and `.down.sql`).

### 2.2 Server Bootstrapping & Fail-Closed Lifecycle (`main.go`)
1. **Config Ingestion (`config.LoadConfig`):** Loads runtime settings. In non-dev environments (`APP_ENV=production`), it enforces **fail-closed validation**: `JWT_SECRET` must be $\ge 32$ characters, default passwords (retired dev passwords, redacted — see SEC-01 scan) are rejected, `PAYMENT_GATEWAY_MODE` must be `real`, and IPPanel credentials must be fully configured.
2. **Transactional Database Auto-Migration (`runAutoMigrations`):**
   - Checks or creates the `schema_migrations (version, applied_at)` tracking table.
   - Reads all `*.up.sql` files in alphanumeric order.
   - Executes pending migrations within an ACID transaction (`tx.Begin()` $\to$ `tx.Exec()` $\to$ `tx.Commit()`). If any statement fails, the transaction rolls back immediately and halts the process (`log.Fatalf`).
3. **Redis & Worker Initialization:**
   - Establishes connection pool to Redis 7.
   - Bootstraps `BackgroundWorker`, injecting `orderRepo` and `finRepo`.
   - Starts periodic background goroutines (every 60 seconds for delivery lock release; hourly for search rollups).
4. **HTTP Router Registration:** Registers standard library `net/http.ServeMux` routes wrapped by `enableCORS` and role-guard middlewares.

---

## 3. Database Architecture & Schema Deep Dive

The database architecture is designed with **130+ PostgreSQL 16 tables** managed via **308 versioned SQL migrations**. The schema emphasizes referential integrity, domain enums, JSONB document storage for flexible specifications, and strictly append-only financial records.

### 3.1 Domain Entity Inventory

| Domain | Key Database Tables | Purpose & Design Constraints |
|---|---|---|
| **Identity & Access** | `users`, `user_capabilities`, `user_addresses`, `customer_addresses`, `customer_store_links`, `customer_wallets`, `customer_wishlists`, `customer_withdrawal_requests`, `supplier_members`, `sms_logs` | User identity with UUID primary keys, normalized phone numbers (`09...`), multi-role active capability assignments, and per-store buyer link attribution. |
| **Suppliers & Vendor Ops** | `suppliers`, `supplier_warehouses`, `supplier_shipping_methods`, `supplier_pricing_tiers`, `supplier_price_increases`, `supplier_promotions`, `supplier_campaigns`, `supplier_shipments`, `supplier_shipment_audit_logs`, `warranty_properties`, `warranty_property_requests`, `exclusivity_challenges` | Vendor profiles, approval state machines (`pending_approval`, `approved`, `rejected`, `suspended`), contract versions, direct drop-shipping flags, and vendor warehouse origins. |
| **Stores & Dropshipping** | `stores`, `store_products`, `store_templates`, `store_product_overrides`, `store_niche_overrides`, `store_variant_overrides`, `store_margin_locks`, `store_margin_lock_snapshots`, `store_product_addons`, `store_product_addon_exclusions`, `store_related_products`, `seller_campaigns`, `seller_campaign_history`, `seller_campaign_events`, `seller_discount_campaigns`, `seller_discount_codes`, `seller_early_withdrawals`, `seller_service_purchases`, `subscription_plans`, `referral_bonuses` | Multi-tenant merchant stores (`owner_type IN ('central', 'dropshipper')`), storefront theme templates (`tpl-modern-1`, `tpl-fashion-1`, etc.), profit margins, margin override locks, and seller ad campaigns. |
| **Catalog & Taxonomy** | `categories`, `niches`, `product_niches`, `products`, `product_variants`, `product_images`, `product_addons`, `product_addon_attachments`, `product_attribute_values`, `product_reviews`, `product_margin_ceilings`, `product_market_ceilings`, `product_price_exceptions`, `product_price_daily`, `product_measurement_events`, `dk_attributes`, `dk_attribute_values`, `dk_category_attributes`, `dk_category_attribute_values`, `dk_category_brands`, `dk_category_colors`, `category_import_log`, `proposal_field_reviews`, `proposal_review_history` | Reference product catalog, SPN numbering, Digikala attribute facets, variant inventory (`spnc`), weight/dimension measurements, and product approval workflows. |
| **Orders & Checkout** | `orders`, `order_items`, `order_item_addons`, `order_item_origins`, `order_shipping_legs`, `payment_transactions`, `payment_callback_events`, `payment_callback_retries`, `refunds` | Complete order lifecycle contracts, financial line snapshots (wholesale cost, platform fee, seller margin), shipping origin splits, and transaction audit trails. |
| **WMS & Fulfillment** | `warehouses`, `warehouse_bins`, `warehouse_capacity_settings`, `warehouse_storage_positions`, `warehouse_storage_fee_charges`, `inbound_shipments`, `inbound_receiving_logs`, `qc_discrepancies`, `qc_results`, `putaway_tasks`, `putaway_capacity_reservations`, `pick_tasks`, `shipments`, `shipment_items`, `shipping_manifests`, `inventory_balances`, `inventory_movements`, `inventory_reservations` | Inbound receiving, barcode scanning, quality control (QC pass/fail), bin-level putaway, picking queues, packaging dimensions, and manifest dispatching. |
| **Reverse Logistics** | `customer_returns`, `customer_return_shipping_charges`, `customer_return_financial_events`, `return_items`, `return_price_reviews`, `supplier_return_requests`, `supplier_return_parcels`, `supplier_return_parcel_items`, `supplier_return_back_requests`, `supplier_return_back_events`, `supplier_return_events`, `supplier_return_financial_events`, `disputes` | 7-day return claims, damaged goods arbitration, return parcel routing, return shipping fee allocation, and dispute resolution actions. |
| **Ledger & Accounting** | `ledger_entries`, `finance_adjustments`, `payout_requests`, `paya_batch_metadata`, `monthly_settlement_cycles`, `monthly_settlement_items`, `reconciliation_records`, `historical_reconciliation_reviews` | Strictly append-only, double-entry bookkeeping journal entries (`debit`/`credit`), bank Paya transfer batches, and merchant/supplier monthly settlements. |
| **Shipping & Tariffs** | `tapin_boxes`, `tapin_geo_snapshots`, `tapin_tariff_rates`, `tapin_tariff_surcharges`, `tapin_tariff_versions`, `carrier_configs`, `carrier_tariffs` | Courier tariffs, parcel box definitions, postal code zone matrices, and Tapin upstream synchronization snapshots. |
| **Platform Ops & CMS** | `platform_settings`, `platform_pct_products`, `platform_pct_categories`, `pending_approvals`, `banners`, `homepage_sections`, `homepage_section_history`, `site_content`, `static_pages`, `newsletter_subscribers`, `support_tickets`, `support_ticket_messages`, `audit_logs`, `job_runs`, `notification_outbox`, `search_queries`, `search_term_daily`, `product_view_daily`, `store_view_daily`, `view_dedupe_daily`, `ad_event_rejections` | System governance settings, marketing banners, CMS pages, customer support tickets, scheduled job execution logs, and analytics aggregations. |

### 3.2 Critical Schema DDL Examples

#### A. Immutable Double-Entry Ledger (`ledger_entries`)
```sql
CREATE TABLE IF NOT EXISTS ledger_entries (
    id VARCHAR(50) PRIMARY KEY,
    date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    target_account VARCHAR(150) NOT NULL,
    target_type VARCHAR(20) NOT NULL CHECK (target_type IN ('seller', 'supplier', 'carrier', 'platform', 'warehouse')),
    type VARCHAR(10) NOT NULL CHECK (type IN ('debit', 'credit')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    reason TEXT NOT NULL,
    operator VARCHAR(100) NOT NULL,
    related_order_or_dispute VARCHAR(50),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_target ON ledger_entries (target_account, target_type);
CREATE INDEX IF NOT EXISTS idx_ledger_date ON ledger_entries (date DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_related ON ledger_entries (related_order_or_dispute);
```

#### B. Orders Contract & Snapshots (`orders` and `order_items`)
```sql
CREATE TABLE IF NOT EXISTS orders (
    id VARCHAR(50) PRIMARY KEY,
    order_number VARCHAR(30) UNIQUE NOT NULL,
    customer_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    store_id VARCHAR(50) REFERENCES stores(id),
    store_name VARCHAR(100) NOT NULL,
    total_amount BIGINT NOT NULL,
    discount_amount BIGINT NOT NULL DEFAULT 0,
    payable_amount BIGINT NOT NULL,
    wallet_amount BIGINT NOT NULL DEFAULT 0,
    gateway_amount BIGINT NOT NULL DEFAULT 0,
    payment_method VARCHAR(20) NOT NULL DEFAULT 'cash' CHECK (payment_method IN ('cash', 'installments', 'wallet')),
    payment_status VARCHAR(20) NOT NULL DEFAULT 'unpaid' CHECK (payment_status IN ('unpaid', 'paid', 'refunded')),
    customer_full_name VARCHAR(100) NOT NULL,
    customer_phone VARCHAR(20) NOT NULL,
    customer_province VARCHAR(50) NOT NULL,
    customer_city VARCHAR(50) NOT NULL,
    customer_address TEXT NOT NULL,
    customer_postal_code VARCHAR(20) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending', 'processing', 'shipped', 'delivered_unconfirmed',
        'delivered_confirmed', 'delivered', 'delivery_disputed', 'cancelled'
    )),
    tracking_code VARCHAR(50) UNIQUE NOT NULL,
    shipping_cost BIGINT NOT NULL DEFAULT 0,
    dropshipper_profit BIGINT NOT NULL DEFAULT 0,
    platform_profit BIGINT NOT NULL DEFAULT 0,
    profit_released BOOLEAN NOT NULL DEFAULT FALSE,
    profit_released_at TIMESTAMPTZ,
    shipped_at TIMESTAMPTZ,
    confirmed_at TIMESTAMPTZ,
    return_window_ends_at TIMESTAMPTZ,
    fulfillment_method VARCHAR(30) NOT NULL DEFAULT 'warehouse' CHECK (fulfillment_method IN ('warehouse', 'supplier_direct', 'mixed')),
    inventory_captured BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_order_payment_sum CHECK (wallet_amount + gateway_amount <= payable_amount)
);

CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id VARCHAR(50) REFERENCES orders(id) ON DELETE CASCADE,
    product_id VARCHAR(50) REFERENCES products(id),
    variant_id VARCHAR(50) REFERENCES product_variants(id),
    variant_name VARCHAR(100),
    variant_spnc VARCHAR(50),
    quantity INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    wholesale_cost BIGINT NOT NULL,   -- Snapshot Wholesale Cost (C)
    platform_fee BIGINT NOT NULL,     -- Snapshot Platform Fee (F)
    seller_margin BIGINT NOT NULL,    -- Snapshot Seller Margin (D)
    final_unit_price BIGINT NOT NULL  -- Final Customer Price: P = C + F + D
);
```

#### C. User Capabilities & Dynamic Permissions (`user_capabilities`)
```sql
CREATE TABLE IF NOT EXISTS user_capabilities (
    id VARCHAR(50) PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    capability VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'pending', 'revoked')),
    source_type VARCHAR(50) NOT NULL,
    source_id VARCHAR(100),
    granted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_capability UNIQUE (user_id, capability)
);
```

---

## 4. Authentication, Authorization & Role-Based Access Control (RBAC)

Sellersho employs a decoupled, multi-role identity architecture supporting dual authentication channels:

```
                           +------------------------+
                           |  Authentication Entry  |
                           +------------------------+
                                        |
                   +--------------------+--------------------+
                   |                                         |
                   v                                         v
       [ OTP Authentication ]                    [ Password Authentication ]
       (All Users: Buyer/Seller/                  (Strictly Restricted to
        Supplier/Staff/Admin)                     Super-Admin Mobile Allowlist)
                   |                                         |
                   v                                         v
       +-----------------------+                 +-----------------------+
       | - Normalize Mobile    |                 | - Validate Allowlist  |
       | - 30s Cooldown        |                 | - Bcrypt Compare Hash |
       | - 120s TTL in Redis   |                 | - Lockout on 5 Fails  |
       | - IPPanel SMS Pattern |                 +-----------------------+
       | - Atomic Lua Verify   |                             |
       |   (Max 5 Attempts)    |                             |
       +-----------------------+                             |
                   |                                         |
                   +--------------------+--------------------+
                                        |
                                        v
                       +----------------------------------+
                       | Generate 7-Day HMAC-SHA256 JWT   |
                       | Payload:                         |
                       | - userId, mobile, fullName       |
                       | - role (Selected Active Role)    |
                       | - capabilities: []string         |
                       +----------------------------------+
                                        |
                                        v
                       +----------------------------------+
                       | Per-Request Route Middleware     |
                       | 1. Validate JWT Signature        |
                       | 2. RefreshCapabilities (DB Query)|
                       | 3. IsUserBlocked (is_active=F)   |
                       | 4. RequireRoles / Capabilities   |
                       +----------------------------------+
```

### 4.1 OTP Authentication Flow (Primary)
1. **Send OTP (`POST /api/v1/auth/otp/send`):**
   - Normalizes Iranian phone numbers (converts `+98`, `0098`, spaces, or dashes into `09xxxxxxxxx`).
   - Validates Iranian 11-digit regex: `^09[0-9]{9}$`.
   - Checks 30-second cooldown via Redis `SETNX otp:cooldown:09xxxxxxxxx 1 EX 30`. Returns `429 Too Many Requests` if active.
   - Generates cryptographically secure 4-digit numeric code using `crypto/rand` ($1000 \le \text{code} \le 9999$).
   - Stores code in Redis under `otp:09xxxxxxxxx` with a strict **120-second (2-minute) TTL**.
   - Dispatches SMS via **IPPanel Pattern API** asynchronously and logs event into `sms_logs`.
2. **Verify OTP (`POST /api/v1/auth/otp/verify`):**
   - Executes an **atomic Redis Lua script** evaluating attempts:
     ```lua
     local code = redis.call('GET', KEYS[1])
     if not code then return 0 end
     if tonumber(redis.call('GET', KEYS[2]) or '0') >= 5 then return 0 end
     if code == ARGV[1] then
       redis.call('DEL', KEYS[1], KEYS[2], KEYS[3])
       return 1
     end
     local attempts = redis.call('INCR', KEYS[2])
     redis.call('EXPIRE', KEYS[2], 120)
     if attempts >= 5 then redis.call('DEL', KEYS[1], KEYS[3]) end
     return 0
     ```
   - If the code matches, it deletes the OTP and cooldown immediately, preventing replay attacks.
   - Upserts the user into the `users` table. If the user is new, automatically provisions the `customer` capability.
   - If the user's mobile is in `accesspolicy.SuperAdminMobiles()`, automatically provisions the `super_admin` capability.
   - If the caller supplies `role`, checks if that role is active in `user_capabilities`. If not, returns `403 Forbidden`.
   - If `storeSlug` is supplied for a customer, idempotently binds the buyer to the merchant in `customer_store_links`.
   - Emits a signed **HMAC-SHA256 JWT** valid for 7 days.

### 4.2 Password Authentication Flow (Super-Admin Emergency Channel)
- Route: `POST /api/v1/auth/password/login`
- Access is strictly gated in Go code before database lookup:
  ```go
  if !accesspolicy.IsSuperAdminMobile(req.Username) {
      http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
      return
  }
  ```
- Allowed Super-Admin Mobiles: `09933070870`, `09127504005`.
- Evaluates bcrypt hashed password (`bcrypt.CompareHashAndPassword`).
- Tracks consecutive login failures; automatically applies a lockout duration in `users.locked_until`.

### 4.3 JWT Claim Structure & Dynamic Revocation
The JWT payload:
```json
{
  "userId": "d7a1b32f-5192-41f8-9a37-4d7a8d5423f1",
  "mobile": "09123456789",
  "fullName": "علی رضایی",
  "role": "seller",
  "capabilities": ["customer", "seller"],
  "exp": 1731628800,
  "iat": 1731024000,
  "iss": "drup-platform"
}
```
**Dynamic Mid-Session Security:**
Even though tokens are valid for 7 days, the `AuthMiddleware` executes two real-time checks on every protected request:
1. `jwtService.RefreshCapabilities(ctx, claims)`: Re-queries `user_capabilities` for the user's active membership set. If an admin revoked the user's capability, the updated claims instantly reflect it.
2. `jwtService.IsUserBlocked(ctx, claims.UserID)`: Checks `users.is_active`. If `false`, rejects the request immediately with `403 Forbidden` (`{"error":"Account is disabled"}`).

### 4.4 Supported Platform Roles
- `customer`: Storefront consumer, places orders, manages addresses, submits returns.
- `seller`: Dropshipper merchant, manages storefront products, pricing overrides, ad campaigns, and payout withdrawals.
- `supplier`: Wholesale vendor, proposes catalog products, manages warehouse inventory batches, accepts direct dropship shipments, prints Tapin labels.
- `warehouse_staff`: Central warehouse operator, receives inbounds, scans barcodes, runs QC checks, assigns putaway bins, picks, packs, and dispatches manifests.
- `logistics_specialist`: Warehouse supervisor, manages bin capacities, carrier tariffs, and physical inventory reconciliations.
- `finance_specialist`: Financial accountant, reviews payout requests, executes manual bank transfers, audits ledger adjustments, and resolves delivery disputes.
- `support_specialist`: Customer service representative, handles customer tickets and dispute mediation.
- `admin`: Platform administrator, manages store approvals, supplier approvals, catalog review dossiers, margin locks, platform percentage settings, and user blocking.
- `super_admin`: System owner, possesses unrestricted administrative overrides and capability grant/revoke authority.

---

## 5. Canonical Pricing & Margin Engine

The platform implements a **Percent-Only Reference Pricing Engine** (`backend/internal/pricing/reference_pricing.go`).

### 5.1 The Mathematical Model
For any product sold on a storefront:
1. **Wholesale Cost ($C$):** Base cost set by the supplier in Tomans.
2. **Platform Percentage Ladder ($\text{platformPct}$):** Determined hierarchically:
   $$\text{platformPct} = \text{Product Override} \succ \text{Category Override} \succ \text{Global Setting}$$
3. **Reference Cost ($R$):**
   $$R = \text{RoundTo100}\left(C \times \left(1 + \frac{\text{platformPct}}{100}\right)\right)$$
4. **Platform Fee ($F$):**
   $$F = R - C$$
   *(When $\text{platformPct} = 0$, $R = C$ and $F = 0$)*
5. **Applied Seller Margin ($\text{margin}$):** Evaluated according to precedence rules (see below).
6. **Final Unit Price ($P$):**
   $$P = \text{RoundTo100}\left(R \times \left(1 + \frac{\text{margin}}{100}\right)\right)$$
7. **Dropshipper Margin Amount ($D$):**
   $$D = P - R$$
8. **Central Store Price ($P_{\text{central}}$):** When the store is `owner_type = 'central'`:
   $$P_{\text{central}} = \text{RoundTo100}\left(C \times \left(1 + \frac{\text{centralProfitPct}}{100}\right)\right) \quad (\text{Default } \text{centralProfitPct} = 10\%)$$
9. **Derived Display Discount Percentage:**
   $$\text{discountPercent} = \text{round}\left(\frac{\text{OriginalPrice} - P}{\text{OriginalPrice}} \times 100\right)$$
   *(Note: This is a derived visual comparison against the anchor `original_price`, not a merchant-funded coupon).*

### 5.2 Seller Margin Precedence Rules
When pricing an item for a dropshipper storefront, the margin percentage is resolved in strict order:
1. **Admin Margin Lock (`store_margin_locks`):** Mode `admin_set` with an explicit percentage. Bypasses seller controls and margin ceilings.
2. **Product Percent Override (`store_product_overrides`):** Explicit percentage configured by the seller for the product. Clamped to `effectiveMarginCeiling`.
3. **Admin Store Margin Lock (`stores.is_margin_locked_by_admin`):** When locked by admin, `stores.custom_store_margin_override` overrides the fallback.
4. **Max-Allowed Default Fallback:** If no specific margin is set, the system automatically assigns the maximum permissible margin:
   $$\text{margin} = \begin{cases} \text{product\_margin\_ceilings}[p] & \text{if ceiling exists} \\ \text{platform\_settings.max\_profit\_margin} & \text{otherwise (default 35\%)} \end{cases}$$

### 5.3 Concrete Calculation Example
- Supplier Base Wholesale Cost ($C$): **1,200,000 Tomans**
- Original Anchor Price: **1,650,000 Tomans**
- Global $\text{platformPct}$: **0%** $\implies R = 1,200,000$, $F = 0$
- Seller sets **15%** product margin:
  $$P = \text{RoundTo100}(1,200,000 \times 1.15) = 1,380,000 \text{ Tomans}$$
  $$D = 1,380,000 - 1,200,000 = 180,000 \text{ Tomans (Seller Profit)}$$
  $$\text{discountPercent} = \text{round}\left(\frac{1,650,000 - 1,380,000}{1,650,000} \times 100\right) = 16\%$$

---

## 6. Comprehensive Backend API Route Registry

The following table provides the exhaustive catalogue of the platform's backend HTTP endpoints, specifying paths, methods, required authorization roles, and handler purposes.

### 6.1 Authentication & User Session
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `POST` | `/api/v1/auth/otp/send` | Public | Send 4-digit SMS OTP. Body: `{"mobile": "0912..."}`. Returns: `{"success": true, "expiresIn": 120}`. |
| `POST` | `/api/v1/auth/otp/verify` | Public | Verify OTP and authenticate. Body: `{"mobile": "...", "code": "1234", "role": "seller", "storeSlug": "..."}`. Returns JWT + User details. |
| `POST` | `/api/v1/auth/password/login` | Super-Admin Only | Password login for allowlisted admin mobiles. Body: `{"username": "09933070870", "password": "..."}`. |
| `POST` | `/api/v1/auth/supplier/register` | Public | Register new vendor. Body: `{"mobile": "...", "name": "...", "warehouseCity": "تهران"}`. |
| `GET` | `/api/v1/auth/me` | Authenticated | Inspect active JWT claims and capabilities. |
| `POST` | `/api/v1/auth/logout` | Authenticated | Invalidate local session state. |

### 6.2 Public Catalog & Storefront CMS
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `GET` | `/api/health` | Public | System health check (PostgreSQL status, Redis ping, environment, timestamp). |
| `GET` | `/api/v1/categories` | Public (Cached 1h) | List all root and child taxonomy categories. |
| `GET` | `/api/v1/niches?category={slug}` | Public (Cached 1h) | List target market niches. |
| `GET` | `/api/v1/products` | OptionalAuth | Paginated catalog listing with search, category, brand, and niche filters. Unlocks wholesale costs if caller is vendor/admin. |
| `GET` | `/api/v1/products/{idOrSlug}` | OptionalAuth | Product detail view with specifications, images, and variants. |
| `GET` | `/api/v1/products/{id}/reviews` | Public | Public verified reviews for product. |
| `GET` | `/api/v1/products/price-bounds` | Public | Returns min and max price boundaries across active catalog. |
| `GET` | `/api/v1/brands` | Public | List all distinct product brands in catalog. |
| `GET` | `/api/v1/categories/{slug}` | Public | Category facet options (Digikala attribute facets, brands, colors). |
| `GET` | `/api/v1/stores` | Public | List active dropshipper storefronts. |
| `GET` | `/api/v1/stores/{slug}` | Public | Single store presentation view (logo, templateId, templateConfig). |
| `POST` | `/api/v1/stores/{slug}/discount-quote` | Public | Read-only calculation of discount codes or campaigns without consuming code. |
| `GET` | `/api/v1/banners` | Public | Active promotional banners for homepage and catalog. |
| `GET` | `/api/v1/homepage-sections` | Public | Dynamic homepage widgets and layouts. |
| `GET` | `/api/v1/site-content` | Public | General site configurations and footer links. |
| `GET` | `/api/v1/static-pages` | Public | Informational CMS pages (About Us, Terms, Privacy, FAQ). |
| `GET` | `/api/v1/store-templates` | Public | Active storefront theme templates list. |
| `POST` | `/api/v1/newsletter/subscribe` | Public | Subscribe customer mobile or email to merchant newsletter. |
| `POST` | `/api/v1/newsletter/unsubscribe` | Public | Unsubscribe from store updates via token. |
| `POST` | `/api/v1/views/events` | Public | Telemetry tracking for product and store views. |
| `GET` | `/api/v1/ads/click?ad_token=...` | Public | Anti-fraud ad click token verification (24h Redis deduplication). |

### 6.3 Customer Profile, Cart & Orders
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `GET` | `/api/v1/geo/provinces` | Customer / Authenticated | List all 31 Iranian provinces. |
| `GET` | `/api/v1/geo/cities?province={id}`| Customer / Authenticated | List cities within selected province. |
| `GET`, `POST` | `/api/v1/addresses` | Customer | List saved addresses or create new shipping address. |
| `PUT`, `DELETE`| `/api/v1/addresses/{id}` | Customer | Update or delete saved address. |
| `GET`, `POST` | `/api/v1/wishlist` | Customer | List or toggle saved wishlist items (per store). |
| `POST` | `/api/v1/shipping/quote` | Customer / Seller / Admin | Real-time pre-checkout shipping cost calculation based on cart weight, origin warehouses, and destination city. |
| `POST` | `/api/v1/orders` | Customer / Seller / Admin | Place new customer order. Performs stock validation, locks inventory, splits shipping legs, and returns order number. |
| `GET` | `/api/v1/orders` | Customer / Seller / Admin | List user's placed orders with status and financial snapshots. |
| `GET` | `/api/v1/orders/track/{trackingCode}`| Public | Public order tracking timeline by tracking code. |
| `GET` | `/api/v1/orders/{id}/tracking` | Customer / Seller / Admin | Detailed shipment carrier tracking numbers and milestones. |
| `GET` | `/api/v1/orders/{id}/shipping` | Customer / Seller / Admin | Detailed shipping leg breakdown and origin assignments. |
| `POST` | `/api/v1/orders/{id}/cancel` | Customer / Admin | Cancel unfulfilled order and release inventory. |
| `POST` | `/api/v1/orders/confirm-delivery/{id}`| Customer | Customer manual delivery confirmation. |
| `POST` | `/api/v1/orders/{id}/return-quote`| Customer | Pre-return financial estimation of refundable items and return shipping charges. |
| `POST` | `/api/v1/orders/{id}/return` | Customer | Initiate 7-day return request for delivered order. |
| `GET`, `POST` | `/api/v1/returns` | Customer / Admin | List returns or submit return claim. |
| `POST` | `/api/v1/returns/{id}/{action}` | Customer / Staff / Admin | Actions: `approve`, `reject`, `receive`, `refund`, `cancel`, `ship`. |
| `GET`, `POST` | `/api/v1/customer/wallet` | Customer | Inspect customer wallet balance or view deposit history. |
| `POST` | `/api/v1/customer/wallet/withdrawals`| Customer | Request withdrawal from customer wallet to Sheba IBAN. |
| `GET`, `POST` | `/api/v1/notifications` | Customer | In-app notification inbox. |

### 6.4 Seller Dashboard & Storefront Management
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `GET` | `/api/v1/stores/mine` | Customer/Seller/Supplier/Admin | List stores owned by authenticated user. |
| `POST` | `/api/v1/stores/register` | Authenticated | Create new dropshipper storefront; grants `seller` capability. |
| `GET`, `PUT` | `/api/v1/stores/{slug}/settings` | Seller | Read or update store details, custom domain, tagline, and logo. |
| `GET`, `PUT` | `/api/v1/stores/{slug}/pricing` | Seller / Admin | Update product/niche margin overrides. |
| `GET` | `/api/v1/stores/{slug}/orders` | Seller | List customer orders placed on merchant storefront (customer details masked). |
| `GET` | `/api/v1/stores/{slug}/returns` | Seller | Returns and reverse logistics reports for merchant. |
| `GET` | `/api/v1/stores/{slug}/referrals` | Seller | Referral network status, downstream stores, and earned bonuses. |
| `GET`, `POST` | `/api/v1/stores/{slug}/discount-campaigns` | Seller | Manage seller discount campaigns (scope: product, niche, store). |
| `GET` | `/api/v1/stores/{slug}/subscribers`| Seller | View newsletter subscribers collected via storefront. |
| `PUT` | `/api/v1/stores/{slug}/ad-widget` | Seller / Admin | Configure storefront promotional deals widget. |
| `GET`, `PUT` | `/api/v1/seller/store/template` | Seller | Manage active storefront theme template and JSON layout configuration. |
| `POST` | `/api/v1/seller/store/template/publish`| Seller | Publish draft template configuration to live storefront. |
| `GET` | `/api/v1/seller/analytics` | Seller | Sales revenue, conversion rates, visitor traffic, and top products. |
| `GET` | `/api/v1/seller/search-insight` | Seller | High-intent customer search queries and zero-result search analytics. |
| `GET` | `/api/v1/seller/variant-overrides`| Seller | View per-variant price adjustments. |
| `GET` | `/api/v1/seller/pricing-settings`| Seller | Effective pricing bounds and platform margin settings for merchant. |

### 6.5 Supplier & Wholesale Vendor Panel
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `GET`, `PUT` | `/api/v1/supplier/profile` | Supplier | View or update supplier profile, logo, description, and warehouse city. |
| `GET`, `POST`| `/api/v1/supplier/team` | Supplier | Manage vendor team members, role assignments, and permissions. |
| `GET`, `POST`| `/api/v1/supplier/products` | Supplier | List supplier's wholesale catalog items or submit new product proposal. |
| `PUT` | `/api/v1/supplier/products/{id}/stock` | Supplier | Real-time stock update for catalog product. |
| `PUT` | `/api/v1/supplier/products/{id}/pricing` | Supplier | Update wholesale base cost ($C$). If increase exceeds platform cap, creates governance approval ticket. |
| `GET`, `POST`| `/api/v1/supplier/products/{id}/variants` | Supplier | Manage product variants (colors, sizes, attributes, SPNC barcodes). |
| `POST` | `/api/v1/supplier/products/{id}/images` | Supplier | Upload and attach product images via multipart form. |
| `GET`, `POST`| `/api/v1/supplier/inbounds` | Supplier | Schedule inbound stock delivery to central warehouse. |
| `POST` | `/api/v1/supplier/inbounds/{id}/dispatch`| Supplier | Dispatch inbound consignment with tracking barcode. |
| `GET` | `/api/v1/supplier/orders` | Supplier | Orders requiring supplier fulfillment or wholesale dispatch. |
| `POST` | `/api/v1/supplier/orders/{id}/shipping` | Supplier | Submit shipping courier tracking code or generate Tapin waybill. |
| `GET` | `/api/v1/supplier/shipments/{id}/tapin-label` | Supplier | Stream PDF/ZPL shipping label generated via Tapin. |
| `GET` | `/api/v1/supplier/settlement/balance` | Supplier | Unsettled wholesale balance, pending deductions, and payable totals. |
| `POST` | `/api/v1/supplier/settlement/request`| Supplier | Request wholesale balance payout to verified Sheba account. |
| `GET`, `PUT` | `/api/v1/supplier/tapin/settings` | Supplier | Configure Tapin upstream API token and shop ID. |
| `POST` | `/api/v1/supplier/tapin/test` | Supplier | Test upstream connection to `api.tapin.ir`. |
| `GET`, `POST`| `/api/v1/supplier/warehouses` | Supplier | Register supplier warehouse origin locations for direct fulfillment. |
| `POST` | `/api/v1/supplier/exclusivity-challenges`| Supplier | Challenge exclusive brand or catalog distribution claims. |

### 6.6 Warehouse Management System (WMS)
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `GET` | `/api/v1/warehouse/overview` | Warehouse Staff / Logistics / Admin | Warehouse inventory metrics, occupied positions, and inbound summary. |
| `GET` | `/api/v1/warehouse/bins` | Warehouse Staff / Logistics / Admin | Bin hierarchy (Aisle, Shelf, Level, Bin code) and capacity utilization. |
| `PUT` | `/api/v1/warehouse/bins/{code}/capacity`| Logistics Specialist / Admin | Update bin maximum volumetric capacity and storage classes. |
| `GET` | `/api/v1/warehouse/inbounds/pending`| Warehouse Staff | Inbound consignments awaiting arrival and dock receiving. |
| `POST` | `/api/v1/warehouse/inbounds/{id}/start-receiving` | Warehouse Staff | Dock check-in for supplier delivery truck. |
| `POST` | `/api/v1/warehouse/inbounds/{id}/scan-item` | Warehouse Staff | Scan individual product barcode/SPNC during receiving. |
| `POST` | `/api/v1/warehouse/inbounds/{id}/complete` | Warehouse Staff | Finalize inbound dock receiving. |
| `GET` | `/api/v1/warehouse/qc/queue` | Warehouse Staff | Received products awaiting quality inspection. |
| `POST` | `/api/v1/warehouse/qc/{id}/pass` | Warehouse Staff | Mark batch item as QC passed. Moves to putaway queue. |
| `POST` | `/api/v1/warehouse/qc/{id}/fail` | Warehouse Staff | Flag defective or non-compliant item; creates `qc_discrepancy`. |
| `GET` | `/api/v1/warehouse/putaway/queue`| Warehouse Staff | QC-passed items awaiting bin storage assignment. |
| `POST` | `/api/v1/warehouse/putaway/{id}/assign`| Warehouse Staff | Suggest or assign target bin position for inventory putaway. |
| `POST` | `/api/v1/warehouse/putaway/{id}/confirm`| Warehouse Staff | Confirm physical bin placement and increment active inventory balance. |
| `GET` | `/api/v1/warehouse/orders/ready-to-pick`| Warehouse Staff | Orders ready for wave picking. |
| `POST` | `/api/v1/warehouse/orders/{id}/start-picking` | Warehouse Staff | Lock order and generate pick list route. |
| `POST` | `/api/v1/warehouse/orders/{id}/pack` | Warehouse Staff | Scan packed items, record packaging box dimensions and tare weight. |
| `POST` | `/api/v1/warehouse/orders/{id}/ship` | Warehouse Staff | Handover parcel to carrier and generate tracking label. |
| `GET`, `POST`| `/api/v1/warehouse/manifests` | Warehouse Staff / Logistics | Generate carrier dispatch manifest for bulk postal pickup. |

### 6.7 Finance, Ledger & Payout Operations
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `GET` | `/api/v1/finance/ledger` | Finance Specialist / Admin | Filter immutable double-entry ledger journal entries by date, account, or type. |
| `GET` | `/api/v1/finance/stores/{id}/statement` | Finance Specialist / Admin | Merchant financial statement (credits, debits, hold balances). |
| `GET` | `/api/v1/finance/settlements/pending`| Finance Specialist / Admin | Pending payout requests from merchants and suppliers. |
| `POST` | `/api/v1/finance/settlements/{id}/approve` | Finance Specialist / Admin | Approve payout request for bank processing. |
| `POST` | `/api/v1/finance/settlements/{id}/transfer-confirmed` | Finance Specialist / Admin | Record completed bank Paya transfer reference and write ledger debit. |
| `POST` | `/api/v1/finance/adjustments` | Finance Specialist / Admin | Manual financial adjustment with mandatory audit reason and ledger entry. |
| `GET` | `/api/v1/finance/reports/revenue` | Finance Specialist / Admin | Platform gross merchandise value (GMV), net margin, and fee revenue reports. |

### 6.8 Platform Administration & Governance
| Method | Endpoint | Role Guard | Description / Request Body / Response |
|---|---|---|---|
| `GET`, `POST`| `/api/v1/admin/approvals` | Admin | Centralized approvals inbox for store slugs, custom domains, and products. |
| `POST` | `/api/v1/admin/approvals/{id}/review`| Admin | Approve or reject pending entity request. |
| `GET` | `/api/v1/admin/audit-logs` | Admin | Comprehensive security and administrative audit trail. |
| `GET` | `/api/v1/admin/users` | Admin | List all registered platform users with capability filters. |
| `POST` | `/api/v1/admin/users/{id}/block` | Admin | Instantly disable user account (`is_active = FALSE`). |
| `POST` | `/api/v1/admin/users/{id}/unblock`| Admin | Re-enable disabled user account. |
| `GET`, `PUT` | `/api/v1/admin/settings/pricing` | Admin | View or update platform global pricing settings, profit bounds, and ladder. |
| `GET`, `POST`| `/api/v1/admin/price-increases` | Admin | Review supplier wholesale price increase requests. |
| `POST` | `/api/v1/admin/price-increases/{id}/review`| Admin | Approve or reject vendor price increase. |
| `GET` | `/api/v1/admin/jobs` | Admin | Inspect status of background cron workers and scheduled tasks. |
| `POST` | `/api/v1/admin/jobs/{id}/run` | Admin | Trigger immediate out-of-band execution of scheduled background task. |
| `POST` | `/api/v1/admin/capabilities/grant`| Super-Admin Only | Explicitly grant capability to user. |
| `POST` | `/api/v1/admin/capabilities/revoke`| Super-Admin Only | Revoke capability from user. |

---

## 7. External Integrations & Third-Party Services

### 7.1 SMS Gateway: IPPanel (`backend/internal/ippanel/client.go`)
- **Upstream Host:** `https://edge.ippanel.com/v1`
- **Method:** `POST /api/send`
- **Authentication:** Token-based header (`Authorization: <api-key>`).
- **Delivery Strategy:** **Pattern-Based SMS (پترن خدماتی)**. Guarantees immediate OTP delivery to blacklisted / promotional-blocked mobile numbers without passing through telecom blacklists.
- **Request Structure:**
  ```json
  {
    "sending_type": "pattern",
    "from_number": "+983000...",
    "code": "otp-template-pattern-id",
    "recipients": ["989123456789"],
    "params": {
      "code": "4892"
    }
  }
  ```
- **Network Resilience:** Uses dedicated HTTP transport bypassing OS-level proxies to ensure uninterrupted Iranian telecom gateway access.

### 7.2 Payment Gateways: Shaparak PSP (`backend/internal/payment/gateway.go`)
- **Supported Mode:** Shaparak PSP integration (e.g. Asan Pardakht, Saman, Mellat) with dual operational modes:
  - `real`: Live PSP endpoint connectivity with cryptographic terminal verification.
  - `mock`: Configurable simulation mode for local development and CI testing.
- **Atomic Two-Phase Inventory Hold:**
  Before issuing a Shaparak payment token (`/api/v1/payments/shaparak/token`), the system executes `reserveOrderInventory` against Redis:
  1. Checks stock availability across all line items.
  2. Sets temporary Redis reservation keys with a 15-minute expiration:
     `inventory:hold:order:{orderId}:product:{productId}`.
  3. If another concurrent customer consumes the remaining stock, the reservation fails with `409 Conflict` (`insufficient_stock`), preventing overselling.
- **Callback Processing (`HandleShaparakCallbackHTTP`):**
  - Upon gateway redirect, verifies the transaction token and expected amount with the PSP.
  - If verified, commits permanent inventory reduction in PostgreSQL (`inventory_balances` and `order.inventory_captured = TRUE`), deletes the temporary Redis holds, transitions order status to `processing`, and logs payment RRN and masked card number (`6037-99**-****-1234`).
- **Sheba Bank Account Validation (`/api/v1/bank/verify-sheba`):**
  - Evaluates Iranian Sheba IBANs (`IR...`) using checksum validation and bank identification lookup.

### 7.3 Shipping & Logistics: Tapin / Tipax Gateway (`backend/internal/tapin/`)
- **Upstream Host:** `https://api.tapin.ir`
- **Authentication:** Bearer token (`Authorization: Bearer <tapin-token>`).
- **API Envelope Contract:** All requests return the standard envelope:
  ```json
  {
    "returns": {
      "status": 20,
      "message": "موفق"
    },
    "entries": { ... }
  }
  ```
  Success statuses: `20` (Read/Register) and `21` (Status change). Real failure statuses: `332` (token not found), `338` (no shop selected), `30129` (Tipax service not activated for the merchant).
- **Core Logistics Capabilities:**
  - `TipaxCheckPrice`: Queries real-time courier tariffs based on origin/destination province/city IDs, weight in grams, dimensions ($L \times W \times H$), product value, and packing type.
  - `TipaxRegister`: Books parcel pickup, registers recipient contact information, and generates upstream tracking codes.
  - `Parcel Label Proxy`: Directly streams carrier PDF shipping labels (`/api/v1/supplier/shipments/{id}/tapin-label`) to warehouse thermal label printers without leaking upstream credentials.

### 7.4 Media Storage & Product Upload Handler (`upload_handlers.go`)
- **Route:** `POST /api/v1/upload/product-image`
- **Role Requirement:** Authenticated `supplier` or `admin`.
- **Security Protections:**
  1. **Payload Size Bound:** Request body capped at 10MB via `http.MaxBytesReader` to prevent denial-of-service memory spikes. File size capped at 5MB.
  2. **Extension Whitelist:** Only `.jpg`, `.jpeg`, `.png`, and `.webp` allowed.
  3. **Strict Magic-Bytes Inspection (Content Sniffing):** Reads the first 512 bytes to verify magic numbers:
     - JPEG: `FF D8 FF`
     - PNG: `89 50 4E 47 0D 0A 1A 0A`
     - WEBP: `RIFF....WEBP`
     Executable files masked as images are rejected immediately.
  4. **Filename Sanitization:** The original client filename is discarded. Files are renamed using a UUID + timestamp format: `uuid_timestamp.ext`.
  5. **Static File Serving:** Stored locally in `./static/uploads/products/` and served via Nginx `/static/` reverse proxy.

---

## 8. Asynchronous Workers, Scheduling & Event Handlers

The platform runs an in-process, multi-loop background worker (`backend/internal/worker/scheduler.go`) designed to handle business timeouts and financial state machines without external cron dependencies.

```
                           +--------------------------+
                           | BackgroundWorker (Go)   |
                           +--------------------------+
                                        |
                 +----------------------+----------------------+
                 | (Every 60 Seconds)                          | (Hourly)
                 v                                             v
     +-----------------------+                    +-----------------------+
     | Staged Delivery Lock  |                    | Search Query Rollup   |
     | Releaser Cycle        |                    | (Archive queries and  |
     +-----------------------+                    |  aggregate terms)     |
                 |                                +-----------------------+
         +-------+-------+
         |               |
         v               v
    [ Stage 1 ]     [ Stage 2 ]
    After 20 days   After Return Window Ends
    shipped_at:     (return_window_ends_at <= NOW()):
    - Move to       - Flip profit_released = TRUE
      delivered_    - Release seller margin D
      confirmed     - Release referral commissions
    - Stamp window  - Write double-entry ledger:
      (confirmed_at   * Debit Platform
       + 7 days)      * Credit Seller Balance
                    - Record run in job_runs
```

### 8.1 Two-Stage Staged Profit Release Cycle
To protect consumers while ensuring vendor liquidity, merchant profits are released in two discrete stages:
1. **Stage 1 (Auto-Confirmation):**
   - Scans orders in status `shipped` where $\text{shipped\_at} \le \text{NOW}() - \text{delivery\_hold\_period\_days}$ (default 20 days).
   - Automatically transitions status to `delivered_confirmed`.
   - Sets `confirmed_at = NOW()` and calculates the closing of the consumer return window:
     $$\text{return\_window\_ends\_at} = \text{confirmed\_at} + 7 \text{ days}$$
   - *Profit remains locked during this stage.*
2. **Stage 2 (Financial Release & Ledger Write):**
   - Scans orders in `delivered_confirmed` where `profit_released = FALSE` and $\text{return\_window\_ends\_at} \le \text{NOW}()$.
   - Executes an atomic transaction:
     - Sets `profit_released = TRUE` and `profit_released_at = NOW()`.
     - Releases `referral_commission_released = TRUE`.
     - Inserts double-entry journal records into `ledger_entries`:
       - **Credit Seller:** Target type `seller`, amount $D$, reason `"Automated profit release after return window closed"`.
       - **Debit Platform:** Target type `platform`, amount $D$.
   - Records execution metrics (duration, confirmed count, released count) in `job_runs` under `job_id = 'delivery-lock-releaser'`.

### 8.2 Automated Monthly Settlement Sweep (`settlement.go`)
- Runs every 60 seconds and once upon worker boot.
- Checks if the current day matches `platform_settings.settlement_day` (e.g. the 10th of each Shamsi/Gregorian month).
- Aggregates all unsettled ledger balances for active sellers and suppliers.
- Creates records in `monthly_settlement_cycles` and line items in `monthly_settlement_items`.
- Ensures idempotency: will only execute once per month.

---

## 9. Infrastructure, Deployment & Nginx Reverse Proxy

### 9.1 Multi-Service Docker Architecture (`docker-compose.yml`)
The platform services run in a coordinated Docker network:
- `postgres` (`postgres:16-alpine`): Configured with a health check (`pg_isready -U drup_admin -d drup_db`) and persistent volume `postgres_data`.
- `redis` (`redis:7-alpine`): Configured with append-only persistence (`--appendonly yes`) and health check (`redis-cli ping`).
- `api` (Go API Container): Builds from Alpine 3.19, exposes port `8080`, and waits for PostgreSQL and Redis to report `service_healthy`.

### 9.2 Production Dockerfile (`Dockerfile`)
Multi-stage minimal image based on `alpine:3.19`:
```dockerfile
FROM alpine:3.19
WORKDIR /app
RUN mkdir -p /app/static/uploads/products
COPY bin/server-linux /app/server
COPY migrations /app/migrations
RUN chmod +x /app/server
EXPOSE 8080
CMD ["/app/server"]
```

### 9.3 Nginx Reverse Proxy & SEO Canonical Mapping (`nginx.conf`)
The Nginx configuration serves three critical production responsibilities:
1. **API & Static Proxy:**
   - Proxies `/api/` traffic to `http://backend:8080/api/` with `X-Forwarded-For` and `X-Real-IP`.
   - Proxies `/static/` traffic to `http://backend:8080/static/` with `autoindex off`.
2. **SPA Routing & Caching:**
   - Serves Vite frontend assets from `/usr/share/nginx/html`.
   - Hashed bundles in `/assets/` are cached for **1 year** with `Cache-Control: "public, immutable"`.
   - `index.html` is strictly served with `Cache-Control: "no-store, no-cache, must-revalidate"`.
   - SPA fallback: `try_files $uri $uri/ /index.html;`.
3. **Persian SEO Canonical Product URL Rewriting:**
   - Implements an optimized `map $uri $product_redirect` containing 301 permanent redirects from legacy Latin URLs to Persian canonical SPN URLs.
   - Example:
     `/product/demo-kitchen-pro-01` $\implies$ `301 /product/spn-10000058/سرویس-قابلمه-10-پارچه/`
   - Preserves original query arguments (`$is_args$args`) during redirection.

---

## 10. Concrete Code, Payload & Configuration Catalog

### 10.1 Sample Order Placement Request Payload
**Endpoint:** `POST /api/v1/orders`  
**Headers:** `Authorization: Bearer <jwt-token>`, `Content-Type: application/json`
```json
{
  "storeId": "store-tehran-tech",
  "storeName": "تهران تک",
  "paymentMethod": "cash",
  "customerFullName": "سارا محمدی",
  "customerPhone": "09121112233",
  "customerProvince": "تهران",
  "customerCity": "تهران",
  "customerAddress": "خیابان ولیعصر، بالاتر از میدان ونک، برج نگار، طبقه ۵",
  "customerPostalCode": "1969713511",
  "shippingCost": 45000,
  "items": [
    {
      "productId": "prod-earbuds-pro",
      "variantId": "var-earbuds-white",
      "quantity": 2
    }
  ]
}
```

### 10.2 Sample Order Creation Success Response
**Status:** `200 OK`
```json
{
  "id": "ord-1731024560123",
  "orderNumber": "ORD-2026-89412",
  "storeId": "store-tehran-tech",
  "totalAmount": 2760000,
  "discountAmount": 540000,
  "payableAmount": 2805000,
  "walletAmount": 0,
  "gatewayAmount": 2805000,
  "paymentStatus": "unpaid",
  "status": "pending",
  "trackingCode": "TRK-981240182",
  "shippingCost": 45000,
  "dropshipperProfit": 360000,
  "platformProfit": 0,
  "items": [
    {
      "id": "c1f72834-319a-4c91-a1b4-2e92c212879a",
      "productId": "prod-earbuds-pro",
      "productTitle": "هندزفری بلوتوثی پرو با نویزکنسلینگ",
      "variantId": "var-earbuds-white",
      "variantName": "سفید صدفی",
      "variantSpnc": "SPNC-10000003-01",
      "quantity": 2,
      "wholesaleCost": 1200000,
      "platformFee": 0,
      "sellerMargin": 180000,
      "finalUnitPrice": 1380000
    }
  ],
  "createdAt": "2026-11-07T22:45:00Z"
}
```

### 10.3 Sample Shaparak Payment Verification Request & Response
**Request to:** `POST /api/v1/payments/shaparak/token`
```json
{
  "orderId": "ord-1731024560123",
  "orderNumber": "ORD-2026-89412",
  "amount": 2805000,
  "redirectUrl": "https://sellershow.ir/order/confirmation"
}
```
**Response:**
```json
{
  "success": true,
  "token": "SHP-TKN-849102",
  "orderNumber": "ORD-2026-89412",
  "amount": 2805000,
  "message": "توکن پرداخت اینترنتی شاپرک صادر شد"
}
```

### 10.4 Sample Tapin Tipax Price Inquiry Payload & Response
**Upstream Request (`POST /api/v4/tipax/public/user/order/check-price/`):**
```json
{
  "shop_id": "tapin-shop-9812",
  "receiver_province_id": 8,
  "receiver_city_id": 301,
  "product_type_id": 1,
  "packing_type_id": 2,
  "payment_type": 1,
  "service_type": 1,
  "delivery_type": 1,
  "pickup_type": 1,
  "length": 25,
  "width": 18,
  "height": 10,
  "package_weight": 850,
  "products": [
    {
      "count": 1,
      "weight_per_count": 850,
      "amount_per_count": 28050000,
      "discount_per_count": 0
    }
  ]
}
```
**Upstream Response:**
```json
{
  "returns": {
    "status": 20,
    "message": "موفق"
  },
  "entries": {
    "send_price": 420000,
    "send_tax": 42000,
    "total_send_price": 462000,
    "total_product_price": 28050000,
    "total_panel_price": 15000,
    "total_receive_price": 28512000,
    "total_weight": 850,
    "total_discount": 0,
    "courier_discount": 0
  }
}
```

### 10.5 Sample Environment Configuration (`.env`)
```bash
# Server Runtime
APP_ENV=production
PORT=8080
ALLOWED_ORIGINS=https://sellershow.ir,https://app.sellershow.ir

# PostgreSQL Database
DATABASE_URL=postgres://drup_admin:P@ssw0rd_Prod_2026_Secure@postgres:5432/drup_db?sslmode=disable

# Redis 7 Cache & Locks
REDIS_URL=redis://:Prod_Redis_Pass_2026_Strong@redis:6379/0

# Security Credentials
JWT_SECRET=c8f49a1b63e28d09f7a5b3c1e2d4f6a8b0c2e4d6f8a0b2c4e6f8a0b2c4e6f8a0
TRACKING_WEBHOOK_SECRET=whk_sec_991823471029384710923847

# Payment Gateway
PAYMENT_GATEWAY_MODE=real
PSP_TERMINAL_ID=98127341
PSP_API_KEY=psp_live_sec_1092830192830192

# IPPanel SMS Gateway
IPPANEL_ENABLED=true
IPPANEL_BASE_URL=https://edge.ippanel.com/v1
IPPANEL_API_KEY=live_ippanel_key_77192840192840192840
IPPANEL_SENDER=+983000505
IPPANEL_PATTERN_CODE=otppattern2026

# Storage
UPLOAD_DIR=/app/static/uploads/products
```

---

## 11. Conclusion & Next Steps for Documentation Team

This survey represents an exhaustive analysis of the Sellersho platform codebase. Every component, database table, API route, authentication mechanism, pricing formula, external integration, and infrastructure configuration has been analyzed from primary source code, SQL migrations, and runtime configs.

### Recommended Next Actions for Persian Documentation Writers:
1. **Module 1 (Architecture & Infrastructure):** Translate Section 1, 2, and 9 into `docs/fa/architecture/` with accompanying Mermaid component and container diagrams.
2. **Module 2 (Database & Data Models):** Translate Section 3 and detailed table inventories into `docs/fa/database/` with Mermaid Entity-Relationship (ER) diagrams.
3. **Module 3 (Security & Identity):** Translate Section 4 into `docs/fa/auth/` detailing customer OTP login, seller registration, admin controls, and token lifecycles with sequence diagrams.
4. **Module 4 (Pricing & Financial Operations):** Translate Section 5 and Section 8 into `docs/fa/pricing/` and `docs/fa/finance/`, highlighting the $R = C \times (1 + \text{platformPct})$ and $P = R \times (1 + \text{margin})$ pricing rules and the immutable double-entry ledger.
5. **Module 5 (API Reference):** Use the complete route tables in Section 6 to create the definitive Persian REST API manual in `docs/fa/api/`.
6. **Module 6 (External Integrations):** Translate Section 7 (IPPanel, Shaparak, Tapin, Media Uploads) into `docs/fa/integrations/`.
