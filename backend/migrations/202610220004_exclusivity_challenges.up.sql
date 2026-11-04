-- 202610220004: Exclusivity challenges (گزینهٔ چالش انحصار).
--
-- A supplier whose product proposal is refused by the catalog exclusivity
-- policy (E1) may ask an expert to review the conflict and grant a waiver:
--   branch 'same_variant'  — "exactly this product with exactly this
--                            variant, at a lower price" (+ proposed price)
--   branch 'new_variants'  — "new variant(s) of this product identity"
--                            (variant names) (+ proposed price)
-- Barcode-only conflicts NEVER get a challenge (user decision 1,
-- 2026-10-22): a barcode waiver could never survive the partial unique
-- index on products.barcode, so no rows are created for them.
--
-- Security: the client never sends a conflict reference. The server
-- re-runs the exclusivity check with the challenger's own identity and
-- derives holder references + identity keys server-side; they are stored
-- here and are only ever exposed on admin routes — supplier-facing DTOs
-- are whitelists that contain none of the holder columns.
--
-- Waiver consumption (user decision 2, 2026-10-22): one-time. The product
-- part is consumed by the first successful ApproveProposal of the waived
-- combination (product_consumed_at); rejection in the catalog review
-- dossier does NOT consume it. For branch 'same_variant' the variant part
-- is consumed separately by the first successful variant apply whose
-- variant_key matches (variant_consumed_at).

CREATE TABLE IF NOT EXISTS exclusivity_challenges (
  id                  TEXT PRIMARY KEY,
  supplier_id         TEXT NOT NULL,
  branch              TEXT NOT NULL CHECK (branch IN ('same_variant', 'new_variants')),
  proposed_price      BIGINT NOT NULL,
  -- Challenger's own product identity (as proposed by the challenger).
  product_title       TEXT NOT NULL,
  product_brand       TEXT NOT NULL DEFAULT '',
  product_category    TEXT NOT NULL DEFAULT '',
  barcode             TEXT NOT NULL DEFAULT '',
  -- Server-derived keys/references (admin-only; never in supplier DTOs).
  product_identity_key TEXT NOT NULL,
  holder_kind         TEXT NOT NULL DEFAULT '',
  holder_id           TEXT NOT NULL DEFAULT '',
  holder_title        TEXT NOT NULL DEFAULT '',
  holder_supplier_id  TEXT NOT NULL DEFAULT '',
  holder_product_id   TEXT NOT NULL DEFAULT '',
  -- Branch payload.
  variant_name        TEXT NOT NULL DEFAULT '',
  variant_attributes  TEXT NOT NULL DEFAULT '',
  variant_key         TEXT NOT NULL DEFAULT '',
  variant_names       JSONB NOT NULL DEFAULT '[]'::jsonb,
  -- Challenger's original proposal payload snapshot (admin review aid).
  challenger_payload  JSONB NOT NULL DEFAULT '{}'::jsonb,
  status              TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'approved', 'rejected')),
  rejection_reason    TEXT,
  reviewed_by         TEXT,
  reviewed_at         TIMESTAMPTZ,
  product_consumed_at TIMESTAMPTZ,
  variant_consumed_at TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_exclusivity_challenges_supplier
  ON exclusivity_challenges (supplier_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_exclusivity_challenges_status
  ON exclusivity_challenges (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_exclusivity_challenges_waiver
  ON exclusivity_challenges (supplier_id, status, product_identity_key);
