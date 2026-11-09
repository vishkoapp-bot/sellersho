# BRIEFING — 2026-11-07T18:10:30Z

## Mission
Conduct an exhaustive specification and business logic survey of all platform modules, domain logic, user/merchant/admin/supplier features across Sellersho, documenting features, edge cases, formulas, schemas, and concrete real-world examples in `survey_business_report.md` and generating a complete `handoff.md`.

## 🔒 My Identity
- Archetype: specification-miner
- Roles: Teamwork specialist, Specification Miner (Business Survey 2)
- Working directory: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\spec_miner_doc_survey_2
- Original parent: 243d6987-8b38-4157-8806-a46f267c60d0
- Milestone: Business Domain & Feature Survey

## 🔒 Key Constraints
- Do NOT implement or modify any application code — read-only probe.
- Prioritize authoritative sources (codebase, schemas, domain services, store models, routing, UI components).
- Document features in the required table format with inputs, outputs, error behavior, and discovery method.
- Document edge cases with observed behavior.
- Document concrete real-world examples (sample products, pricing formulas, order scenarios, config settings).
- Maintain persistent progress heartbeat in `progress.md`.
- Communicate via `send_message` with parent upon completion.

## Current Parent
- Conversation ID: 243d6987-8b38-4157-8806-a46f267c60d0
- Updated: not yet

## Task Summary
- **What to build**: Comprehensive survey reports: `survey_business_report.md` and `handoff.md`.
- **Success criteria**: Full coverage of Storefront, Seller Dashboard, Admin Panel, Supplier/Warehouse, Order Processing Lifecycle & Financials, concrete examples, edge cases, formulas, and schema mappings.
- **Interface contracts**: `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\ORIGINAL_REQUEST.md`

## Key Decisions Made
- Will conduct thorough static analysis of `src/` and `backend/` directories to inspect routes, controllers, models, store templates, markup engines, wallet transactions, order state machines, etc.

## Artifact Index
- `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\spec_miner_doc_survey_2\DISPATCH.md` — Dispatch message
- `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\spec_miner_doc_survey_2\progress.md` — Liveness & progress tracker
- `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\spec_miner_doc_survey_2\survey_business_report.md` — Comprehensive business domain survey
- `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\spec_miner_doc_survey_2\handoff.md` — 5-component handoff report
