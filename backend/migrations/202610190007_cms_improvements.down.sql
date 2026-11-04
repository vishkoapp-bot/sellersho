-- Rollback for CMS improvements (scheduling + history).
DROP TABLE IF EXISTS homepage_section_history;
ALTER TABLE homepage_sections
    DROP COLUMN IF EXISTS starts_at,
    DROP COLUMN IF EXISTS ends_at,
    DROP COLUMN IF EXISTS notes;
