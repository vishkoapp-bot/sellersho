## 2026-11-07T18:09:38Z
You are teamwork_explorer_survey_1.
Your working directory is: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1
Your parent is: 243d6987-8b38-4157-8806-a46f267c60d0

MANDATORY FIRST STEP:
Read C:\Users\LENOVO\projects\sellersho\.agents\teamwork\ORIGINAL_REQUEST.md before starting any work.

Objective:
Conduct an exhaustive technical and architectural survey of the Sellersho platform codebase:
1. Examine the root directory, `backend/`, and `src/` to understand the full technology stack, package dependencies, entry points, configuration, and architectural patterns.
2. Investigate the Database models, schemas, and ORM (Prisma schema in backend, models, migrations, relationships, fields, data types, indexes).
3. Investigate Authentication & Authorization: JWT, OTP, password hashing, session tokens, refresh tokens, role-based access control (Admin, Seller, Customer, Supplier/Warehouse), route guards/middleware.
4. Investigate all Backend API routes, controllers, and services (all endpoints, HTTP methods, route prefixes, request bodies, response shapes, error handling).
5. Investigate External Integrations: Payment gateways (Zarinpal, etc.), SMS services (Kavenegar), storage/upload handlers, webhooks, background tasks, Nginx/Docker configs.
6. Provide concrete examples (sample code, payloads, schema snippets, configs) for every architectural component.

Outputs required:
- Update progress.md in your working directory regularly with "Last visited" timestamps.
- Write your comprehensive technical survey report to:
  `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1\survey_tech_report.md`
- Write your self-contained handoff report to:
  `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_1\handoff.md`
- Once complete, notify parent via send_message.
