# BRIEFING — 2026-11-07T22:50:30Z

## Mission
Conduct an exhaustive mapping of all end-to-end user journeys, system workflows, and state transitions across the Sellersho platform for comprehensive Persian documentation.

## 🔒 My Identity
- Archetype: explorer
- Roles: explorer, investigator, workflow analyst
- Working directory: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_3_gen2
- Original parent: 243d6987-8b38-4157-8806-a46f267c60d0
- Milestone: survey_workflows_and_state_transitions

## 🔒 Key Constraints
- Read-only investigation — do NOT implement platform code
- Files for content delivery, Messages for coordination
- Self-contained handoff with 5 sections: Observation, Logic Chain, Caveats, Conclusion, Verification Method
- High-fidelity evidence chain referencing exact files and lines

## Current Parent
- Conversation ID: 243d6987-8b38-4157-8806-a46f267c60d0
- Updated: 2026-11-07T22:50:30Z

## Investigation State
- **Explored paths**:
  - `test_lifecycle_e2e.cjs`, `test_direct_dropship_e2e.cjs`, `test_warehouse_e2e.cjs`, `test_finance_e2e.cjs`, `test_supplier_e2e.cjs`, `test_admin_e2e.cjs`, `test_multi_supplier_audit.cjs`, `test_warranty_approval.cjs`
  - `backend/cmd/server/main.go`, `order_handlers.go`, `payment_handlers.go`, `supplier_panel_handlers.go`, `shipping_quote_handlers.go`
  - `backend/internal/repository/order_repo.go`, `settlement_repo.go`, `supplier_panel_repo.go`, `supplier_shipment_repo.go`, `finance_repo.go`
  - `backend/internal/worker/scheduler.go`
  - `src/context/CartContext.tsx`, `OrderContext.tsx`, `StoreContext.tsx`
  - `src/components/CartCheckoutForm.tsx`, `SellerOnboardingModal.tsx`
- **Key findings**:
  1. Complete dual fulfillment mapping (Warehouse vs. Supplier Direct).
  2. Multi-supplier order split architecture and independent shipment progression.
  3. JIT Redis inventory reservation (15-min TTL) and atomic PostgreSQL inventory capture during Shaparak IPG callback.
  4. Staged profit release: carrier delivery alone leaves profit locked; profit released upon customer confirmation or 7-day return window expiration (with 20-day auto-confirm safety net).
  5. Double-entry ledger entries (`ledger_entries`) and canonical supplier settlement formula (`earned - reserved - paid - returnCharges - returnClawbacks + adjustments - storageFees`).
  6. Return lifecycle (quote, file, approve/reject, ship, receive/QC, settle return shipping, refund).
  7. Concrete Persian Mermaid diagrams ready for all 6 workflows.
- **Unexplored areas**: None regarding the requested 6 core workflows and state transitions.

## Key Decisions Made
- Compiling complete, exhaustive documentation in `survey_workflow_report.md` with fully detailed Persian text, concrete numerical examples, exact API paths, database tables, and Mermaid flowcharts and sequence diagrams.

## Artifact Index
- DISPATCH.md — Parent dispatch log
- BRIEFING.md — Persistent context & state
- progress.md — Liveness heartbeat & task progress
- survey_workflow_report.md — Comprehensive workflow survey report
- handoff.md — Self-contained handoff report
