## 2026-11-07T21:47:45Z
From: 243d6987-8b38-4157-8806-a46f267c60d0
Priority: MESSAGE_PRIORITY_HIGH

You are teamwork_explorer_survey_1_gen2.
Your working directory is: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1_gen2
Your parent is: 243d6987-8b38-4157-8806-a46f267c60d0

MANDATORY FIRST STEP:
Read C:\Users\LENOVO\projects\sellersho\.agents\teamwork\ORIGINAL_REQUEST.md before starting any work.

Context from predecessor:
The platform architecture has been identified as a Go 1.23 modular monolith (`backend/`), PostgreSQL 16 (in `backend/migrations` with 308 SQL migration files), Redis 7, and a React 19 / Vite 6 frontend (`src/`). There is no Prisma ORM — database models are defined in Go structs (`backend/internal/models/` or similar) and SQL migrations.

Objective:
Conduct an exhaustive technical and architectural survey of the Sellersho platform codebase:
1. Examine `backend/cmd/server/main.go`, `backend/internal/`, router, middleware, and config files to document the full architectural pattern.
2. Investigate the Database models, SQL schemas/migrations, tables, relationships, foreign keys, enums, indexes, and data types across the PostgreSQL database.
3. Investigate Authentication & Authorization: JWT handling, OTP generation/validation, password hashing, session tokens, refresh tokens, role-based access control (Admin, Seller, Customer, Supplier/Warehouse), route guards/middleware.
4. Investigate all Backend API routes, controllers, and services (all endpoints, HTTP methods, route groups/prefixes, request bodies, response shapes, error handling, status codes).
5. Investigate External Integrations: Payment gateways (Zarinpal, etc.), SMS services (IPPanel/Kavenegar), storage/upload handlers, Tapin/postal shipping, webhooks, background tasks, Nginx/Docker configs.
6. Provide concrete examples (sample code, payloads, schema snippets, configs) for every architectural component.

Outputs required:
- Update progress.md in your working directory regularly with "Last visited" timestamps.
- Write your comprehensive technical survey report to:
  `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1_gen2\survey_tech_report.md`
- Write your self-contained handoff report to:
  `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1_gen2\handoff.md`
- Once complete, notify parent via send_message.
