-- Rollback for 202610310006: drops only the columns this migration added.
-- NOTE: rolling back removes the agreement flags and the auto-apply /
-- agreement-review markers. It does not reverse any price already
-- auto-applied or approved while the migration was live.
ALTER TABLE supplier_price_increases DROP COLUMN IF EXISTS agreement_review;
ALTER TABLE supplier_price_increases DROP COLUMN IF EXISTS auto_approved;
ALTER TABLE products DROP COLUMN IF EXISTS implicit_agreement_until;
ALTER TABLE products DROP COLUMN IF EXISTS implicit_agreement;
