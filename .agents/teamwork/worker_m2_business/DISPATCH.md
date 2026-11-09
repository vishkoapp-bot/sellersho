## 2026-11-07T23:03:03Z
You are worker_m2_business.
Your working directory is: C:\Users\LENOVO\projects\sellersho\.agents\teamwork\worker_m2_business
Your parent is: 243d6987-8b38-4157-8806-a46f267c60d0

MANDATORY FIRST STEP:
Read C:\Users\LENOVO\projects\sellersho\.agents\teamwork\ORIGINAL_REQUEST.md before starting any work.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Write Ownership:
You own exclusively all files in `docs/fa/business/`. You MUST NOT write to any file outside `docs/fa/business/` (except your own directory in `.agents/teamwork/worker_m2_business`).

Authoritative Reference:
Read `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\spec_miner_doc_survey_2_gen2\survey_business_report.md` for complete verified domain rules, tab registries, formulas, and real-world examples.
Read `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\PROJECT.md`.

Objective:
Create comprehensive, modular business domain and user/merchant documentation in fluent Persian (Farsi) in `docs/fa/business/`:
1. `docs/fa/business/storefront.md`: Storefront capabilities, 5 official blueprints (`tpl-modern-1`, `tpl-electronic-1`, `tpl-fashion-1`, `tpl-digikala-1`, `tpl-tech-1`), live draft customizer, multi-supplier cart, direct buy modal, customer order tracking, 7-day return policy, with concrete store configuration examples.
2. `docs/fa/business/seller_dashboard.md`: 19 tabs across 7 groups, onboarding, contract v2 re-confirmation, custom domain routing, VIP multi-store quotas (1 vs 10), supplier product selection, pricing rules, wallet (unsettled vs available), early withdrawal with 5% fee, with concrete merchant scenarios.
3. `docs/fa/business/pricing_engine.md`: Canonical percent-only reference pricing cascade ($P = \text{RoundTo100}(R \times (1 + \text{margin}))$, $R = \text{RoundTo100}(C \times (1 + \text{platformPct}/100))$), margin lock hierarchy (`admin_set` > `product` > `store` > `max-default` ceiling), central store pricing ($C \times 1.10$), and derived display discounts with concrete calculation examples.
4. `docs/fa/business/supplier_warehouse.md`: Supplier portal (16 tabs, team RBAC, strict edit restrictions limiting changes to wholesale cost $C$ and stock only, supply readiness tiers: 3hr, 1day, 3day) and Central Warehouse WMS (7-stage pipeline across 11 tabs: SKU barcode scan, inbound dock, QC with discrepancy filing, bin putaway, picking wave, white-label packing table, shipping manifest, and reverse logistics).
5. `docs/fa/business/admin_finance.md`: Super Admin control panel (30 modules across 7 sections, role matrix: super_admin, admin, finance, logistics, support), immutable double-entry ledger (`ledger_entries`), monthly Paya sweeps, bank reconciliation, dispute arbitration, and audit logs.

Requirements:
- All documentation files MUST be in Persian (Farsi) with standard business and e-commerce terminology.
- Every section and feature documented MUST contain at least one clear, concrete example.
- References to codebase files and paths must be completely accurate. No placeholders or unverified text.

Outputs required:
- Update progress.md in your working directory regularly with "Last visited" timestamps.
- Write your self-contained handoff report to `C:\Users\LENOVO\projects\sellersho\.agents\teamwork\worker_m2_business\handoff.md`.
- Notify parent via send_message when complete.
