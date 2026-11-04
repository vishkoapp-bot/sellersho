-- 202610230008: category visibility flag (is_active).
--
-- Until now categories had NO visibility mechanism: GET
-- /api/v1/categories returned every row unconditionally, so an empty
-- category (e.g. «طلا و زیورآلات» / slug 'gold', 0 products) was publicly
-- displayed with its image as a dead-end first tap. This migration adds
-- the flag the new admin visibility toggle writes:
--
--   - public list (CatalogRepository.GetCategories) filters is_active
--   - admin list (GetAllCategories) returns all rows incl. the flag
--   - PATCH /api/v1/admin/categories/{id}/active flips it and
--     invalidates the Redis key catalog:categories:all
--
-- DEFAULT true keeps every existing category visible, so applying this
-- migration alone changes no public behavior.

ALTER TABLE categories ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT true;
