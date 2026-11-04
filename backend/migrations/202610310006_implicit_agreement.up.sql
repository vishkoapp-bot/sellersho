-- Implicit-agreement flag + auto-apply rule (2026-10-31, Andrew's decided policy).
--
-- Decided behaviour:
--   * By DEFAULT, a supplier increase within the effective cap during an
--     unlocked window is approved AUTOMATICALLY and supplier_base_cost
--     updates immediately (no support review). The request row is kept
--     as an audit trail with status='approved' and auto_approved=TRUE.
--   * Products may carry an admin-set implicit-agreement flag: the
--     platform has implicitly agreed with a supplier that this product's
--     price stays fixed until a horizon (e.g. 6 months). For such
--     products an increase request does NOT auto-apply; instead a
--     pending row (agreement_review=TRUE) is created for support/admin
--     decision in the existing price-increases review queue.
--     CORRECTION (2026-10-31): an admin GRANT is itself an approval —
--     an agreement product WITH a valid per-product grant auto-applies
--     immediately at that moment (under the grant's cap); only
--     agreement products WITHOUT a grant route to review.
--
-- Agreement ACTIVE definition (implemented in
-- PricingRepository.IsImplicitAgreementActive): flag is TRUE AND
-- (implicit_agreement_until IS NULL OR implicit_agreement_until >= NOW()).
-- An expired agreement behaves as no agreement: auto-apply resumes.
-- Clearing the flag also clears the horizon (until = NULL).
--
-- Additive only: no existing column or table is altered in place other
-- than adding the four columns below. The products table is assumed to
-- exist, exactly as migration 202610310003 already assumes base tables.
ALTER TABLE products ADD COLUMN IF NOT EXISTS implicit_agreement BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS implicit_agreement_until TIMESTAMPTZ;
ALTER TABLE supplier_price_increases ADD COLUMN IF NOT EXISTS auto_approved BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE supplier_price_increases ADD COLUMN IF NOT EXISTS agreement_review BOOLEAN NOT NULL DEFAULT FALSE;
