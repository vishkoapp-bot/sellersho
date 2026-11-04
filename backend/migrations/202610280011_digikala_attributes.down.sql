-- 202610280011 DOWN — reverses the Digikala attributes migration.
--
-- Safe to drop outright: no other table references the dk_* tables
-- (their FKs point INTO categories / dk_attributes, never the reverse),
-- so dropping them cannot orphan product, variant, or proposal rows.
-- Proposal selections live inside pending_approvals.requested_data
-- (JSONB, field categoryAttributes) and are untouched by this file;
-- after the drop the attributes endpoint simply returns 404/empty
-- until the tables are re-created and re-imported.
-- Drop order follows the FK chain (link values before links before
-- registries).

DROP TABLE IF EXISTS dk_category_brands;
DROP TABLE IF EXISTS dk_category_colors;
DROP TABLE IF EXISTS dk_category_attribute_values;
DROP TABLE IF EXISTS dk_category_attributes;
DROP TABLE IF EXISTS dk_attribute_values;
DROP TABLE IF EXISTS dk_attributes;
