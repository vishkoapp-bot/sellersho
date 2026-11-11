# Canonical product model rebuild implementation plan

> Execute via subagent-driven-development for nonoverlapping implementation domains, then root integration and independent review. No production mutation tests.

**Goal:** one fixed-or-axis attribute definition, stable multi-axis SKU identities through review, catalog, commerce and fulfillment.
**Spec:** ../specs/2026-11-11-product-model-rebuild.md (approved in chat).
**Architecture:** additive V2 schema; legacy product rows retain schemaVersion=0 and productType=legacy. Existing UUIDs/SPNCs and snapshots stay unchanged. New V2 products use strict SKU inventory, canonical attribute/option identities, a structural combination key and a persisted productSchema. Unknown legacy inventory stays unallocated, never divided between SKUs.
**Stack:** Go/PostgreSQL/React/TypeScript.

## Shared contract
- Maximum 500 total combinations/product, 100 attribute definitions/product; generate lazily/bounded; paginate editing.
- productSchema JSON: {version:2,type:'simple'|'variable',attributes:[{id:string,name:string,role:'fixed'|'axis',source:'color'|'dk'|'local'|'requested',registryId?:number,values:[{id:string,label:string}]}]}.
- color attribute id=color; option id=color:<numeric ID>. Imported attributes id=dk:<signed integer>; options id=dkv:<attributeID>:<valueID>. Accept valid negative local-import IDs and validate category membership. Local/requested IDs are UUID-backed stable text IDs; requested definitions/values publish atomically with product approval.
- Proposal top level: schemaVersion:2, productType, productSchema. Each variants[] entry: id(UUID), selection:{attributeID:optionID}, variant_name(auto display only), attributes(label snapshots), supplier_base_cost, stock, is_active. Excluded combinations are omitted or inactive; active zero stock remains a real sold-out SKU.
- Persist products.product_type(default legacy), schema_version(default0), attribute_schema(JSONB). Persist product_variants.selection(JSONB), combination_key(nullable legacy), inventory_mode(default legacy, tracked V2), is_active(defaulttrue). UUID id supplied in a V2 draft persists as actual SKU ID. Do not rewrite existing SKU ids or allocate their old product stock.
- Go domain Product adds ProductType string, SchemaVersion int, ProductSchema *domain.ProductSchema. ProductVariant adds Selection map[string]string, InventoryMode string, IsActive bool, CombinationKey string, MinCartQty int, MaxCartQty *int. Catalog camelCase mirrors these; supplierBaseCost stays private except authorized store-scoped responses.
- New read API GET /api/v1/categories/{slug}/product-attributes -> {attributes:[definitions with id,name,source,registryId,values],brands:existing category brand shape}. Full schema validation is transactional at submission/approval/correction and published SKU mutations.
- V2 correction review paths use stable ids: /variants/<uuid>/stock, /variants/<uuid>/supplier_base_cost, /variants/<uuid>/selection/<attributeID>. Structural review groups /productSchema and /variants permit explicitly rejected structure edits. Existing index paths remain readable for legacy drafts. Root owns granular review integration.
- Public purchase resolution requires active selected SKU for variable/V2. Numeric zero is sold out for tracked, separate from legacy untracked. Stock capture and restore are transactional/idempotent and consume/restore exact SKU plus canonical balances.

## Workstreams and ownership
- [x] Backend model/registry/approval: migration 202611110002_product_model_v2; domain structs; new validation/registry helpers; supplier_repo.go, product_variation_validation.go, catalog_repo.go, handlers.go catalog mapping. Add V2 tests before implementation. Agent core owns these files.
- [x] Commerce/fulfillment: migration 202611110003_sku_inventory; order_handlers.go, order_repo.go, warehouse_repo.go, inventory_unify.go, supplier_shipment_repo.go, tapin_repo.go, supplier_direct_ship_handlers.go and new helpers. Preserve legacy mode and unallocated balances. Agent commerce owns these files. Changes needed in supplier_repo.go receipt/stock routes must be delivered as a patch proposal to root/core to avoid collisions.
- [x] Customer/seller consumers: ProductVariantSelector, PDP/view/modal, ProductCard/DealProductCard, CartContext, StoreContext/direct buy, SellerProductPricingModal and utility tests. Agent UI owns these files. Root owns shared TS types/catalogApi/ProductContext mapper and supplier creator/review.
- [x] Root: shared TS contracts, unified supplier attribute editor, paged combination builder preserving stable ids and stock/prices, pending missing-attribute requests; review/correction stable pointers and structural groups; API registration/route wiring. Keep category picker intact.

## Verification / acceptance
1. Red/green pure validation and combination builder tests: two and three axes; 5x5=25; sparse matrices; duplicate/whitespace/alias protection; fixed-vs-axis exclusivity; negative IDs; missing definitions.
2. Isolated PostgreSQL migration up/down and lifecycle: submission -> field review -> correction -> sparse rereview -> approval; exactly stable UUIDs, no artificial default, requested registry publication, correct facet union.
3. Isolated commerce tests: missing/foreign/inactive/zero SKU rejected; duplicate lines aggregate; concurrent final-unit capture, callback retries, cancellation and QC returns preserve stock exactly.
4. Warehouse receive/QC/putaway/pick/return tests identify exact SKU; unallocated legacy stock stays unchanged.
5. Browser desktop/mobile: unified creator, color x size choices, excluding unavailable combinations, stale matrix protection, partial review/correction, storefront valid combinations, cart/direct buy and min/max limits.
6. Build/vet plus targeted legacy regression tests, independent final review, explicit-file commits, push, deploy.ps1 and read-only health/contract checks. Preserve secrets and other services. Exclude docs/fa and paused networkreward throughout.

## Rulings
- User explicitly authorized implementation and the standing change/validate/commit/push/deploy workflow; no new release approval needed.
- A dedicated managed worktree avoids the concurrently maintained fashion template. No user data mutation during testing.
- Do not silently convert ambiguous legacy inventory or hide historical default SKUs. V2 behavior is explicit and compatibility defaults remain legacy.

## Validation record (2026-11-11)
- 55 discovered TypeScript tests passed; TypeScript compilation and production Vite build passed.
- Targeted backend model, review, compatibility, purchase-limit and DTO tests passed; repository/server go vet passed.
- 15 opt-in PostgreSQL integration tests passed against disposable local PostgreSQL 16 using production schema only (no production rows): 5 complete model/review/publication/return lifecycle tests and 10 inventory/payment/concurrency/QC/putaway tests.
- Migration up/down/up roundtrip passed on a disposable remote empty schema; populated V2 downgrade refused atomically and retained its registry identity in a disposable local database.
- Chrome desktop/mobile tests exercised 5x5 generation, fixed/axis exclusivity, requested size options, pagination, exclusions, regeneration identity preservation, exact partial correction payload and approved-field locks. Artifacts remain ignored under releases/.
- Independent review identified staging inventory and legacy inbound bypasses; both corrected and re-reviewed with no remaining blockers. Physical receipts cannot sell until exact SKU putaway; legacy inbound rejects V2 rather than allocating ambiguous stock.
- Existing IDs and historical snapshots are not rewritten. Legacy unallocated balances are never divided between SKUs.
- Commit/push/release verification follows implementation. Persian docs and paused network reward source are excluded.
