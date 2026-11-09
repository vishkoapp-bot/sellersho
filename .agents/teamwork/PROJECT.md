# Project: Sellersho Comprehensive Persian Documentation & Workflows

## Architecture
- Documentation root: `docs/fa/`
- Modular subdirectories:
  - `docs/fa/technical/`: Codebase architecture, PostgreSQL schema & migrations, Auth & RBAC, API reference, external integrations.
  - `docs/fa/business/`: Storefront templates & customization, Seller dashboard, canonical pricing engine, Supplier & Warehouse WMS, Admin & Finance.
  - `docs/fa/workflows/`: End-to-end customer checkout, seller onboarding, dual dropship fulfillment, payment & financial settlement, returns & disputes with Mermaid diagrams.
  - `docs/fa/index.md` & `docs/fa/README.md`: Master table of contents, navigation, terminology glossary, and quick start guide.

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Go Modular Monolith Architecture | `backend/` structure, `net/http.ServeMux`, clean layers, Vite/React 19 frontend, Redis 7, Nginx | M1: Technical Docs | survey_tech_report |
| 2 | PostgreSQL 16 Database Schema | 308 migrations, 130+ tables, core entities, foreign keys, indexes | M1: Technical Docs | survey_tech_report |
| 3 | Authentication, OTP & RBAC | 7-day HMAC-SHA256 JWT, Redis OTP with Lua 5-attempt limit, bcrypt super-admin login, capabilities | M1: Technical Docs | survey_tech_report |
| 4 | Backend API Route Catalog | Complete endpoint catalog (auth, stores, catalog, orders, payments, suppliers, warehouse, finance) | M1: Technical Docs | survey_tech_report |
| 5 | External Integrations & Workers | IPPanel SMS, Shaparak PSP, Tapin Post, upload handlers, 2-stage scheduler | M1: Technical Docs | survey_tech_report |
| 6 | Storefront & 5 Blueprint Templates | `tpl-modern-1`, `tpl-electronic-1`, `tpl-fashion-1`, `tpl-digikala-1`, `tpl-tech-1`, live customizer | M2: Business Docs | survey_business_report |
| 7 | Seller Dashboard (19 Tabs) | Store onboarding, custom domain, VIP quotas, catalog import, wallet, discount campaigns | M2: Business Docs | survey_business_report |
| 8 | Canonical Pricing Engine | Formula $P = R \times (1 + \text{margin})$, $R = C \times (1 + \text{PlatformPct})$, margin locks, ceilings | M2: Business Docs | survey_business_report |
| 9 | Supplier Portal & Edit Restrictions | 16 tabs, team RBAC, strict locks ($C$ and stock only), supply readiness tiers | M2: Business Docs | survey_business_report |
| 10 | Central Warehouse WMS Pipeline | 7-stage automated pipeline across 11 tabs, SKU scan, QC, bin putaway, white-label packing | M2: Business Docs | survey_business_report |
| 11 | Admin Panel & Double-Entry Ledger | 30 admin modules, immutable double-entry ledger (`ledger_entries`), Paya sweeps, dispute arbitration | M2: Business Docs | survey_business_report |
| 12 | Customer Purchase & Checkout Workflow | Browsing -> Cart -> JIT Redis reserve -> Gateway -> Callback -> Order capture, Mermaid diagram | M3: Workflows & Diagrams | survey_workflow_report |
| 13 | Seller Onboarding & Listing Workflow | Registration -> Contract v2 -> Template customizer -> Markup rule -> Publish, Mermaid diagram | M3: Workflows & Diagrams | survey_workflow_report |
| 14 | Dropship Order Fulfillment Workflow | Multi-supplier order splitting (`order_item_origins`), WMS vs Direct fulfillment, tracking, Mermaid diagram | M3: Workflows & Diagrams | survey_workflow_report |
| 15 | Payment & Financial Settlement Workflow | PSP capture -> Ledger allocation -> 20-day confirm -> 7-day return hold -> Payout, Mermaid diagram | M3: Workflows & Diagrams | survey_workflow_report |
| 16 | Returns, Warranties & Disputes Workflow | Customer 7-day return -> Review -> RMA -> Warehouse QC -> Refund & ledger reversal, Mermaid diagram | M3: Workflows & Diagrams | survey_workflow_report |
| 17 | Master Index & Persian Table of Contents | Central navigation, complete cross-links, glossary of terms, setup guide | M4: Index & Master Navigation | original_request |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Technical Architecture Docs | `docs/fa/technical/` (architecture.md, database.md, auth.md, api_reference.md, integrations.md) | Survey | PLANNED |
| M2 | Business Domain Docs | `docs/fa/business/` (storefront.md, seller_dashboard.md, pricing_engine.md, supplier_warehouse.md, admin_finance.md) | Survey | PLANNED |
| M3 | End-to-End Workflows & Diagrams | `docs/fa/workflows/` (customer_checkout.md, seller_onboarding_listing.md, dropship_fulfillment.md, payment_financial_settlement.md, returns_disputes.md) | Survey | PLANNED |
| M4 | Master Index & Verification | `docs/fa/index.md` & `docs/fa/README.md` (Table of contents, glossary, navigation cross-checks) | M1, M2, M3 | PLANNED |
| M5 | Audit & Verification Gate | Forensic integrity audit & independent verification of all documentation in `docs/fa/` | M4 | PLANNED |

## Code Layout & Conventions
- Documentation root: `docs/fa/`
- Language: Persian (Farsi) with standard Persian e-commerce terminology and English technical references.
- Style: Highly structured Markdown with headings, tables, bullet points, code blocks (Go, TypeScript, SQL, JSON, cURL, Bash), and Mermaid diagrams.
- Concrete examples: Every single section and feature MUST contain at least one complete, realistic real-world example with actual Iranian Toman prices, telephone numbers, and data models.
