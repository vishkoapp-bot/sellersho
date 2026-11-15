# Monthly settlement resume implementation plan — FIN-002

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline, with fresh read-only reviewers. No implementer delegation.

**Goal:** One bounded, restart-safe monthly reservation service for manual and scheduled sweeps.
**Architecture:** Durable cycle candidate/progress manifest, one canonical account transaction per store, fair bounded batches and separate finalization. Preserve sealed history and rollback compatibility.
**Tech Stack:** Existing Go1.23, PostgreSQL16, repository transaction guards, existing scheduler; no external queue or provider operations.
**Spec:** docs/superpowers/specs/2026-11-15-monthly-settlement-resume-design.md

## Global constraints

- Authoritative supplied FIN-002 acceptance remains mandatory; no production-equivalent capacity certification locally.
- Fee-free monthly internal reservations, noncentral-store filter, configured day/catch-up window and existing bank-details/skip semantics remain.
- Batch25, call4s, per-account1s, lock_timeout100ms, statement_timeout750ms; record measured local limits. No provider call in a transaction.
- CORE order and FIN-001 BalanceTx/account guards govern every debit. No stale amount, invented historical link or new monthly payout after a prior cycle/store effect.
- One manifest initialization transaction; one account per financial transaction; separate finalizer. Incomplete/blocked work cannot complete a cycle. Fair cycle/store retries.
- Existing applied migrations immutable; add006, preserve completed cycles and reject inconsistent history without destructive repair.
- Validate, explicit commit, push, deploy.ps1 -BuildOnServer, verify. Exclude docs/fa and paused networkreward. Protect runtime secrets, services and backups.

## Review focus

- An ambiguous failed/rejected/unknown old payout never causes a second same-cycle reservation.
- A poison/locked account and an older blocked period cannot starve unrelated accounts/new periods.
- Context cancellation after the first account commits leaves durable progress, while the next restart completes only remaining work.
- Finalization racing an account commit cannot mark unfinished work completed or compute inconsistent totals.
- Rollback uses the actual old handlers/service against the additive new schema, preserves progress and leaves financial state unchanged.

### Task 1: Durable per-store settlement and unified triggers

**Files and responsibilities:**

- Create `backend/migrations/202611150006_monthly_settlement_progress.up.sql`: additive statuses/manifest, safe legacy validation, required indexes; preserve original monthly unique effect.
- Create `backend/internal/repository/monthly_settlement.go`: shared initialization, bounded/fair batch, single-account transaction, progress reads/finalization and error classification. Keep new helpers focused.
- Modify `backend/internal/repository/settlement_repo.go`: retain public scheduled signature and delegate to shared service; expose progress fields in cycle DTO. Keep early-withdrawal semantics.
- Modify `backend/cmd/server/finance_parity_handlers.go`: manual run calls shared service and truthful202/409/200 contracts; list/detail expose durable progress without a duplicate financial writer.
- Modify `backend/internal/worker/settlement.go`: bounded tick/resume, fair older/new cycles, truthful partial/blocked logs; no scheduler/lifecycle redesign.
- Extend `backend/internal/repository/transaction_contract.go` only if a new guarded table needs an allowlist entry, preserving existing ordering.
- Add `backend/internal/repository/monthly_settlement_integration_test.go`, server monthly run regressions and migration/old-writer compatibility tests. Use migrated disposable schema and real contention barriers.
- Add operational/architecture contract and update public progress only after acceptance. No new frontend flow unless existing callers require progress handling.

- [x] Establish baseline/source and read-only production counts; retain original race outcome if already fixed by FIN-001. Inspect existing manual/automatic writers and all monthly item/payout review paths.
- [x] Write and execute meaningful RED boundedness/resume tests against old source. Hold a later account to expose whole-sweep account lock retention; cancel after one committed account and require durable progress. Prove the failure without weakening the invariant or manufacturing a regression.
- [x] Implement migration006 with legacy completed-cycle preservation, uniqueness/link checks, manifest/progress fields and scheduling indexes. Rehearse valid history plus duplicate/mismatched fixtures; unsafe fixtures fail atomically and preserve data.
- [x] Implement the shared service and single-account effects with revalidated signed availability, exact cycle/store identity, existing payout lifecycle handling and checked totals. Test two workers, manual+worker and foreground payout/service/early-withdraw competitors; from2m total reservations never exceed2m.
- [x] Implement fair bounded retries/finalizer and running/blocked/completed state. Force crash/cancellation between accounts and resume across restart/window/month boundaries. Assert completed accounts never rearm after payout rejection and a blocked oldest account/cycle does not starve later work.
- [x] Replace manual duplicate writer and scheduled wrapper, preserve API identity/authorization, expose progress and truthful partial statuses. Test both real HTTP handler and worker/repository path with concurrent runs and request cancellation.
- [x] Measure local foreground authorization/debit latency during a multi-account sweep using actual DB locks and disclose limits. Verify no whole-sweep lock holding and bounded transaction/batch size. Hardware SLO remains FIN-013 OPEN.
- [x] Rehearse actual previous manual/automatic code with migration006 and running/blocked cycles: same-period409/return-existing, financial/work state unchanged, old inserts compatible. Document rollback pauses and forward resume; preserve automatic availability rollback.
- [x] Fresh whole-change review; fix Important findings with RED/GREEN. Full allowed frozen Go, frontend/build/typecheck/vet/Linux and required migration/rollback gates; no paused reward package.
- [ ] Explicit allowlist commit/push/deploy.ps1; verify marker, restricted backup, immutable migration checksums, all new migration state, financial preservation and health. Update completion evidence and proceed JOB-01.

## Design check

Fresh read-only reviewer `review_fin002_design` identified only older-cycle
starvation. Resolved with newly-due initialization plus persisted fair last-attempt
selection and per-account retry-after. Reviewer confirmed no important questions
remain. Old writer code supports compatible rollback with paused new progress;
prove it instead of blocking availability rollback based on an assumption.
