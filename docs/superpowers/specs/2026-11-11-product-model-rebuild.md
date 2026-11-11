# Product and variable-product rebuild design

Status: user-approved design implemented and validated; release verification follows.
Date: 2026-11-11

## User requirement
One clear simple/variable product model. Enter an attribute once; allow several variation axes (color x size x material), independent SKU prices/stock, readable review and correction. Audit all consumers and preserve existing behavior and historical references.

## Reproduced and verified evidence
- SupplierCategoryAttributes offers color independently of SupplierProposeForm variantAxes: duplicate semantic inputs.
- Live men-tee-shirts and women-tee-shirts facet responses each contain 9 attributes and 12 colors, but no size attribute. The imported search facets are not a complete sellable-variation schema.
- Isolated current-validator probe: color x size succeeds; mixed axis sets and trailing-whitespace duplicate combinations incorrectly succeed. Probe artifacts: releases/product-model-audit, ignored.
- Proposal form and validator cap combinations at 24, so 5 colors x 5 sizes fail.
- productType is carried by pending JSON but not persisted in products/domain DTOs; downstream code infers behavior from default flags and SKU counts.
- Canonical attribute picks and free-string SKU attributes have separate identities. Negative local attribute IDs are dropped by proposalAttributePicks; category membership is not enforced by its global value join.
- Published single/bulk SKU additions bypass typed proposal validation.
- Checkout permits omitted variantId and treats zero SKU stock as untracked. Payment ignores affected-row count on SKU decrement. These are source-confirmed failure paths requiring isolated runtime regressions before fixes.
- Warehouse balances/bins are product-scoped; receiving/picking DTOs do not consistently carry the purchased SKU. Existing historical snapshots and SKU IDs must remain intact.
- Actual catalog min/max limits are omitted/hardcoded. Seller preview cost path does not receive authoritative per-SKU scoped costs.
- StoreContext already intercepts no-default quick-add and opens selection; do not misreport that guarded UI path as an unrestricted cart insertion. Raw checkout still needs independent validation.

## Recommended approach: incremental canonical model
Preserve product IDs, SKU UUID/SPNCs, financial snapshots, order/return references and existing URLs. Add explicit versioned product type, attribute roles and stable combination identities. Use additive migrations and compatibility adapters rather than replacing all tables or deleting legacy SKUs.

1. A product attribute is declared once, as either fixed (one value) or a variation axis (several values). Brand remains a product-level selection. Product facets for an axis are derived from the actual SKU options, not a contradictory fixed color field.
2. Category-appropriate central definitions and option IDs drive selection. The absence of size in an import must be explicit. A missing definition/value can be requested and used in the pending product, then promoted to the registry during product approval.
3. Each active SKU has exactly one value for every selected axis. Generate the Cartesian matrix and allow unavailable combinations to be excluded. Excluded is different from active with stock zero. Rebuilding combinations preserves price/stock for identical structural keys.
4. Support a bounded, paginated matrix with a server-enforced documented limit instead of the arbitrary existing 24-row request cap. Never silently discard rows beyond the limit.
5. Canonical validation applies to creation, correction, approval and published SKU changes. Reject contradictory roles, inconsistent axis sets, duplicate combinations, invalid category/value IDs, noninteger money/stock and ambiguous aliases.
6. Review uses stable draft SKU/axis identities. Structural corrections need explicit grouped rejection and dependent review invalidation; ordinary leaf corrections preserve unrelated approvals.
7. Public type/axes metadata enables separate customer color/size selectors resolving to one existing SKU. Missing combinations and zero tracked stock cannot be purchased. Cart/direct-buy preserve the same selected SKU, scoped price and quantity limits.
8. Checkout requires a SKU for variable products. Atomic capture must verify SKU rows affected and aggregate repeated/addon-separated lines. Cancellation and returns mirror actually captured units, once.
9. Warehouse and supplier fulfillment must retain order-item and SKU identity. Introduce SKU allocation/status/location support additively where required; preserve unresolved legacy product-only balances in an explicit unallocated bucket rather than inventing color/size distribution.
10. Preserve store scoping, pricing overrides, campaigns, addons, commissions, search/facets, direct shipping, images, labels, QC and return references. No paused network-reward changes.

## Alternative
A full database/table replacement would simplify new code but requires risky stock and history conversion. It is not recommended for this live system.

## Acceptance matrix
- Simple product retains one real SKU and reloads as simple.
- 2 colors x 3 sizes creates 6 distinct combinations; exclusion and zero stock behave differently; 5x5 is supported within the chosen bound.
- Changing choices preserves unchanged SKU prices/stock and IDs, rejects duplicate/partial combinations and inconsistent axis sets.
- An attribute used as an axis is not requested again as a fixed value.
- Missing size can be requested with several values and survives product review and registry publication.
- Rejected price/stock/attribute or structural group can be corrected; unrelated approvals survive.
- Storefront, catalog, cart/direct-buy, checkout, payment/cancellation/returns and warehouse receipt/picking use the same SKU identity.
- Concurrent purchase of the last unit cannot oversell; retries do not capture or restore twice.
- Existing simple products, legacy unknown allocations and historical references are preserved without guessed redistribution.

## Release constraints
Only synthetic data in isolated PostgreSQL/browser fixtures for mutation tests. Production verification is read-only except the authorized release/migrations. Exclude docs/fa and backend/internal/networkreward. Follow validate, commit, push, deploy.ps1, post-release health/contract checks.
