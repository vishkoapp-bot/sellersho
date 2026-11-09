# Progress - Explorer Doc Survey 1

**Last visited**: 2026-11-07T18:15:20Z
**Status**: Initial repository discovery completed. Found architecture is Go 1.23 modular monolith with PostgreSQL 16 (308 migrations) + Redis 7 + React 19 / Vite 6 frontend. Note: No Prisma ORM in codebase (SQL migrations + Go structs). Diving deep into backend/cmd/server/main.go, models, auth, APIs, and integrations.

## Checklist
- [x] Initialized DISPATCH.md, BRIEFING.md, progress.md
- [x] Read ORIGINAL_REQUEST.md
- [x] Survey repository structure, package.json files, root & backend configs
- [ ] Investigate PostgreSQL schema, database models, migrations, relationships, types, indexes
- [ ] Investigate Authentication & Authorization (JWT, OTP, passwords, sessions, RBAC, middleware)
- [ ] Investigate Backend API routes, controllers, services, request/response models, validation
- [ ] Investigate External Integrations (Zarinpal, IPPanel/Kavenegar, Tapin, storage, webhooks, background tasks, docker/nginx)
- [ ] Synthesize findings into comprehensive `survey_tech_report.md` with concrete examples
- [ ] Compile 5-component `handoff.md`
- [ ] Notify parent agent
