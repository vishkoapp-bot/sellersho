## 2026-11-07T23:02:13Z
You are worker_m1_technical.
Your working directory is: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\worker_m1_technical
Your parent is: 243d6987-8b38-4157-8806-a46f267c60d0

MANDATORY FIRST STEP:
Read C:\Users\LENOVO\projects\sellersho\.agents\teamwork\ORIGINAL_REQUEST.md before starting any work.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Write Ownership:
You own exclusively all files in `docs/fa/technical/`. You MUST NOT write to any file outside `docs/fa/technical/` (except your own directory in `.agents/teamwork/worker_m1_technical`).

Authoritative Reference:
Read `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1_gen2\survey_tech_report.md` for complete verified technical facts, schemas, endpoints, and code citations.
Read `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\PROJECT.md`.

Objective:
Create comprehensive, modular technical architecture documentation in fluent Persian (Farsi) in `docs/fa/technical/`:
1. `docs/fa/technical/architecture.md`: Go 1.23 modular monolith, `net/http.ServeMux`, clean layered design, Vite 6/React 19 frontend, Redis 7, Nginx reverse proxy, Docker configuration. Include concrete architecture code examples and configuration blocks.
2. `docs/fa/technical/database.md`: PostgreSQL 16 schema, 308 migrations, 130+ tables, core entities (`users`, `stores`, `products`, `orders`, `supplier_shipments`, `inventory`, `ledger_entries`), relationships, indexes, with concrete SQL schema definitions and sample queries.
3. `docs/fa/technical/auth.md`: Authentication, JWT 7-day tokens, OTP generation with 120s TTL and 30s cooldown, atomic 5-attempt Lua brute-force guard, allowlisted super-admin bcrypt passwords, RBAC capabilities matrix (`user_capabilities`), real-time session validation with concrete cURL/payload examples.
4. `docs/fa/technical/api_reference.md`: Complete backend API catalog (auth, stores, catalog, products, orders, payments, suppliers, warehouse, finance, admin) with request/response schemas, HTTP status codes, error models, and cURL examples.
5. `docs/fa/technical/integrations.md`: Third-party integrations (IPPanel SMS with pattern template sending, Shaparak PSP payment gateway with fail-closed RRN verification, Tapin Post logistics, multipart file upload engine, background worker schedulers) with concrete configuration and payload examples.

Requirements:
- All documentation files MUST be in Persian (Farsi) with standard technical terminology.
- Every section and feature documented MUST contain at least one clear, concrete example.
- References to codebase files and paths must be completely accurate. No placeholders or unverified text.

Outputs required:
- Update progress.md in your working directory regularly with "Last visited" timestamps.
- Write your self-contained handoff report to `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\worker_m1_technical\handoff.md`.
- Notify parent via send_message when complete.
