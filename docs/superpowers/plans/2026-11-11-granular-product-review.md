# Granular product review implementation plan

> Execute inline using executing-plans; no delegated work is needed.

**Goal:** readable review of every submitted business field, independent decisions for each SKU price/stock/attribute, rejected-only correction, and fail-closed publication.
**Spec:** user-approved chat requirements, 2026-11-11.
**Architecture:** JSON-pointer leaf keys shared by the review API and editors; hash each reviewed value. Serialize writes by locking the proposal row. Keep existing review/history tables and supplier ownership guard. Preserve legacy feedback for older reviews.
**Tech stack:** Go/Postgres/React/TypeScript.

- [x] Add regression tests for pointer traversal, rejected-only edits, required rejection reasons, stale values and SKU validation.
- [x] Implement field review read/write, audited transactions and final approval gate; migration for value hashes.
- [x] Build readable admin dossier and supplier correction editors; retain decisions on unchanged fields.
- [x] Verify isolated tests, browser interactions, build and authorization; never mutate the user's real proposal for tests.
- [ ] Commit explicit files, push, deploy with deploy.ps1, verify read-only production health and dossier.

Constraints: exclude docs/fa and paused networkreward; preserve production services/secrets. Unknown fields must not authorize mutation of identity/ownership metadata. Reject invalid/stale requests atomically. No synthetic success on failed writes.

Validation ledger: red/green pure tests, isolated PostgreSQL lifecycle tests (no customer data), full five-SKU publication after stock/price/attribute/primary-image corrections, migration down/up, and desktop/mobile browser interactions passed. Independent read-only review blockers were corrected and rechecked. Derived totals and variable SKU labels are displayed as summaries; only their source SKU fields are editable/reviewable. Existing aliases synchronize on correction and return changed mirrors to pending. Release step remains pending until deployed health/dossier verification.
