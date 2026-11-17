# Durable checkout attempt implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One immutable logical checkout survives ambiguous responses, reload and concurrent tabs, and deliberate repeat purchases remain possible.
**Architecture:** Account-scoped IndexedDB coordinates opaque browser attempt IDs; PostgreSQL binds a normalized request fingerprint to an atomic order outcome. Recovery and explicit abandonment use the same immutable attempt.
**Tech Stack:** React/TypeScript, Go, PostgreSQL, IndexedDB; existing browser/integration runners.
**Spec:** docs/superpowers/specs/2026-11-15-checkout-attempt-design.md

## Global Constraints
- No dependencies, no wallet checkout enablement, no real PSP calls in tests.
- Preserve migrations 001-004, production data/services and credentials.
- Exclude docs/fa and backend/internal/networkreward from commits and deployment.
- Change, validate, commit, push, deploy.ps1 -BuildOnServer, verify.
- Final production-equivalent capacity certification stays open.

## Review Focus
- Delayed original request after abandonment must see a durable tombstone.
- Completed replay must survive changed/deleted saved address and expired quote.
- Late account-A result must never update account-B state or browser pointer.
- Acknowledged old-tab completion must not overwrite a deliberately new attempt.
- No automatic payment-intent or address/code-use effects on replay/recovery.

### Task 1: Durable checkout, browser recovery and release

**Files:**
- Create backend/migrations/202611150005_checkout_attempts.{up,down}.sql; backend/internal/repository/checkout_attempt.go; backend/cmd/server/checkout_attempt_handlers.go.
- Modify backend/internal/repository/order_repo.go; backend/cmd/server/{order_handlers,main}.go.
- Create src/services/checkoutAttempt.ts; src/context/OrderContext.checkout.test.tsx.
- Modify src/context/OrderContext.tsx; src/services/orderApi.ts; src/components/CartDrawer.tsx.
- Create backend/internal/repository/checkout_attempt_integration_test.go; backend/cmd/server/checkout_attempt_http_test.go.
- Update public correctness progress and this plan.

**Interfaces:**
- Browser claimCheckoutAttempt(scope, normalizedPayload, isCurrent) => Promise<CheckoutAttempt>; hash mismatch rejects until explicit resolution/abandonment.
- Browser read/complete/clear compare the expected attempt ID; account/session guards apply before durable mutations.
- Repository BeginCheckoutAttempt(ctx, customerID, key, fingerprint) => (*CheckoutAttempt,error); GetCheckoutAttempt(ctx,customerID,key); AbandonCheckoutAttempt(ctx,customerID,key).
- Repository CreateCheckoutOrderWithShipments(ctx, order, shipments, fingerprint) => (orderID string, created bool, error); same transaction as order/items/origins/shipments.
- GET /api/v1/checkout-attempts/{id} => {attemptId,state,order?}; POST same route {action:'abandon'} => same snapshot. Ownership from JWT only.
- OrderContext recoverCheckout(): Promise<Order|null>; startNewCheckout(): Promise<void>; checkoutAttempt state drives recovery notice. Completed requests include checkoutReplayed for token suppression.

- [x] Write and run mounted browser regression showing a lost committed response creates duplicate orders with current per-call keys. Expected RED: order count 2 instead of 1.
- [x] Write and run backend payload-reuse regression with existing handlers. Expected RED: changed payload replays 200 rather than 409.
- [x] Implement migration, fingerprint/admission, transactional order linkage, owned recovery and abandonment tombstones; add tests for concurrent commit/abandon, immutable request, legacy key and account isolation. Expected all focused isolated tests PASS with one order/items/allocation and unchanged stock/ledger on replay.
- [x] Implement atomic IndexedDB claim/conditional completion, account isolation and fail-closed unavailable storage; integrate OrderProvider and CartDrawer recovery/new-purchase actions. Add reload, simultaneous tabs, double click, changed payload, logout, delayed response, new identical purchase and storage failure browser tests. Expected PASS via node --import tsx --test src/context/OrderContext.checkout.test.tsx.
- [x] Run complete frontend suite, TypeScript, frontend build, vet/Linux build and isolated Go suite from a frozen allowed-source archive. Expected zero failures; retain source hashes and logs.
- [x] Obtain fresh whole-change code review, reproduce/fix important findings, rerun affected/full gates as needed.
- [ ] Update truthful task status, stage explicit allowlist, verify protected paths absent, commit/push; deploy with deploy.ps1 -BuildOnServer and verify health, migration checksums, production financial totals and release marker. Expected clean allowed tree, origin matches HEAD, deployment exit 0, no unexpected data change.

## Review resolution
Both Important findings were reproduced as failing tests: changed catalog title altered recovery; a failed item query returned 200 with no lines. Migration005 now stores immutable product_title_snapshot values for new keyed checkout lines before sealing the attempt. Historical NULL retains the explicit catalog fallback. Order item query/scan/iteration errors propagate. The browser also rejects an incomplete recovered representation before acknowledging it; its regression failed before the validation change and then passed. No prior migration was changed.

Final local gate: candidate845571502a0134ae15eac14e912b6425ed34ebef; Go700 entries/0 failures, frontend300/300, checkout browser10/10, typecheck/build/vet/Linux binaries all passed. Prior001-004 SQL unchanged. Publishing follows this gate; production SSH currently times out, so deployment remains pending rather than certified. Reviewer Minor deferred: compressed browser test formatting. Existing cart editing, best-effort post-commit delivery, browser storage eviction and provider/hardware validation keep the boundaries recorded in the public execution ledger.
