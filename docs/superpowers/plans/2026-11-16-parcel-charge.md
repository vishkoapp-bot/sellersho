# Parcel charge business identity implementation

**Goal:** Close FIN-004 with exactly one initial bill and auditable corrections.
**Architecture:** Existing ordered supplier/return/parcel guards own atomic
immutable receipts, signed events and effective bill updates.
**Tech Stack:** Go, PostgreSQL, existing warehouse HTTP middleware.
**Spec:** docs/superpowers/specs/2026-11-16-parcel-charge-design.md

## Global Constraints

All applied migrations through007 and older immutable. New migration160001 only.
No docs/fa or paused networkreward staging, deployment or compilation. No provider
calls, real financial mutations or deletion. Preserve data, credentials and
services. Root owns commit/push/deploy.ps1 after frozen tests and review. Retain
provided codex workspace; use tracked-source archives for full-suite isolation.

## Review Focus

- A replay cannot charge a cancelled or advanced unconfirmed parcel.
- A key reused across parcels or changed payload cannot return another receipt.
- Receiver confirmation and corrections cannot create supplier debits.
- Corrected effective totals must not alter the original immutable evidence.
- Historical ambiguity must abort migration without partial identity assignments.

### Task 1: End-to-end parcel charge business identity

**Files:** Modify backend/internal/repository/returns_operations_repo.go narrowly;
create parcel_charge.go and parcel_charge_integration_test.go beside it, plus
focused migration tests; add backend/migrations/202611160001_parcel_charge_identity
up/down.sql; add backend/cmd/server/parcel_charge_handlers.go and HTTP tests;
wire new route narrowly in main.go, retaining its historical EOL; update existing
private-schema finance fixture DDL only where new contract needs metadata.
Create scripts/audit-parcel-charges.sql and docs/architecture/parcel-charges.md.
Validation-gate repair also modifies worker/background_jobs_acceptance_test.go
and adds worker/background_jobs_gate_test.go under backend/internal; this is
test-only eligibility/teardown/classifier repair, with unchanged production jobs.
The same validation repair narrowly updates repository/background_jobs_test.go
and adds repository/background_jobs_budget_test.go to recognize a driver
cancellation only when its own error chain contains the actual deadline;
unknown and metadata errors remain failures.

**Interfaces:** Keep ConfirmReturnParcelCharge error API. New
CorrectReturnParcelCharge(ctx,returnID,parcelID,actor,key,input) returns an
immutable receipt/error. Input and fields exactly as spec; existing BalanceTx and
transaction-contract guard order remain authoritative. New tables are subordinate
metadata, never independently acquired late locks.

- [x] Read spec, FIN004 supplied contract and guard/balance/migration/HTTP callers.
- [x] Persist actual failing invalid-state debit and receiver/correction gaps using
      guarded disposable DB; don't call compilation failure behavioral RED.
- [x] Add migration, fail-closed old-row evidence audit/backfill and unique original
      business identity; test clean, matched legacy, ambiguous rollback and down.
- [x] Implement initial confirmation with receipt replay, state/key/amount/binding
      checks, single-row transition and atomic event+ledger+receipt writes.
- [x] Implement optimistic correction and immutable replay; assert signed sums,
      original evidence and exact effective final bill across correction chains.
- [x] Wire authenticated warehouse correction route; test missing auth/key, payload,
      409 replay/conflict and existing lock-busy503 behavior.
- [x] Run real concurrent and failure-injection matrices specified by design;
      existing financial/return/inventory regressions must pass.
- [x] Self-review, exact allowlist and report with persisted RED/GREEN evidence;
      do not stage/commit/push/deploy or spawn subagents.
- [x] Root fresh task/final reviews, frozen backend gates, unchanged frontend input
      proof/build, production read-only reconciliation/headroom audit, commit/push,
      deploy.ps1, independent financial/history/migration/health postcheck.
