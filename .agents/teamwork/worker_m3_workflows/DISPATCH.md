## 2026-11-07T23:03:50Z
[Message] timestamp=2026-11-07T23:03:50Z sender=243d6987-8b38-4157-8806-a46f267c60d0 priority=MESSAGE_PRIORITY_HIGH content=You are worker_m3_workflows.
Your working directory is: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\worker_m3_workflows
Your parent is: 243d6987-8b38-4157-8806-a46f267c60d0

MANDATORY FIRST STEP:
Read C:\Users\LENOVO\projects\sellersho\.agents\teamwork\ORIGINAL_REQUEST.md before starting any work.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Write Ownership:
You own exclusively all files in `docs/fa/workflows/`. You MUST NOT write to any file outside `docs/fa/workflows/` (except your own directory in `.agents/teamwork/worker_m3_workflows`).

Authoritative Reference:
Read `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\explorer_doc_survey_3_gen2\survey_workflow_report.md` for complete verified lifecycle flows, Mermaid diagram specifications, equations, and transition matrices.
Read `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\PROJECT.md`.

Objective:
Create comprehensive, end-to-end workflow documentation with visual Mermaid sequence and flowchart diagrams in fluent Persian (Farsi) in `docs/fa/workflows/`:
1. `docs/fa/workflows/customer_checkout.md`: End-to-end customer purchase journey from storefront browsing, multi-supplier cart aggregation, JIT Redis inventory reservation, Shaparak gateway redirection, webhook callback verification, order confirmation, to status transitions. Include a detailed Mermaid sequence diagram and step-by-step concrete Iranian e-commerce example.
2. `docs/fa/workflows/seller_onboarding_listing.md`: Seller registration, OTP verification, store creation, template customization, supplier catalog selection, profit margin application, domain configuration, and publishing. Include a Mermaid sequence diagram and concrete example.
3. `docs/fa/workflows/dropship_fulfillment.md`: Dual fulfillment model (Central Warehouse WMS vs Supplier Direct drop-shipping), multi-supplier order splitting (`order_item_origins`), barcode scanning, white-label packing, Tapin courier dispatch, tracking code issuance, and delivery confirmation. Include Mermaid flowchart and sequence diagrams.
4. `docs/fa/workflows/payment_financial_settlement.md`: Payment gateway capture, double-entry ledger allocation, platform commission extraction, supplier COGS escrow, 2-stage worker release (20-day auto-confirm + 7-day return lock), seller wallet crediting, payout request, and Paya settlement. Include a Mermaid diagram and complete financial arithmetic example in Tomans.
5. `docs/fa/workflows/returns_disputes.md`: Customer return request within 7-day window, photo/reason submission, seller/admin review, RMA generation, warehouse QC inspection, return shipping fee allocation, customer wallet refund, and ledger reversal. Include a Mermaid diagram and real-world RMA scenario.

Requirements:
- All documentation files MUST be in Persian (Farsi).
- Visual workflow diagrams using valid, elegant Mermaid syntax (sequence diagrams and flowcharts) MUST be present in each workflow.
- Every workflow step, actor role, and system transition MUST have clear, concrete examples.
- References to codebase files and paths must be completely accurate. No placeholders or unverified text.

Outputs required:
- Update progress.md in your working directory regularly with "Last visited" timestamps.
- Write your self-contained handoff report to `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\worker_m3_workflows\handoff.md`.
- Notify parent via send_message when complete.
