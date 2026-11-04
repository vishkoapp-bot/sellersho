-- Down for 202610200006 (pending_approvals entity types): restore the
-- original 6-value CHECK from migration 000023.
--
-- Fail-closed by design: ADD CONSTRAINT validates existing rows, so the
-- rollback errors out while proposals of the 3 widened types still exist
-- instead of silently orphaning them.

ALTER TABLE pending_approvals DROP CONSTRAINT IF EXISTS pending_approvals_entity_type_check;
ALTER TABLE pending_approvals ADD CONSTRAINT pending_approvals_entity_type_check
    CHECK (entity_type IN ('store_name', 'store_slug', 'custom_domain', 'proposed_product', 'payout', 'warranty_property'));
