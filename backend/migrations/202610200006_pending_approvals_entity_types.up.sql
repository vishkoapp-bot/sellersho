-- 202610200006: widen the pending_approvals.entity_type CHECK to the full
-- set the platform code inserts (store-gaps wave-1 finding #1).
--
-- The constraint on prod (migrations 000001 -> 000023) allows only 6 values:
-- store_name, store_slug, custom_domain, proposed_product, payout,
-- warranty_property. But admin_repo.SubmitCatalogChangeProposal also stages
-- supplier catalog mutations as product_gallery_update /
-- product_variant_create (Fix 8) and product_variant_create_bulk
-- (store-gaps spec §3.6) — the DB rejects those inserts with 23514, which
-- surfaces as HTTP 500, so the single-variant and gallery proposal flows
-- are broken on prod today.
--
-- The complete insertable set was verified against every
-- INSERT INTO pending_approvals in the codebase (CreatePendingApproval,
-- SubmitCatalogChangeProposal, RequestStoreChange, ProposeProduct, the
-- warranty_property staging insert): exactly these 9 values.

ALTER TABLE pending_approvals DROP CONSTRAINT IF EXISTS pending_approvals_entity_type_check;
ALTER TABLE pending_approvals ADD CONSTRAINT pending_approvals_entity_type_check
    CHECK (entity_type IN ('store_name', 'store_slug', 'custom_domain', 'proposed_product', 'payout', 'warranty_property', 'product_gallery_update', 'product_variant_create', 'product_variant_create_bulk'));
